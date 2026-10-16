package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/norlinga/mason/internal/output"
	"github.com/norlinga/mason/internal/remote"
	"github.com/norlinga/mason/internal/setup"
	"github.com/spf13/cobra"
)

// keyDeps resolves the runner (as the mason user on the default server) and this
// machine's public key line ("" when unknown).
type keyDeps func(root rootOptions) (remote.CommandRunner, string, error)

type keyJSON struct {
	Comment     string `json:"comment"`
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	ThisMachine bool   `json:"this_machine"`
}

func newKeyCommand(stdout, stderr io.Writer, deps keyDeps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Manage which machines may publish (ls, add, rm)",
		Long: "List, authorize, and revoke the SSH keys of machines allowed to publish to your\n" +
			"server. Runs as the mason user; no admin access needed. To add a new laptop, run\n" +
			"'mason setup <server>' on it — it prints the exact 'mason key add' line to run here.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "ls",
		Short: "List authorized machines",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			runner, current, err := deps(root)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			keys, err := setup.KeyList(cmd.Context(), runner)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			return renderKeys(stdout, opts, keys, current)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "add <public-key | file.pub | ->",
		Short: "Authorize another machine's public key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			pub, err := readKeyArg(args[0], cmd.InOrStdin())
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			runner, _, err := deps(root)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			k, added, err := setup.KeyAdd(cmd.Context(), runner, pub)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			if opts.JSON {
				return output.WriteJSON(stdout, output.Success(map[string]any{"added": added, "key": toKeyJSON(k, "")}))
			}
			if !opts.Quiet {
				if added {
					fmt.Fprintf(stdout, "✓ Authorized %s (%s)\n", orDash(k.Comment), k.Fingerprint)
				} else {
					fmt.Fprintf(stdout, "Already authorized: %s (%s)\n", orDash(k.Comment), k.Fingerprint)
				}
			}
			return nil
		},
	})

	var force bool
	rm := &cobra.Command{
		Use:   "rm <comment | fingerprint>",
		Short: "Revoke a machine's key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			runner, current, err := deps(root)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			k, err := setup.KeyRemove(cmd.Context(), runner, args[0], current, force)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			if opts.JSON {
				return output.WriteJSON(stdout, output.Success(map[string]any{"removed": toKeyJSON(k, "")}))
			}
			if !opts.Quiet {
				fmt.Fprintf(stdout, "✓ Revoked %s (%s)\n", orDash(k.Comment), k.Fingerprint)
			}
			return nil
		},
	}
	rm.Flags().BoolVar(&force, "force", false, "allow removing this machine's key or the last key")
	cmd.AddCommand(rm)
	return cmd
}

func renderKeys(stdout io.Writer, opts output.Options, keys []setup.AuthorizedKey, current string) error {
	if opts.JSON {
		items := make([]keyJSON, 0, len(keys))
		for _, k := range keys {
			items = append(items, toKeyJSON(k, current))
		}
		return output.WriteJSON(stdout, output.Success(items))
	}
	grid := make([][]string, 0, len(keys))
	for _, k := range keys {
		mark := ""
		if toKeyJSON(k, current).ThisMachine {
			mark = "*"
		}
		grid = append(grid, []string{orDash(k.Comment), k.Type, k.Fingerprint, mark})
	}
	return renderTable(stdout, []string{"MACHINE", "TYPE", "FINGERPRINT", "THIS"}, grid)
}

func toKeyJSON(k setup.AuthorizedKey, current string) keyJSON {
	this := false
	if cur, err := setup.ParsePublicKey(current); err == nil {
		this = cur.Blob == k.Blob
	}
	return keyJSON{Comment: k.Comment, Type: k.Type, Fingerprint: k.Fingerprint, ThisMachine: this}
}

func readKeyArg(arg string, stdin io.Reader) (string, error) {
	switch {
	case arg == "-":
		data, err := io.ReadAll(stdin)
		return strings.TrimSpace(string(data)), err
	case strings.HasSuffix(arg, ".pub") && !strings.Contains(arg, " "):
		data, err := os.ReadFile(arg)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", arg, err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	return arg, nil
}

func defaultKeyDeps(root rootOptions) (remote.CommandRunner, string, error) {
	cfg, err := loadConfig(root)
	if err != nil {
		return nil, "", err
	}
	if cfg.Server == nil {
		return nil, "", errors.New("no server configured — run 'mason setup <host>' first")
	}
	current := ""
	key := cfg.Server.Key
	if key == "" {
		key = setup.DefaultKeyPath
	}
	if expanded, err := expandHome(key); err == nil {
		if data, err := os.ReadFile(expanded + ".pub"); err == nil {
			current = strings.TrimSpace(string(data))
		}
	}
	return remote.NewSSH(remote.SSHOptionsFromServer(*cfg.Server)), current, nil
}

func expandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home + strings.TrimPrefix(p, "~"), nil
}
