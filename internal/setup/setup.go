// Package setup implements Mason's server-facing workflows: machine onboarding
// (mason setup), the bootstrap/share-store/site provisioning scripts, per-machine key
// management, project init, and doctor. Collaborators (prompts, key generation, SSH) are
// injected so each flow is testable without a terminal or a server.
package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/norlinga/mason/internal/config"
	"github.com/norlinga/mason/internal/remote"
)

// DefaultKeyPath is the key setup generates when no existing SSH setup reaches the server.
const DefaultKeyPath = "~/.ssh/mason"

const defaultAdmin = "root"

// Params controls one setup run.
type Params struct {
	// Server is the destination as typed: host, user@host, host:port, or an ssh alias.
	Server string
	// Key is an explicit private key path (--key); "" lets setup decide.
	Key string
	// Admin is the user that may bootstrap the server (default root).
	Admin string
	// AdminKey is an explicit key for the admin connection; "" uses the normal SSH setup.
	AdminKey string
	// ShareURL, when set, provisions the share store at that URL and records it.
	ShareURL string
	// SharePath overrides the share store's path (default /var/www/<share host>).
	SharePath string
	// Bootstrap forces the bootstrap even when the mason user is already reachable.
	Bootstrap bool
	// PrintOnly renders the scripts and changes nothing.
	PrintOnly bool
	// Yes answers confirmations.
	Yes bool
	// UserFile is the config path to write (default ~/.mason).
	UserFile string
	// ControlDir multiplexes SSH connections; "" disables multiplexing.
	ControlDir string
}

// Deps are the injectable collaborators for Run.
type Deps struct {
	// Connect returns a command runner for the given SSH options.
	Connect func(remote.SSHOptions) remote.CommandRunner
	// Keygen creates a key pair.
	Keygen Keygen
	// Confirm asks a yes/no question.
	Confirm func(question string) (bool, error)
	// Hostname names this machine in key comments; defaults to os.Hostname.
	Hostname func() (string, error)
	// HomeDir resolves ~; defaults to os.UserHomeDir.
	HomeDir func() (string, error)
	// Log receives progress lines; nil discards them.
	Log io.Writer
}

// Result reports what setup did.
type Result struct {
	ConfigPath       string
	ConfigBackup     string
	ConfigChanged    bool
	Server           string
	KeyPath          string
	KeyGenerated     bool
	Bootstrapped     bool
	ShareProvisioned bool
	// Scripts holds the rendered scripts in PrintOnly mode.
	Scripts []string
}

// Keygen generates an SSH key pair.
type Keygen interface {
	Generate(ctx context.Context, keyPath, comment string) error
}

