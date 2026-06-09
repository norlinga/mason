package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/norlinga/mason/internal/links"
	"github.com/norlinga/mason/internal/output"
	"github.com/spf13/cobra"
)

// pruneRunFunc garbage-collects orphaned blobs. It is the seam that lets output be
// tested without real SSH.
type pruneRunFunc func(ctx context.Context, root rootOptions) (links.PruneResult, error)

type prunedBlobJSON struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

type pruneJSON struct {
	Deleted   bool             `json:"deleted"`
	TotalSize int64            `json:"total_size"`
	Blobs     []prunedBlobJSON `json:"blobs"`
}

func newPruneCommand(stdout, stderr io.Writer, run pruneRunFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "prune",
		Short: "Garbage-collect orphaned blobs (dry-run unless --yes)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			res, err := run(cmd.Context(), root)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			return renderPrune(stdout, opts, res)
		},
	}
}

func renderPrune(stdout io.Writer, opts output.Options, res links.PruneResult) error {
	if opts.JSON {
		blobs := make([]prunedBlobJSON, 0, len(res.Orphaned))
		for _, b := range res.Orphaned {
			blobs = append(blobs, prunedBlobJSON{Hash: b.Blob, Size: b.Size})
		}
		return output.WriteJSON(stdout, output.Success(pruneJSON{
			Deleted:   res.Deleted,
			TotalSize: res.TotalSize,
			Blobs:     blobs,
		}))
	}

	if len(res.Orphaned) == 0 {
		if opts.Quiet {
			return nil
		}
		_, err := fmt.Fprintln(stdout, "No orphaned blobs.")
		return err
	}

	verb := "Would prune"
	if res.Deleted {
		verb = "Pruned"
	}
	fmt.Fprintf(stdout, "%s %d orphaned blob(s) (%s):\n", verb, len(res.Orphaned), output.HumanSize(res.TotalSize))
	for _, b := range res.Orphaned {
		fmt.Fprintf(stdout, "  %s  %s\n", shortHash(b.Blob), output.HumanSize(b.Size))
	}
	if !res.Deleted {
		fmt.Fprintln(stdout, "Run with --yes to delete.")
	}
	return nil
}

// defaultPruneRun wires the real remote stack and runs prune. Deletion happens only
// when --yes is set and --dry-run is not.
func defaultPruneRun(ctx context.Context, root rootOptions) (links.PruneResult, error) {
	resolved, err := resolveTarget(root)
	if err != nil {
		return links.PruneResult{}, err
	}
	stack, err := newRemoteStack(resolved)
	if err != nil {
		return links.PruneResult{}, err
	}
	confirm := root.yes && !root.dryRun
	return stack.links.Prune(ctx, links.PruneRequest{Confirm: confirm})
}
