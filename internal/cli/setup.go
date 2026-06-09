package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/norlinga/mason/internal/config"
	"github.com/norlinga/mason/internal/output"
	"github.com/norlinga/mason/internal/remote"
	"github.com/norlinga/mason/internal/setup"
	"github.com/spf13/cobra"
)

// setupRunFunc performs onboarding and returns the result. It is the seam that lets
// output be tested without real prompts, key generation, or SSH.
type setupRunFunc func(ctx context.Context, root rootOptions, p setup.Params) (setup.Result, error)

type setupJSON struct {
	ConfigPath   string   `json:"config_path"`
	KeyPath      string   `json:"key_path"`
	KeyExists    bool     `json:"key_exists"`
	KeyGenerated bool     `json:"key_generated"`
	Tested       bool     `json:"tested"`
	TestOK       bool     `json:"test_ok"`
	TestError    string   `json:"test_error,omitempty"`
	Warnings     []string `json:"warnings"`
}

func newSetupCommand(stdout, stderr io.Writer, run setupRunFunc) *cobra.Command {
	var (
		in       setup.Input
		noKeygen bool
		force    bool
		noTest   bool
	)
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Onboard a target: ensure an SSH key, write config, test connectivity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			params := setup.Params{
				Input:       in,
				ConfigPath:  root.config,
				GenerateKey: !noKeygen,
				Force:       force,
				Test:        !noTest,
			}
			res, err := run(cmd.Context(), root, params)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			return renderSetup(stdout, opts, res)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&in.TargetName, "name", "", "target name (default \"home\")")
	flags.StringVar(&in.Host, "host", "", "remote host")
	flags.StringVar(&in.User, "user", "", "remote user (default \"mason\")")
	flags.StringVar(&in.Root, "root", "", "site root on the server")
	flags.StringVar(&in.BaseURL, "base-url", "", "public base URL")
	flags.StringVar(&in.KeyPath, "key-path", "", "SSH private key path (default ~/.ssh/id_ed25519)")
	flags.IntVar(&in.Port, "port", 0, "SSH port (default 22)")
	flags.StringVar(&in.DefaultPath, "default-path", "", "share.default_path (optional)")
	flags.StringVar(&in.BuildDir, "build-dir", "", "deploy.build_dir (optional)")
	flags.StringVar(&in.RemotePath, "remote-path", "", "deploy.remote_path (optional)")
	flags.BoolVar(&noKeygen, "no-generate-key", false, "do not create an SSH key when none exists")
	flags.BoolVar(&force, "force", false, "overwrite an existing config")
	flags.BoolVar(&noTest, "no-test", false, "skip the SSH connectivity test")
	return cmd
}

func renderSetup(stdout io.Writer, opts output.Options, res setup.Result) error {
	if opts.JSON {
		warnings := res.Warnings
		if warnings == nil {
			warnings = []string{}
		}
		return output.WriteJSON(stdout, output.Success(setupJSON{
			ConfigPath:   res.ConfigPath,
			KeyPath:      res.KeyPath,
			KeyExists:    res.KeyExists,
			KeyGenerated: res.KeyGenerated,
			Tested:       res.Tested,
			TestOK:       res.TestOK,
			TestError:    res.TestErr,
			Warnings:     warnings,
		}))
	}
	if opts.Quiet {
		return nil
	}

	fmt.Fprintf(stdout, "Wrote config to %s\n", res.ConfigPath)
	fmt.Fprintf(stdout, "SSH key: %s (%s)\n", res.KeyPath, keyStatus(res))
	switch {
	case !res.Tested:
		fmt.Fprintln(stdout, "Connection test: skipped")
	case res.TestOK:
		fmt.Fprintln(stdout, "Connection test: ok")
	default:
		fmt.Fprintf(stdout, "Connection test: failed: %s\n", res.TestErr)
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(stdout, "warning: %s\n", w)
	}
	return nil
}

func keyStatus(res setup.Result) string {
	switch {
	case res.KeyGenerated:
		return "generated"
	case res.KeyExists:
		return "found"
	default:
		return "missing"
	}
}

// defaultSetupRun wires the real prompter, keygen, and SSH connector.
func defaultSetupRun(ctx context.Context, root rootOptions, p setup.Params) (setup.Result, error) {
	deps := setup.Deps{
		Keygen: setup.NewSystemKeygen(),
		Connect: func(target config.Target) remote.CommandRunner {
			return remote.NewSSH(remote.SSHOptionsFromTarget(target))
		},
	}
	// With -y we skip prompts and rely on flags plus defaults.
	if !root.yes {
		deps.Prompter = setup.NewStdinPrompter(os.Stdin, os.Stderr)
	}
	return setup.Run(ctx, deps, p)
}
