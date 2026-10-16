package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/norlinga/mason/internal/domain"
	"github.com/norlinga/mason/internal/links"
	"github.com/norlinga/mason/internal/output"
	"github.com/spf13/cobra"
)

// mvRunFunc re-homes a share's object-path endpoint and returns the outcome plus
// the target base URL for rendering. It is the seam that lets output be tested
// without real SSH.
type mvRunFunc func(ctx context.Context, root rootOptions, src, dst string) (mvOutcome, error)

type mvOutcome struct {
	res     links.MoveResult
	baseURL string
}

type mvJSON struct {
	OldPath    string `json:"old_path"`
	ObjectPath string `json:"object_path"`
	Slug       string `json:"slug"`
	Hash       string `json:"hash"`
	LongURL    string `json:"long_url"`
}

func newMvCommand(stdout, stderr io.Writer, run mvRunFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "mv <src> <dst>",
		Short: "Re-home a share's object path to a new path",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			outcome, err := run(cmd.Context(), root, args[0], args[1])
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			return renderMv(stdout, opts, outcome)
		},
	}
}

func renderMv(stdout io.Writer, opts output.Options, outcome mvOutcome) error {
	res := outcome.res
	view := mvJSON{
		OldPath:    res.OldPath,
		ObjectPath: res.NewPath,
		Hash:       res.Share.Blob,
		LongURL:    domain.ObjectURL(outcome.baseURL, res.NewPath),
	}
	if res.Share.Slug != nil {
		view.Slug = res.Share.Slug.Value
	}

	if opts.JSON {
		return output.WriteJSON(stdout, output.Success(view))
	}
	if opts.Quiet {
		_, err := fmt.Fprintln(stdout, view.LongURL)
		return err
	}
	if view.OldPath != "" {
		fmt.Fprintf(stdout, "Moved %s → %s\n", view.OldPath, view.ObjectPath)
	} else {
		fmt.Fprintf(stdout, "Re-homed object path → %s\n", view.ObjectPath)
	}
	fmt.Fprintf(stdout, "  Long:  %s\n", view.LongURL)
	return nil
}

// defaultMvRun wires the real remote stack and runs the move.
func defaultMvRun(ctx context.Context, root rootOptions, src, dst string) (mvOutcome, error) {
	stack, err := shareStack(root)
	if err != nil {
		return mvOutcome{}, err
	}
	res, err := stack.links.Move(ctx, links.MoveRequest{Src: src, Dst: dst})
	if err != nil {
		return mvOutcome{}, err
	}
	return mvOutcome{res: res, baseURL: stack.share.URL}, nil
}
