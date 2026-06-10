package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/norlinga/mason/internal/output"
	"github.com/norlinga/mason/internal/remote"
	"github.com/norlinga/mason/internal/setup"
	"github.com/spf13/cobra"
)

// remoteInitRunFunc generates (and optionally applies) the server bootstrap script.
// It is the seam that lets output be tested without real SSH.
type remoteInitRunFunc func(ctx context.Context, root rootOptions, p remoteInitParams) (remoteInitOutcome, error)

type remoteInitParams struct {
	apply      bool
	adminUser  string
	adminKey   string
	caddyUser  string
	group      string
	caddyfile  string
	snippetDir string
	reload     bool
}

type remoteInitOutcome struct {
	script        string
	applied       bool
	output        string
	pubKeyMissing bool
	keyPath       string
}

type remoteInitJSON struct {
	Script        string `json:"script"`
	Applied       bool   `json:"applied"`
	Output        string `json:"output,omitempty"`
	PublicKeyPath string `json:"public_key_path"`
	PublicKeyOK   bool   `json:"public_key_found"`
}

func newRemoteInitCommand(stdout, stderr io.Writer, run remoteInitRunFunc) *cobra.Command {
	p := remoteInitParams{reload: true}
	var noReload bool
	cmd := &cobra.Command{
		Use:   "remote-init",
		Short: "Emit (or apply) an idempotent server bootstrap script",
		Long: "Generate an idempotent, inspectable bootstrap script for the target: a dedicated\n" +
			"mason service user, a hardened deploy key, the share and deploy trees, Caddy read\n" +
			"access, and a Mason-owned Caddy snippet imported once. Printed by default so you can\n" +
			"review it; use --apply --as-admin <user> to run it over SSH as an admin.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			p.reload = !noReload
			outcome, err := run(cmd.Context(), root, p)
			if err != nil {
				// A failed --apply still captured the script's remote output; that is
				// where the actual failure diagnostics live, so surface it.
				if outcome.output != "" {
					io.WriteString(stderr, outcome.output)
					if !strings.HasSuffix(outcome.output, "\n") {
						io.WriteString(stderr, "\n")
					}
				}
				return emitError(stdout, stderr, opts, err)
			}
			return renderRemoteInit(stdout, stderr, opts, outcome)
		},
	}
	flags := cmd.Flags()
	flags.BoolVar(&p.apply, "apply", false, "run the script over SSH as the admin user")
	flags.StringVar(&p.adminUser, "as-admin", "", "admin user to apply as (required with --apply)")
	flags.StringVar(&p.adminKey, "admin-key", "", "SSH key for the admin connection (default: target key_path)")
	flags.StringVar(&p.caddyUser, "caddy-user", "", "Caddy's system user (default \"caddy\")")
	flags.StringVar(&p.group, "group", "", "service group (default = remote user)")
	flags.StringVar(&p.caddyfile, "caddyfile", "", "main Caddyfile path (default /etc/caddy/Caddyfile)")
	flags.StringVar(&p.snippetDir, "snippet-dir", "", "Mason snippet directory (default /etc/caddy/mason.d)")
	flags.BoolVar(&noReload, "no-reload", false, "do not append caddy validate + reload to the script")
	return cmd
}

func renderRemoteInit(stdout, stderr io.Writer, opts output.Options, outcome remoteInitOutcome) error {
	if opts.JSON {
		return output.WriteJSON(stdout, output.Success(remoteInitJSON{
			Script:        outcome.script,
			Applied:       outcome.applied,
			Output:        outcome.output,
			PublicKeyPath: outcome.keyPath + ".pub",
			PublicKeyOK:   !outcome.pubKeyMissing,
		}))
	}

	if outcome.pubKeyMissing && !opts.Quiet {
		fmt.Fprintf(stderr, "warning: no public key at %s.pub; the script omits key install — run 'mason setup' first\n", outcome.keyPath)
	}

	if outcome.applied {
		if outcome.output != "" {
			io.WriteString(stdout, outcome.output)
			if !strings.HasSuffix(outcome.output, "\n") {
				io.WriteString(stdout, "\n")
			}
		}
		if !opts.Quiet {
			fmt.Fprintln(stderr, "✓ remote-init applied")
		}
		return nil
	}

	// Default: the script is the artifact — print it verbatim for inspection/piping.
	_, err := io.WriteString(stdout, outcome.script)
	if err == nil && !opts.Quiet {
		fmt.Fprintln(stderr, "Review the script above, then apply with: mason remote-init --apply --as-admin <user>")
	}
	return err
}

// defaultRemoteInitRun resolves the target, embeds the deploy public key, generates
// the script, and applies it over SSH when requested.
func defaultRemoteInitRun(ctx context.Context, root rootOptions, p remoteInitParams) (remoteInitOutcome, error) {
	resolved, err := resolveTarget(root)
	if err != nil {
		return remoteInitOutcome{}, err
	}
	target := resolved.Target

	keyPath, err := setup.ResolveKeyPath(target.KeyPath, os.UserHomeDir)
	if err != nil {
		return remoteInitOutcome{}, err
	}
	pub, missing := readPublicKey(keyPath)

	script, err := setup.RemoteInitScript(setup.RemoteInitParams{
		Target:     target,
		TargetName: resolved.Name,
		PublicKey:  pub,
		CaddyUser:  p.caddyUser,
		Group:      p.group,
		Caddyfile:  p.caddyfile,
		SnippetDir: p.snippetDir,
		Reload:     p.reload,
	})
	if err != nil {
		return remoteInitOutcome{}, err
	}

	outcome := remoteInitOutcome{script: script, pubKeyMissing: missing, keyPath: keyPath}
	if !p.apply {
		return outcome, nil
	}

	if strings.TrimSpace(p.adminUser) == "" {
		return outcome, output.NewError(output.CodeUsage, "--apply requires --as-admin <user>", nil)
	}
	opts := remote.SSHOptionsFromTarget(target)
	opts.User = p.adminUser
	if strings.TrimSpace(p.adminKey) != "" {
		opts.KeyPath = p.adminKey
	}
	res, runErr := remote.NewSSH(opts).Run(ctx, script)
	outcome.output = res.Stdout + res.Stderr
	if runErr != nil {
		// The session-run error echoes the whole command — here the entire bootstrap
		// script — so report script failures concisely; the captured output (rendered
		// by the command) carries the diagnostics. Connect errors are already concise.
		if res.ExitCode > 0 {
			return outcome, fmt.Errorf("apply remote-init: bootstrap script exited %d", res.ExitCode)
		}
		return outcome, fmt.Errorf("apply remote-init: %w", runErr)
	}
	outcome.applied = true
	return outcome, nil
}

func readPublicKey(keyPath string) (string, bool) {
	data, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		return "", true
	}
	return strings.TrimSpace(string(data)), false
}
