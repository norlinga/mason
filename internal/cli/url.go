package cli

import (
	"fmt"
	"io"

	"github.com/norlinga/mason/internal/domain"
	"github.com/norlinga/mason/internal/output"
	"github.com/spf13/cobra"
)

type urlJSON struct {
	URL string `json:"url"`
}

func newURLCommand(stdout, stderr io.Writer, run lsRunFunc) *cobra.Command {
	var long bool
	cmd := &cobra.Command{
		Use:   "url <ref>",
		Short: "Print a share URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			data, err := run(cmd.Context(), root)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			if data.stale != "" && !opts.Quiet {
				fmt.Fprintln(stderr, "warning: "+data.stale)
			}
			share, err := resolveShareRef(data.manifest, args[0])
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			url, err := shareURL(data.baseURL, share, long)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			if opts.JSON {
				return output.WriteJSON(stdout, output.Success(urlJSON{URL: url}))
			}
			_, err = fmt.Fprintln(stdout, url)
			return err
		},
	}
	cmd.Flags().BoolVar(&long, "long", false, "print the long object-path URL instead of the short URL")
	return cmd
}

// shareURL returns the short URL (default) or the long object-path URL.
func shareURL(baseURL string, share *domain.Share, long bool) (string, error) {
	if long {
		if share.Path == nil {
			return "", output.NewError(output.CodeUsage, "share has no object-path endpoint", nil)
		}
		return domain.ObjectURL(baseURL, share.Path.Value), nil
	}
	if share.Slug == nil {
		return "", output.NewError(output.CodeUsage, "share has no slug endpoint", nil)
	}
	return domain.SlugURL(baseURL, share.Slug.Value, share.Slug.Ext), nil
}
