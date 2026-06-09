package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/norlinga/mason/internal/output"
	"github.com/norlinga/mason/internal/setup"
	"github.com/spf13/cobra"
)

type initRunFunc func(ctx context.Context, dir string) (string, error)

type initJSON struct {
	Path string `json:"path"`
}

func newInitCommand(stdout, stderr io.Writer, run initRunFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Scaffold a project .mason file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			path, err := run(cmd.Context(), ".")
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			if opts.JSON {
				return output.WriteJSON(stdout, output.Success(initJSON{Path: path}))
			}
			if opts.Quiet {
				return nil
			}
			_, err = fmt.Fprintf(stdout, "Created %s\n", path)
			return err
		},
	}
}

func defaultInitRun(_ context.Context, dir string) (string, error) {
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = cwd
	}
	return setup.InitProject(dir)
}