// Run onboards this machine for a server. See setup.go.trellis for the contract.
func Run(ctx context.Context, deps Deps, p Params) (Result, error) {
	deps = withDefaults(deps)
	logf := func(format string, args ...any) { fmt.Fprintf(deps.Log, format+"\n", args...) }

	server, err := config.ParseServer(p.Server, "")
	if err != nil {
		return Result{}, err
	}
	res := Result{Server: server.String()}

	userFile := p.UserFile
	if userFile == "" {
		home, err := deps.HomeDir()
		if err != nil {
			return res, fmt.Errorf("resolve home directory: %w", err)
		}
		userFile = filepath.Join(home, config.FileName)
	}

	// A re-run starts from the key ~/.mason already records, so a key that is not in the
	// agent is not mistaken for "nothing works" (which would generate a new one).
	keyFromConfig := ""
	if p.Key == "" {
		if cfg, err := config.Load(config.LoadOptions{UserFile: userFile, NoProject: true}); err == nil &&
			cfg.Server != nil && cfg.Server.Host == server.Host && cfg.Server.Key != "" {
			p.Key, keyFromConfig = cfg.Server.Key, cfg.Server.Key
		}
	}

	var share *config.Share
	if strings.TrimSpace(p.ShareURL) != "" {
		s, err := config.NewShare(p.ShareURL, p.SharePath, &server)
		if err != nil {
			return res, err
		}
		share = &s
	}

	if p.PrintOnly {
		return printScripts(deps, p, server, share, res)
	}

	opts := sshOpts(server, p.ControlDir)
	opts.AcceptNewHostKey = true

	// 1. Does the existing SSH setup (explicit key, agent, ~/.ssh/config) already work?
	keyPath := ""
	if p.Key != "" {
		if keyPath, err = expandPath(p.Key, deps.HomeDir); err != nil {
			return res, err
		}
		opts.KeyPath = keyPath
	}
	reachable := ping(ctx, deps, opts) == nil
	if !reachable && keyPath == "" {
		// Fall back to Mason's own key, generating it if needed.
		if keyPath, err = expandPath(DefaultKeyPath, deps.HomeDir); err != nil {
			return res, err
		}
		if !fileExists(keyPath) {
			host, _ := deps.Hostname()
			logf("Generating %s for this machine…", DefaultKeyPath)
			if err := deps.Keygen.Generate(ctx, keyPath, "mason@"+host); err != nil {
				return res, fmt.Errorf("generate ssh key: %w", err)
			}
			res.KeyGenerated = true
		}
		opts.KeyPath = keyPath
		reachable = ping(ctx, deps, opts) == nil
	}
	res.KeyPath = opts.KeyPath
	if reachable {
		logf("✓ %s is reachable", server)
	}

	// 2. Bootstrap the server as an admin when the mason user is not reachable.
	if !reachable || p.Bootstrap {
		pub, err := readPublicKey(opts.KeyPath, deps.HomeDir)
		if err != nil {
			return res, err
		}
		script, err := BootstrapScript(BootstrapParams{Host: server.Host, User: server.User, PublicKey: pub, Reload: true})
		if err != nil {
			return res, err
		}
		admin := adminOpts(server, p)
		question := fmt.Sprintf("Bootstrap %s as %s? This creates the %q user, authorizes this machine's key, and adds Caddy's import line (review with --print).",
			server.Host, admin.User, server.User)
		if err := confirm(deps, p.Yes, question); err != nil {
			return res, err
		}
		logf("Bootstrapping %s as %s…", server.Host, admin.Destination())
		if out, err := deps.Connect(admin).Run(ctx, script); err != nil {
			var ce *remote.ConnectError
			if errors.As(err, &ce) {
				return res, fmt.Errorf("%w\nIf another machine can already publish to %s, authorize this one from there:\n  mason key add '%s'",
					err, server.Host, pub)
			}
			return res, fmt.Errorf("bootstrap failed: %w\n%s", err, strings.TrimSpace(out.Stdout+out.Stderr))
		}
		res.Bootstrapped = true
		opts.AcceptNewHostKey = false
		if err := ping(ctx, deps, opts); err != nil {
			return res, fmt.Errorf("bootstrap ran, but %s is still unreachable: %w", server, err)
		}
		logf("✓ %s is reachable", server)
	}

	// 3. Share store, when asked for.
	if share != nil {
		script, err := ShareStoreScript(ShareStoreParams{Share: *share, Reload: true})
		if err != nil {
			return res, err
		}
		admin := adminOpts(server, p)
		if err := confirm(deps, p.Yes, fmt.Sprintf("Provision the share store %s at %s:%s as %s?", share.URL, server.Host, share.Path, admin.User)); err != nil {
			return res, err
		}
		logf("Provisioning share store %s…", share.URL)
		if out, err := deps.Connect(admin).Run(ctx, script); err != nil {
			return res, fmt.Errorf("share store provisioning failed: %w\n%s", err, strings.TrimSpace(out.Stdout+out.Stderr))
		}
		res.ShareProvisioned = true
	}

	// 4. Record what worked.
	keyToWrite := ""
	if opts.KeyPath != "" && keyFromConfig == "" {
		keyToWrite = contractHome(opts.KeyPath, deps.HomeDir)
	}
	write, err := WriteUserConfig(userFile, UserConfigValues{
		Server:    serverValue(server),
		Key:       keyToWrite,
		ShareURL:  shareURL(share),
		SharePath: sharePathValue(share, p.SharePath),
	})
	if err != nil {
		return res, err
	}
	res.ConfigPath, res.ConfigChanged, res.ConfigBackup = userFile, write.Changed, write.Backup
	return res, nil
}

