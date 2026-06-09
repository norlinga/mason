package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/norlinga/mason/internal/domain"
	"github.com/norlinga/mason/internal/output"
	"github.com/spf13/cobra"
)

const infoLabelWidth = 14

// resolveShareRef finds the share named by ref (object path or s/slug) in manifest.
func resolveShareRef(manifest *domain.Manifest, ref string) (*domain.Share, error) {
	parsed, err := domain.ParseRef(ref)
	if err != nil {
		return nil, output.NewError(output.CodeUsage, err.Error(), err)
	}
	var (
		share *domain.Share
		ok    bool
	)
	if parsed.IsSlug() {
		share, ok = manifest.FindShareBySlug(parsed.Value)
	} else {
		share, ok = manifest.FindShareByPath(parsed.Value)
	}
	if !ok {
		return nil, output.NewError(output.CodeUsage, fmt.Sprintf("no share found for %q", ref), nil)
	}
	return share, nil
}

type infoJSON struct {
	ObjectPath string `json:"object_path"`
	PathState  string `json:"path_state"`
	Slug       string `json:"slug"`
	Ext        string `json:"ext"`
	SlugState  string `json:"slug_state"`
	Hash       string `json:"hash"`
	Size       int64  `json:"size"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	LongURL    string `json:"long_url"`
	ShortURL   string `json:"short_url"`
	BlobRefs   int    `json:"blob_refs"`
}

func newInfoCommand(stdout, stderr io.Writer, run lsRunFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "info <ref>",
		Short: "Show details for one share",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			data, err := run(cmd.Context(), root)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			share, err := resolveShareRef(data.manifest, args[0])
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			return renderInfo(stdout, opts, data, share)
		},
	}
}

func renderInfo(stdout io.Writer, opts output.Options, data lsData, share *domain.Share) error {
	view := infoJSON{
		Hash:      share.Blob,
		Size:      data.manifest.Blobs[share.Blob].Size,
		CreatedAt: share.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: share.UpdatedAt.UTC().Format(time.RFC3339),
		BlobRefs:  data.manifest.BlobRefCount(share.Blob),
	}
	if share.Path != nil {
		view.ObjectPath = share.Path.Value
		view.PathState = string(share.Path.State)
		view.LongURL = domain.ObjectURL(data.baseURL, share.Path.Value)
	}
	if share.Slug != nil {
		view.Slug = share.Slug.Value
		view.Ext = share.Slug.Ext
		view.SlugState = string(share.Slug.State)
		view.ShortURL = domain.SlugURL(data.baseURL, share.Slug.Value, share.Slug.Ext)
	}

	if opts.JSON {
		return output.WriteJSON(stdout, output.Success(view))
	}

	line := func(label, value string) {
		fmt.Fprintf(stdout, "%-*s%s\n", infoLabelWidth, label+":", value)
	}
	line("Object path", endpointDisplay(view.ObjectPath, view.PathState))
	line("Short slug", endpointDisplay(view.Slug, view.SlugState))
	line("Hash", view.Hash)
	line("Size", output.HumanSize(view.Size))
	line("Created", view.CreatedAt)
	line("Updated", view.UpdatedAt)
	line("Long URL", orDash(view.LongURL))
	line("Short URL", orDash(view.ShortURL))
	line("Blob refs", fmt.Sprintf("%d", view.BlobRefs))
	return nil
}

func endpointDisplay(value, state string) string {
	if value == "" {
		return emptyCell
	}
	if state == "" {
		return value
	}
	return fmt.Sprintf("%s (%s)", value, state)
}

func defaultReadRun(ctx context.Context, root rootOptions) (lsData, error) {
	return defaultLsRun(ctx, root)
}
