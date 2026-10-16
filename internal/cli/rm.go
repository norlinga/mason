package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/norlinga/mason/internal/links"
	"github.com/norlinga/mason/internal/output"
	"github.com/spf13/cobra"
)

// rmRunFunc removes the endpoint named by ref. It is the seam that lets output be
// tested without real SSH.
type rmRunFunc func(ctx context.Context, root rootOptions, ref string) (links.RemoveResult, error)

type rmJSON struct {
	Ref          string `json:"ref"`
	Endpoint     string `json:"endpoint"`
	ShareRemoved bool   `json:"share_removed"`
	BlobOrphaned bool   `json:"blob_orphaned"`
	Hash         string `json:"hash"`
}

func newRmCommand(stdout, stderr io.Writer, run rmRunFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <ref>",
		Short: "Remove a share endpoint (object path or short slug)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			res, err := run(cmd.Context(), root, args[0])
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			return renderRm(stdout, opts, args[0], res)
		},
	}
}

func renderRm(stdout io.Writer, opts output.Options, ref string, res links.RemoveResult) error {
	if opts.JSON {
		return output.WriteJSON(stdout, output.Success(rmJSON{
			Ref:          ref,
			Endpoint:     res.Endpoint,
			ShareRemoved: res.ShareRemoved,
			BlobOrphaned: res.BlobOrphaned,
			Hash:         res.Blob,
		}))
	}
	if opts.Quiet {
		return nil
	}

	fmt.Fprintf(stdout, "Removed %s\n", ref)
	if res.ShareRemoved {
		fmt.Fprintln(stdout, "Share has no remaining endpoints.")
	}
	if res.BlobOrphaned {
		fmt.Fprintln(stdout, "Blob is now unreferenced — run 'mason prune' to reclaim space.")
	}
	return nil
}

// defaultRmRun wires the real remote stack and runs the removal.
func defaultRmRun(ctx context.Context, root rootOptions, ref string) (links.RemoveResult, error) {
	stack, err := shareStack(root)
	if err != nil {
		return links.RemoveResult{}, err
	}
	return stack.links.Remove(ctx, links.RemoveRequest{Ref: ref})
}
