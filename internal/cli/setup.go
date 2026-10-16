package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/norlinga/mason/internal/output"
	"github.com/norlinga/mason/internal/remote"
	"github.com/norlinga/mason/internal/setup"
	"github.com/spf13/cobra"
)

// setupRunFunc performs onboarding. It is the seam that lets output be tested without
// prompts, key generation, or SSH.
type setupRunFunc func(ctx context.Context, p setup.Params, log io.Writer, confirm func(string) (bool, error)) (setup.Result, error)

type setupJSON struct {
	ConfigPath       string   `json:"config_path"`
	ConfigChanged    bool     `json:"config_changed"`
	ConfigBackup     string   `json:"config_backup,omitempty"`
	Server           string   `json:"server"`
	KeyPath          string   `json:"key_path,omitempty"`
	KeyGenerated     bool     `json:"key_generated"`
	Bootstrapped     bool     `json:"bootstrapped"`
	ShareProvisioned bool     `json:"share_provisioned"`
	Scripts          []string `json:"scripts,omitempty"`
}

func newSetupCommand(stdout, stderr io.Writer, run setupRunFunc) *cobra.Command {
	var p setup.Params
	cmd := &cobra.Command{
		Use:   "setup <server>",
		Short: "Set up this machine to publish to a server (and bootstrap the server if needed)",
		Long: "Make this machine able to publish to <server> — a host, user@host, host:port, or an\n" +
			"~/.ssh/config alias. Uses your existing SSH setup when it already works; otherwise\n" +
			"creates ~/.ssh/mason and, after asking, bootstraps the server as an admin (default root):\n" +
			"the mason user, this machine's key, and Caddy's import line. Records the server in\n" +
			"~/.mason. Safe to re-run.\n\n" +
			"--share URL also provisions your share store (for 'mason share') at that URL.\n" +
			"--print shows the scripts instead of running anything.",
		Example: "  mason setup example.com\n" +
			"  mason setup example.com --share https://files.example.com\n" +
			"  mason setup example.com --print | less",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			p.Server = args[0]
			p.Yes = root.yes
			// No connection reuse here: setup is testing which credentials work, and a
			// multiplexed master would answer for whichever key opened it.
			var log io.Writer = stderr
			if opts.Quiet || opts.JSON {
				log = io.Discard
			}
			res, err := run(cmd.Context(), p, log, func(q string) (bool, error) {
				return confirmPrompt(stderr, cmd.InOrStdin(), q)
			})
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			return renderSetup(stdout, opts, res)
		},
	}
	f := cmd.Flags()
	f.StringVar(&p.Key, "key", "", "use this private key instead of letting setup choose")
	f.StringVar(&p.Admin, "admin", "", "admin user for bootstrapping (default root)")
	f.StringVar(&p.AdminKey, "admin-key", "", "SSH key for the admin connection (default: your normal SSH setup)")
	f.StringVar(&p.ShareURL, "share", "", "provision your share store at this URL")
	f.StringVar(&p.SharePath, "share-path", "", "share store path on the server (default /var/www/<share host>)")
	f.BoolVar(&p.Bootstrap, "bootstrap", false, "run the server bootstrap even if this machine can already connect")
	f.BoolVar(&p.PrintOnly, "print", false, "print the scripts instead of changing anything")
	return cmd
}

func renderSetup(stdout io.Writer, opts output.Options, res setup.Result) error {
	if opts.JSON {
		return output.WriteJSON(stdout, output.Success(setupJSON{
			ConfigPath: res.ConfigPath, ConfigChanged: res.ConfigChanged, ConfigBackup: res.ConfigBackup,
			Server: res.Server, KeyPath: res.KeyPath, KeyGenerated: res.KeyGenerated,
			Bootstrapped: res.Bootstrapped, ShareProvisioned: res.ShareProvisioned, Scripts: res.Scripts,
		}))
	}
	if len(res.Scripts) > 0 {
		_, err := io.WriteString(stdout, strings.Join(res.Scripts, "\n"))
		return err
	}
	if opts.Quiet {
		return nil
	}
	if res.ConfigBackup != "" {
		fmt.Fprintf(stdout, "Moved your old (v1) config to %s — see CONFIG.md#migrating-from-v1\n", res.ConfigBackup)
	}
	verb := "Already set up"
	if res.ConfigChanged || res.Bootstrapped || res.ShareProvisioned || res.KeyGenerated {
		verb = "Set up"
	}
	fmt.Fprintf(stdout, "✓ %s: this machine publishes to %s\n", verb, res.Server)
	if res.KeyPath != "" {
		fmt.Fprintf(stdout, "  key:    %s\n", res.KeyPath)
	}
	fmt.Fprintf(stdout, "  config: %s\n", res.ConfigPath)
	fmt.Fprintln(stdout, "Next: in a site's project folder, run 'mason init'")
	return nil
}

// defaultSetupRun wires the real key generation and SSH.
func defaultSetupRun(ctx context.Context, p setup.Params, log io.Writer, confirm func(string) (bool, error)) (setup.Result, error) {
	return setup.Run(ctx, setup.Deps{
		Connect: func(o remote.SSHOptions) remote.CommandRunner { return remote.NewSSH(o) },
		Keygen:  setup.NewSystemKeygen(),
		Confirm: confirm,
		Log:     log,
	}, p)
}
