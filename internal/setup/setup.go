// Package setup implements local onboarding (mason setup): collect target details,
// ensure an SSH key exists, write a validated .mason config, and test connectivity.
// It is local-only — it never bootstraps the server (that is remote-init). The core
// is dependency-injected so the wizard logic is testable without real prompts or SSH.
package setup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/norlinga/mason/internal/config"
	"github.com/norlinga/mason/internal/remote"
	"gopkg.in/yaml.v3"
)

const (
	defaultKeyRelPath = ".ssh/id_ed25519"
	defaultTargetName = "home"
	defaultUser       = "mason"
	defaultRoot       = "/srv/share"
)

// Input holds the target details collected from flags and/or prompts.
type Input struct {
	TargetName  string
	Host        string
	User        string
	Root        string
	BaseURL     string
	KeyPath     string
	Port        int
	DefaultPath string
	BuildDir    string
	RemotePath  string
}

// Params controls one setup run.
type Params struct {
	Input       Input
	ConfigPath  string // where to write config; "" → ~/.mason
	GenerateKey bool   // create an SSH key if missing (the default onboarding path)
	Force       bool   // overwrite an existing config
	Test        bool   // test SSH connectivity after writing
}

// Result reports what setup did. A failed connectivity test is non-fatal and is
// reported here rather than returned as an error.
type Result struct {
	ConfigPath   string
	KeyPath      string
	KeyExists    bool
	KeyGenerated bool
	Tested       bool
	TestOK       bool
	TestErr      string
	Warnings     []string
}

// Prompter asks the user for a value, offering a default. Implementations read from
// a terminal; tests inject a scripted prompter.
type Prompter interface {
	Ask(label, def string) (string, error)
}

// Keygen generates an SSH key pair at keyPath.
type Keygen interface {
	Generate(ctx context.Context, keyPath string) error
}

// Deps are the injectable collaborators for Run.
type Deps struct {
	// Prompter fills empty Input fields. When nil, no prompting occurs and only
	// flag-supplied values plus built-in defaults are used.
	Prompter Prompter
	// Keygen creates a key when GenerateKey is set and none exists.
	Keygen Keygen
	// Connect returns a command runner for the SSH connectivity test.
	Connect func(target config.Target) remote.CommandRunner
	// HomeDir resolves the user's home directory; defaults to os.UserHomeDir.
	HomeDir func() (string, error)
}

// Run performs onboarding: fill input, build+validate config, ensure the key, write
// the config, and optionally test connectivity.
func Run(ctx context.Context, deps Deps, params Params) (Result, error) {
	if deps.HomeDir == nil {
		deps.HomeDir = os.UserHomeDir
	}

	in := params.Input
	if deps.Prompter != nil {
		filled, err := fillInput(in, deps.Prompter)
		if err != nil {
			return Result{}, err
		}
		in = filled
	}

	// Resolve the key path before building config so the written .mason records
	// the path actually used, not an empty field backed by runtime defaults.
	keyPath, err := ResolveKeyPath(in.KeyPath, deps.HomeDir)
	if err != nil {
		return Result{}, err
	}
	in.KeyPath = keyPath

	cfg, err := BuildConfig(in)
	if err != nil {
		return Result{}, err
	}
	target := cfg.Targets[cfg.Default]

	data, err := MarshalConfig(cfg)
	if err != nil {
		return Result{}, err
	}

	result := Result{KeyPath: keyPath, Warnings: Warnings(target)}
	result.KeyExists = fileExists(keyPath)
	if !result.KeyExists && params.GenerateKey {
		if deps.Keygen == nil {
			return Result{}, fmt.Errorf("key generation requested but no keygen is configured")
		}
		if err := deps.Keygen.Generate(ctx, keyPath); err != nil {
			return Result{}, fmt.Errorf("generate ssh key: %w", err)
		}
		result.KeyGenerated = true
		result.KeyExists = true
	}
	if !result.KeyExists {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("no SSH key at %s; connectivity test skipped — generate one with ssh-keygen -t ed25519 or rerun setup without --no-generate-key", keyPath))
	}

	configPath, err := resolveConfigPath(params.ConfigPath, deps.HomeDir)
	if err != nil {
		return Result{}, err
	}
	if err := WriteConfigFile(configPath, data, params.Force); err != nil {
		return Result{}, err
	}
	result.ConfigPath = configPath

	if params.Test && result.KeyExists && deps.Connect != nil {
		result.Tested = true
		if err := TestConnection(ctx, deps.Connect(target)); err != nil {
			result.TestErr = err.Error()
		} else {
			result.TestOK = true
		}
	}
	return result, nil
}

