package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/norlinga/mason/internal/output"
	"github.com/norlinga/mason/internal/share"
	"github.com/spf13/cobra"
)

// shareRunFunc performs the share and returns its result. It is the seam that lets
// output be tested without real SSH/rsync.
type shareRunFunc func(ctx context.Context, root rootOptions, file, as string) (share.Result, error)

// shareJSON is the stable --json shape for a share.
type shareJSON struct {
	Action     string `json:"action"`
	Uploaded   bool   `json:"uploaded"`
	ObjectPath string `json:"object_path"`
	Slug       string `json:"slug"`
	Ext        string `json:"ext"`
	Hash       string `json:"hash"`
	Size       int64  `json:"size"`
	LongURL    string `json:"long_url"`
	ShortURL   string `json:"short_url"`
}

func newShareCommand(stdout, stderr io.Writer, run shareRunFunc) *cobra.Command {
	var as string
	cmd := &cobra.Command{
		Use:   "share <file>",
		Short: "Share a file and print public URLs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			res, err := run(cmd.Context(), root, args[0], as)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			return renderShare(stdout, opts, args[0], res)
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "object path (trailing slash = directory; full path renames)")
	return cmd
}

func renderShare(stdout io.Writer, opts output.Options, file string, res share.Result) error {
	if opts.JSON {
		return output.WriteJSON(stdout, output.Success(shareJSON{
			Action:     res.Action.String(),
			Uploaded:   res.Uploaded,
			ObjectPath: res.ObjectPath,
			Slug:       res.Slug,
			Ext:        res.Ext,
			Hash:       res.Hash,
			Size:       res.Size,
			LongURL:    res.LongURL,
			ShortURL:   res.ShortURL,
		}))
	}

	if opts.Quiet {
		_, err := fmt.Fprintln(stdout, res.ShortURL)
		return err
	}

	fmt.Fprintf(stdout, "Sharing '%s' → %s\n", filepath.Base(file), res.ObjectPath)
	if res.Uploaded {
		fmt.Fprintf(stdout, "Uploaded %s.\n", output.HumanSize(res.Size))
	} else {
		fmt.Fprintln(stdout, "Content already on server — linking only.")
	}
	fmt.Fprintf(stdout, "  Long:  %s\n", res.LongURL)
	if res.ShortURL != "" {
		fmt.Fprintf(stdout, "  Short: %s\n", res.ShortURL)
	}
	return nil
}

// defaultShareRun wires the real remote stack and runs the share.
func defaultShareRun(ctx context.Context, root rootOptions, file, as string) (share.Result, error) {
	resolved, err := resolveTarget(root)
	if err != nil {
		return share.Result{}, err
	}
	stack, err := newRemoteStack(resolved)
	if err != nil {
		return share.Result{}, err
	}
	svc := share.NewService(stack.target, stack.fs, stack.store, stack.rsync, stack.links)
	return svc.Share(ctx, share.Request{File: filepath.Clean(file), As: as})
}
