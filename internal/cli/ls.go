package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/norlinga/mason/internal/domain"
	"github.com/norlinga/mason/internal/output"
	"github.com/spf13/cobra"
)

const emptyCell = "—"

// lsRunFunc returns the cached manifest and the target base URL for listing.
type lsRunFunc func(ctx context.Context, root rootOptions) (lsData, error)

type lsData struct {
	manifest *domain.Manifest
	baseURL  string
}

type lsShareJSON struct {
	ObjectPath string `json:"object_path"`
	PathState  string `json:"path_state"`
	Slug       string `json:"slug"`
	Ext        string `json:"ext"`
	SlugState  string `json:"slug_state"`
	Hash       string `json:"hash"`
	Size       int64  `json:"size"`
	CreatedAt  string `json:"created_at"`
	LongURL    string `json:"long_url"`
	ShortURL   string `json:"short_url"`
}

type shareRow struct {
	path      string
	pathState string
	slug      string
	ext       string
	slugState string
	blob      string
	size      int64
	createdAt time.Time
	longURL   string
	shortURL  string
}

func newLsCommand(stdout, stderr io.Writer, run lsRunFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "ls [prefix]",
		Short: "List shares",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			data, err := run(cmd.Context(), root)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			prefix := ""
			if len(args) == 1 {
				prefix = args[0]
			}
			return renderLs(stdout, opts, data, prefix)
		},
	}
}

func renderLs(stdout io.Writer, opts output.Options, data lsData, prefix string) error {
	rows := buildShareRows(data, prefix)

	if opts.JSON {
		items := make([]lsShareJSON, 0, len(rows))
		for _, r := range rows {
			items = append(items, lsShareJSON{
				ObjectPath: r.path,
				PathState:  r.pathState,
				Slug:       r.slug,
				Ext:        r.ext,
				SlugState:  r.slugState,
				Hash:       r.blob,
				Size:       r.size,
				CreatedAt:  r.createdAt.UTC().Format(time.RFC3339),
				LongURL:    r.longURL,
				ShortURL:   r.shortURL,
			})
		}
		return output.WriteJSON(stdout, output.Success(items))
	}

	if len(rows) == 0 {
		if opts.Quiet {
			return nil
		}
		_, err := fmt.Fprintln(stdout, "No shares.")
		return err
	}

	headers := []string{"PATH", "SLUG", "SIZE", "HASH", "SHARED", "PATH_STATE", "SLUG_STATE"}
	grid := make([][]string, 0, len(rows))
	for _, r := range rows {
		grid = append(grid, []string{
			orDash(r.path),
			orDash(r.slug),
			output.HumanSize(r.size),
			shortHash(r.blob),
			r.createdAt.UTC().Format("2006-01-02"),
			orDash(r.pathState),
			orDash(r.slugState),
		})
	}
	return renderTable(stdout, headers, grid)
}

// buildShareRows filters by object-path prefix and sorts deterministically.
func buildShareRows(data lsData, prefix string) []shareRow {
	rows := make([]shareRow, 0)
	if data.manifest == nil {
		return rows
	}
	prefix = strings.TrimSpace(prefix)
	for i := range data.manifest.Shares {
		share := &data.manifest.Shares[i]
		path := ""
		if share.Path != nil {
			path = share.Path.Value
		}
		if prefix != "" {
			if path == "" || !strings.HasPrefix(path, prefix) {
				continue
			}
		}
		row := shareRow{
			path:      path,
			blob:      share.Blob,
			size:      data.manifest.Blobs[share.Blob].Size,
			createdAt: share.CreatedAt,
			longURL:   urlFor(data.baseURL, path),
		}
		if share.Path != nil {
			row.pathState = string(share.Path.State)
		}
		if share.Slug != nil {
			row.slug = share.Slug.Value
			row.ext = share.Slug.Ext
			row.slugState = string(share.Slug.State)
			row.shortURL = domain.SlugURL(data.baseURL, share.Slug.Value, share.Slug.Ext)
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].sortKey() != rows[j].sortKey() {
			return rows[i].sortKey() < rows[j].sortKey()
		}
		return rows[i].slug < rows[j].slug
	})
	return rows
}

// sortKey orders by object path, pushing path-less shares to the end.
func (r shareRow) sortKey() string {
	if r.path == "" {
		return "~" + r.slug
	}
	return r.path
}

func urlFor(baseURL, path string) string {
	if path == "" {
		return ""
	}
	return domain.ObjectURL(baseURL, path)
}

func orDash(value string) string {
	if value == "" {
		return emptyCell
	}
	return value
}

func shortHash(blob string) string {
	hex, err := domain.BlobHex(blob)
	if err != nil {
		return blob
	}
	return hex[:6] + "…"
}

// defaultLsRun best-effort syncs the cache from remote, then lists from cache.
func defaultLsRun(ctx context.Context, root rootOptions) (lsData, error) {
	resolved, err := resolveTarget(root)
	if err != nil {
		return lsData{}, err
	}
	stack, err := newRemoteStack(resolved)
	if err != nil {
		return lsData{}, err
	}
	_, _ = remoteSync(ctx, stack, resolved.Name) // best-effort; cache still readable offline

	manifest, ok, err := stack.cache.Read(resolved.Name)
	if err != nil {
		return lsData{}, err
	}
	if !ok {
		manifest = domain.NewManifest()
	}
	return lsData{manifest: manifest, baseURL: resolved.Target.BaseURL}, nil
}

// renderTable prints a left-aligned columnar grid with two-space gaps.
func renderTable(w io.Writer, headers []string, rows [][]string) error {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if l := utf8.RuneCountInString(cell); l > widths[i] {
				widths[i] = l
			}
		}
	}

	writeRow := func(cells []string) error {
		var b strings.Builder
		for i, cell := range cells {
			b.WriteString(cell)
			if i < len(cells)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell)+2))
			}
		}
		_, err := fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
		return err
	}

	if err := writeRow(headers); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writeRow(row); err != nil {
			return err
		}
	}
	return nil
}