// BuildConfig assembles a single-target config from input, applying defaults, and
// validates it.
func BuildConfig(in Input) (*config.Config, error) {
	name := strings.TrimSpace(in.TargetName)
	if name == "" {
		name = defaultTargetName
	}
	user := strings.TrimSpace(in.User)
	if user == "" {
		user = defaultUser
	}

	target := config.Target{
		Host:    strings.TrimSpace(in.Host),
		User:    user,
		Root:    strings.TrimSpace(in.Root),
		BaseURL: strings.TrimSpace(in.BaseURL),
		KeyPath: strings.TrimSpace(in.KeyPath),
		Port:    in.Port,
	}
	target.Share.DefaultPath = strings.TrimSpace(in.DefaultPath)
	target.Deploy.BuildDir = strings.TrimSpace(in.BuildDir)
	target.Deploy.RemotePath = strings.TrimSpace(in.RemotePath)

	cfg := &config.Config{
		Default: name,
		Targets: map[string]config.Target{name: target},
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// MarshalConfig renders config as YAML.
func MarshalConfig(cfg *config.Config) ([]byte, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	return data, nil
}

// Warnings returns onboarding best-practice nudges for a target. They are advisory,
// never fatal.
func Warnings(target config.Target) []string {
	var warnings []string
	switch user := strings.TrimSpace(target.User); user {
	case "root":
		warnings = append(warnings, "user is 'root': prefer a dedicated unprivileged 'mason' service account; run remote-init as an admin to create it")
	case "", defaultUser:
		// dedicated service user — the recommended path.
	default:
		warnings = append(warnings, fmt.Sprintf("user is %q: a dedicated 'mason' service account is recommended so a leaked deploy key only reaches the served trees", user))
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(target.BaseURL)), "http://") {
		warnings = append(warnings, "base_url uses http:// (no TLS); https:// is strongly recommended")
	}
	return warnings
}

// ResolveKeyPath returns the absolute SSH key path, defaulting to ~/.ssh/id_ed25519.
func ResolveKeyPath(keyPath string, home func() (string, error)) (string, error) {
	keyPath = strings.TrimSpace(keyPath)
	if keyPath == "" {
		h, err := home()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(h, defaultKeyRelPath), nil
	}
	return expandHome(keyPath, home)
}

// WriteConfigFile writes config data to path, refusing to overwrite unless force.
func WriteConfigFile(path string, data []byte, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; pass --force to overwrite", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", path, err)
		}
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create config directory: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// TestConnection runs a trivial remote command to verify SSH connectivity.
func TestConnection(ctx context.Context, runner remote.CommandRunner) error {
	if runner == nil {
		return fmt.Errorf("no ssh runner")
	}
	res, err := runner.Run(ctx, "true")
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("remote check exited %d: %s", res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	return nil
}

// NewSystemKeygen returns a Keygen that shells out to ssh-keygen (ed25519, no passphrase).
func NewSystemKeygen() Keygen { return systemKeygen{} }

type systemKeygen struct{}

func (systemKeygen) Generate(ctx context.Context, keyPath string) error {
	if dir := filepath.Dir(keyPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create key directory: %w", err)
		}
	}
	cmd := exec.CommandContext(ctx, "ssh-keygen", "-t", "ed25519", "-N", "", "-f", keyPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ssh-keygen failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// StdinPrompter reads answers from a reader, echoing labels to a writer.
type StdinPrompter struct {
	r *bufio.Reader
	w io.Writer
}

// NewStdinPrompter builds a terminal prompter over r/w.
func NewStdinPrompter(r io.Reader, w io.Writer) *StdinPrompter {
	return &StdinPrompter{r: bufio.NewReader(r), w: w}
}

// Ask prints the label (with default) and returns the entered line, or def when blank.
func (p *StdinPrompter) Ask(label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(p.w, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(p.w, "%s: ", label)
	}
	line, err := p.r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}

// fillInput prompts for any empty fields, leaving flag-supplied values untouched.
func fillInput(in Input, p Prompter) (Input, error) {
	ask := func(label, current, def string) (string, error) {
		if strings.TrimSpace(current) != "" {
			return current, nil
		}
		return p.Ask(label, def)
	}
	var err error
	if in.TargetName, err = ask("Target name", in.TargetName, defaultTargetName); err != nil {
		return in, err
	}
	if in.Host, err = ask("Remote host", in.Host, ""); err != nil {
		return in, err
	}
	if in.User, err = ask("Remote user", in.User, defaultUser); err != nil {
		return in, err
	}
	if in.Root, err = ask("Site root on server", in.Root, defaultRoot); err != nil {
		return in, err
	}
	baseURLDefault := ""
	if host := strings.TrimSpace(in.Host); host != "" {
		baseURLDefault = "https://" + host
	}
	if in.BaseURL, err = ask("Public base URL", in.BaseURL, baseURLDefault); err != nil {
		return in, err
	}
	if in.KeyPath, err = ask("SSH key path (blank = ~/.ssh/id_ed25519)", in.KeyPath, ""); err != nil {
		return in, err
	}
	return in, nil
}

func resolveConfigPath(path string, home func() (string, error)) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		h, err := home()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(h, projectConfigName), nil
	}
	return expandHome(path, home)
}

func expandHome(path string, home func() (string, error)) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		h, err := home()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if path == "~" {
			return h, nil
		}
		return filepath.Join(h, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
