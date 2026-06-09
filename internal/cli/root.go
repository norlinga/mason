package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

type rootOptions struct {
	target  string
	json    bool
	dryRun  bool
	yes     bool
	quiet   bool
	verbose bool
	config  string
}

// Execute runs the mason command with process stdio.
func Execute() error {
	return NewRootCommand(os.Stdout, os.Stderr).Execute()
}

// NewRootCommand builds the root command. It is exported for CLI tests.
func NewRootCommand(stdout, stderr io.Writer) *cobra.Command {
	opts := rootOptions{}

	cmd := &cobra.Command{
		Use:           "mason",
		Short:         "Publish files and static sites to a personal VPS",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	flags := cmd.PersistentFlags()
	flags.StringVarP(&opts.target, "target", "t", "", "override the default target")
	flags.BoolVar(&opts.json, "json", false, "emit machine-readable output")
	flags.BoolVarP(&opts.dryRun, "dry-run", "n", false, "show what would happen without changing anything")
	flags.BoolVarP(&opts.yes, "yes", "y", false, "skip confirmation prompts")
	flags.BoolVarP(&opts.quiet, "quiet", "q", false, "minimal output")
	flags.BoolVarP(&opts.verbose, "verbose", "v", false, "extra detail")
	flags.StringVar(&opts.config, "config", "", "use a specific config file")

	cmd.AddCommand(newVersionCommand(stdout))
	cmd.AddCommand(newShareCommand(stdout, stderr, defaultShareRun))
	cmd.AddCommand(newLsCommand(stdout, stderr, defaultLsRun))
	cmd.AddCommand(newInfoCommand(stdout, stderr, defaultReadRun))
	cmd.AddCommand(newURLCommand(stdout, stderr, defaultReadRun))
	cmd.AddCommand(newMvCommand(stdout, stderr, defaultMvRun))
	cmd.AddCommand(newCpCommand(stdout, stderr, defaultCpRun))
	cmd.AddCommand(newRmCommand(stdout, stderr, defaultRmRun))
	cmd.AddCommand(newDisableCommand(stdout, stderr, defaultDisableRun))
	cmd.AddCommand(newEnableCommand(stdout, stderr, defaultEnableRun))
	cmd.AddCommand(newPruneCommand(stdout, stderr, defaultPruneRun))
	cmd.AddCommand(newSyncCommand(stdout, stderr, defaultSyncRun))
	cmd.AddCommand(newDeployCommand(stdout, stderr, defaultDeployRun))
	cmd.AddCommand(newStatusCommand(stdout, stderr, defaultDeployRun))
	cmd.AddCommand(newInitCommand(stdout, stderr, defaultInitRun))
	cmd.AddCommand(newSetupCommand(stdout, stderr, defaultSetupRun))

	return cmd
}

func newVersionCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the mason version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(stdout, "mason %s\n", version)
			return err
		},
	}
}

func stubCommand(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("%s is not implemented yet", cmd.CommandPath())
		},
	}
}
