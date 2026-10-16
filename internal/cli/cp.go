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

// cpRunFunc duplicates a share and returns the outcome plus the target base URL
// for rendering. It is the seam that lets output be tested without real SSH.
type cpRunFunc func(ctx context.Context, root rootOptions, src, dst string) (cpOutcome, error)

type cpOutcome struct {
	res     links.CopyResult
	baseURL string
}

type cpJSON struct {
	ObjectPath string `json:"object_path"`
	Slug       string `json:"slug"`
	Ext        string `json:"ext"`
	Hash       string `json:"hash"`
	LongURL    string `json:"long_url"`
	ShortURL   string `json:"short_url"`
}

func newCpCommand(stdout, stderr io.Writer, run cpRunFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "cp <src> <dst>",
		Short: "Duplicate a share at a new path with a new slug",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			outcome, err := run(cmd.Context(), root, args[0], args[1])
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			return renderCp(stdout, opts, outcome)
		},
	}
}

func renderCp(stdout io.Writer, opts output.Options, outcome cpOutcome) error {
	share := outcome.res.Share
	view := cpJSON{Hash: share.Blob}
	if share.Path != nil {
		view.ObjectPath = share.Path.Value
		view.LongURL = domain.ObjectURL(outcome.baseURL, share.Path.Value)
	}
	if share.Slug != nil {
		view.Slug = share.Slug.Value
		view.Ext = share.Slug.Ext
		view.ShortURL = domain.SlugURL(outcome.baseURL, share.Slug.Value, share.Slug.Ext)
	}

	if opts.JSON {
		return output.WriteJSON(stdout, output.Success(view))
	}
	if opts.Quiet {
		_, err := fmt.Fprintln(stdout, view.ShortURL)
		return err
	}
	fmt.Fprintf(stdout, "Copied → %s\n", view.ObjectPath)
	fmt.Fprintf(stdout, "  Long:  %s\n", view.LongURL)
	if view.ShortURL != "" {
		fmt.Fprintf(stdout, "  Short: %s\n", view.ShortURL)
	}
	return nil
}

// defaultCpRun wires the real remote stack and runs the copy.
func defaultCpRun(ctx context.Context, root rootOptions, src, dst string) (cpOutcome, error) {
	stack, err := shareStack(root)
	if err != nil {
		return cpOutcome{}, err
	}
	res, err := stack.links.Copy(ctx, links.CopyRequest{Src: src, Dst: dst})
	if err != nil {
		return cpOutcome{}, err
	}
	return cpOutcome{res: res, baseURL: stack.share.URL}, nil
}
