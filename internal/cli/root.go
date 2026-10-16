package cli

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// version is stamped by `make build` (-X …/internal/cli.version=$(git describe)). When
// unset, resolveVersion falls back to the VCS info Go embeds in every module build.
var version = ""

// resolveVersion returns the stamped version, else the embedded commit (with -dirty for
// uncommitted changes), else "dev" (e.g. go run, or a build outside a git checkout).
func resolveVersion(stamped string, info *debug.BuildInfo, ok bool) string {
	if stamped != "" {
		return stamped
	}
	if !ok || info == nil {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v // go install …@vX.Y.Z
	}
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if dirty {
		rev += "-dirty"
	}
	return "dev-" + rev
}

func currentVersion() string {
	info, ok := debug.ReadBuildInfo()
	return resolveVersion(version, info, ok)
}

type rootOptions struct {
	json    bool
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
		Version:       currentVersion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	flags := cmd.PersistentFlags()
	flags.BoolVar(&opts.json, "json", false, "emit machine-readable output")
	flags.BoolVarP(&opts.yes, "yes", "y", false, "answer yes to confirmations")
	flags.BoolVarP(&opts.quiet, "quiet", "q", false, "minimal output")
	flags.BoolVarP(&opts.verbose, "verbose", "v", false, "extra detail")
	flags.StringVar(&opts.config, "config", "", "use this project .mason instead of searching upward")

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
	cmd.AddCommand(newInitCommand(stdout, stderr, defaultInitDeps))
	cmd.AddCommand(newSetupCommand(stdout, stderr, defaultSetupRun))
	cmd.AddCommand(newProvisionCommand(stdout, stderr, defaultProvisionRun))
	cmd.AddCommand(newDoctorCommand(stdout, stderr, defaultDoctorRun))
	cmd.AddCommand(newKeyCommand(stdout, stderr, defaultKeyDeps))

	return cmd
}

func newVersionCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the mason version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(stdout, "mason %s\n", currentVersion())
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
