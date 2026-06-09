package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/norlinga/mason/internal/output"
	"github.com/norlinga/mason/internal/remote"
	"github.com/spf13/cobra"
)

// syncRunFunc reconciles the local cache and returns the outcome. warn receives
// divergence warnings (stderr in production).
type syncRunFunc func(ctx context.Context, root rootOptions, warn io.Writer) (remote.SyncResult, error)

type syncJSON struct {
	Updated  bool `json:"updated"`
	Diverged bool `json:"diverged"`
}

func newSyncCommand(stdout, stderr io.Writer, run syncRunFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Sync the local cache from the remote manifest",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			res, err := run(cmd.Context(), root, stderr)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			return renderSync(stdout, opts, res)
		},
	}
}

func renderSync(stdout io.Writer, opts output.Options, res remote.SyncResult) error {
	if opts.JSON {
		return output.WriteJSON(stdout, output.Success(syncJSON{
			Updated:  res.CacheUpdated,
			Diverged: res.Diverged,
		}))
	}
	if opts.Quiet {
		return nil
	}
	if !res.CacheUpdated {
		_, err := fmt.Fprintln(stdout, "Local cache already up to date.")
		return err
	}
	_, err := fmt.Fprintln(stdout, "Local cache synced from remote.")
	return err
}

// defaultSyncRun wires the real remote stack and reconciles the cache.
func defaultSyncRun(ctx context.Context, root rootOptions, warn io.Writer) (remote.SyncResult, error) {
	resolved, err := resolveTarget(root)
	if err != nil {
		return remote.SyncResult{}, err
	}
	stack, err := newRemoteStack(resolved)
	if err != nil {
		return remote.SyncResult{}, err
	}
	return remote.Sync(ctx, stack.store, stack.cache, resolved.Name, warn)
}