func printScripts(deps Deps, p Params, server config.Server, share *config.Share, res Result) (Result, error) {
	keyPath := p.Key
	if keyPath == "" {
		keyPath = DefaultKeyPath
	}
	pub, err := readPublicKey(keyPath, deps.HomeDir)
	if err != nil {
		pub = "ssh-ed25519 <public key of " + keyPath + " — run 'mason setup' without --print to generate it>"
	}
	script, err := BootstrapScript(BootstrapParams{Host: server.Host, User: server.User, PublicKey: pub, Reload: true})
	if err != nil {
		return res, err
	}
	res.Scripts = append(res.Scripts, script)
	if share != nil {
		script, err := ShareStoreScript(ShareStoreParams{Share: *share, Reload: true})
		if err != nil {
			return res, err
		}
		res.Scripts = append(res.Scripts, script)
	}
	return res, nil
}

func withDefaults(deps Deps) Deps {
	if deps.HomeDir == nil {
		deps.HomeDir = os.UserHomeDir
	}
	if deps.Hostname == nil {
		deps.Hostname = os.Hostname
	}
	if deps.Log == nil {
		deps.Log = io.Discard
	}
	if deps.Connect == nil {
		deps.Connect = func(o remote.SSHOptions) remote.CommandRunner { return remote.NewSSH(o) }
	}
	if deps.Keygen == nil {
		deps.Keygen = NewSystemKeygen()
	}
	return deps
}

func sshOpts(s config.Server, controlDir string) remote.SSHOptions {
	return remote.SSHOptions{Host: s.Host, User: s.User, Port: s.Port, KeyPath: s.Key, ControlDir: controlDir}
}

func adminOpts(s config.Server, p Params) remote.SSHOptions {
	opts := sshOpts(s, "")
	opts.User = orDefault(p.Admin, defaultAdmin)
	opts.KeyPath = p.AdminKey
	opts.AcceptNewHostKey = true
	return opts
}

func ping(ctx context.Context, deps Deps, opts remote.SSHOptions) error {
	_, err := deps.Connect(opts).Run(ctx, "true")
	return err
}

func confirm(deps Deps, yes bool, question string) error {
	if yes {
		return nil
	}
	if deps.Confirm == nil {
		return fmt.Errorf("%s\nre-run with -y to proceed non-interactively", question)
	}
	okay, err := deps.Confirm(question)
	if err != nil {
		return err
	}
	if !okay {
		return errors.New("cancelled; nothing was changed on the server")
	}
	return nil
}

// serverValue is the shortest destination that parses back to s.
func serverValue(s config.Server) string {
	if s.User == config.DefaultUser && s.Port == 0 {
		return s.Host
	}
	return s.String()
}

func shareURL(s *config.Share) string {
	if s == nil {
		return ""
	}
	return s.URL
}

func sharePathValue(s *config.Share, explicit string) string {
	if s == nil || strings.TrimSpace(explicit) == "" {
		return ""
	}
	return s.Path
}

func readPublicKey(keyPath string, home func() (string, error)) (string, error) {
	if keyPath == "" {
		return "", errors.New("no key to authorize: pass --key, or let setup create " + DefaultKeyPath)
	}
	expanded, err := expandPath(keyPath, home)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(expanded + ".pub")
	if err != nil {
		return "", fmt.Errorf("read public key: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func expandPath(p string, home func() (string, error)) (string, error) {
	p = strings.TrimSpace(p)
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	h, err := home()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(h, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/")), nil
}

// contractHome writes a path under $HOME as ~/…, so ~/.mason stays portable.
func contractHome(p string, home func() (string, error)) string {
	h, err := home()
	if err != nil || h == "" {
		return p
	}
	if rel, err := filepath.Rel(h, p); err == nil && !strings.HasPrefix(rel, "..") {
		return "~/" + filepath.ToSlash(rel)
	}
	return p
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// NewSystemKeygen returns a Keygen that shells out to ssh-keygen (ed25519). The key has
// no passphrase; add one later with `ssh-keygen -p -f ~/.ssh/mason` and load it into
// ssh-agent — Mason uses the agent automatically.
func NewSystemKeygen() Keygen { return systemKeygen{} }

type systemKeygen struct{}

func (systemKeygen) Generate(ctx context.Context, keyPath, comment string) error {
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return fmt.Errorf("create key directory: %w", err)
	}
	cmd := exec.CommandContext(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", comment, "-f", keyPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ssh-keygen failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
