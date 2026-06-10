package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/norlinga/mason/internal/config"
	"github.com/norlinga/mason/internal/output"
	"github.com/norlinga/mason/internal/setup"
	"github.com/spf13/cobra"
)

func newTargetCommand(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "target",
		Short: "Manage named targets (ls, add, use, rm)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newTargetLsCommand(stdout, stderr))
	cmd.AddCommand(newTargetAddCommand(stdout, stderr))
	cmd.AddCommand(newTargetUseCommand(stdout, stderr))
	cmd.AddCommand(newTargetRmCommand(stdout, stderr))
	return cmd
}

type targetSummaryJSON struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
	Host    string `json:"host"`
	User    string `json:"user"`
	BaseURL string `json:"base_url"`
}

func newTargetLsCommand(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List configured targets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			_, cfg, err := loadConfigForEdit(root)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			return renderTargetList(stdout, opts, setup.ListTargets(cfg))
		},
	}
}

func renderTargetList(stdout io.Writer, opts output.Options, targets []setup.TargetSummary) error {
	if opts.JSON {
		items := make([]targetSummaryJSON, 0, len(targets))
		for _, t := range targets {
			items = append(items, targetSummaryJSON{
				Name:    t.Name,
				Default: t.IsDefault,
				Host:    t.Host,
				User:    t.User,
				BaseURL: t.BaseURL,
			})
		}
		return output.WriteJSON(stdout, output.Success(items))
	}

	headers := []string{"NAME", "DEFAULT", "HOST", "USER", "BASE_URL"}
	grid := make([][]string, 0, len(targets))
	for _, t := range targets {
		marker := ""
		if t.IsDefault {
			marker = "*"
		}
		grid = append(grid, []string{t.Name, marker, orDash(t.Host), orDash(t.User), orDash(t.BaseURL)})
	}
	return renderTable(stdout, headers, grid)
}

func newTargetAddCommand(stdout, stderr io.Writer) *cobra.Command {
	var (
		in          setup.Input
		makeDefault bool
	)
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a target to the config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			path, cfg, err := loadConfigForEdit(root)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			name := args[0]
			if err := setup.AddTarget(cfg, name, setup.BuildTarget(in), makeDefault); err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			if err := saveConfig(path, cfg); err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			isDefault := cfg.Default == name
			if opts.JSON {
				return output.WriteJSON(stdout, output.Success(targetMutationJSON{Name: name, Default: isDefault}))
			}
			if opts.Quiet {
				return nil
			}
			suffix := ""
			if isDefault {
				suffix = " (now default)"
			}
			_, err = fmt.Fprintf(stdout, "Added target %q%s\n", name, suffix)
			return err
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&in.Host, "host", "", "remote host")
	flags.StringVar(&in.User, "user", "", "remote user (default \"mason\")")
	flags.StringVar(&in.Root, "root", "", "site root on the server")
	flags.StringVar(&in.BaseURL, "base-url", "", "public base URL")
	flags.StringVar(&in.KeyPath, "key-path", "", "SSH private key path")
	flags.IntVar(&in.Port, "port", 0, "SSH port (default 22)")
	flags.StringVar(&in.DefaultPath, "default-path", "", "share.default_path (optional)")
	flags.StringVar(&in.BuildDir, "build-dir", "", "deploy.build_dir (optional)")
	flags.StringVar(&in.RemotePath, "remote-path", "", "deploy.remote_path (optional)")
	flags.BoolVar(&makeDefault, "default", false, "make this the default target")
	return cmd
}

type targetMutationJSON struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

func newTargetUseCommand(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set the default target",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			path, cfg, err := loadConfigForEdit(root)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			name := args[0]
			if err := setup.UseTarget(cfg, name); err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			if err := saveConfig(path, cfg); err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			if opts.JSON {
				return output.WriteJSON(stdout, output.Success(targetMutationJSON{Name: name, Default: true}))
			}
			if opts.Quiet {
				return nil
			}
			_, err = fmt.Fprintf(stdout, "Default target is now %q\n", name)
			return err
		},
	}
}

type targetRemoveJSON struct {
	Removed string `json:"removed"`
	Default string `json:"default"`
}

func newTargetRmCommand(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a target from the config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			path, cfg, err := loadConfigForEdit(root)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			name := args[0]
			wasDefault := cfg.Default == name
			newDefault, err := setup.RemoveTarget(cfg, name)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			if err := saveConfig(path, cfg); err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			if opts.JSON {
				return output.WriteJSON(stdout, output.Success(targetRemoveJSON{Removed: name, Default: newDefault}))
			}
			if opts.Quiet {
				return nil
			}
			if wasDefault {
				_, err = fmt.Fprintf(stdout, "Removed target %q (default is now %q)\n", name, newDefault)
			} else {
				_, err = fmt.Fprintf(stdout, "Removed target %q\n", name)
			}
			return err
		},
	}
}

// loadConfigForEdit resolves the config file path (explicit --config or discovered)
// and loads it for mutation.
func loadConfigForEdit(root rootOptions) (string, *config.Config, error) {
	path := root.config
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", nil, err
		}
		discovered, err := config.Discover(cwd)
		if err != nil {
			return "", nil, err
		}
		path = discovered
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return "", nil, err
	}
	return path, cfg, nil
}

// saveConfig writes a validated config back to path, overwriting it.
func saveConfig(path string, cfg *config.Config) error {
	data, err := setup.MarshalConfig(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
