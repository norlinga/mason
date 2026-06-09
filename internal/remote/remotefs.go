package remote

import (
	"context"
	"encoding/base64"
	"fmt"
	"path"
	"strings"

	"github.com/norlinga/mason/internal/domain"
)

// RemoteFS performs filesystem operations on a remote host.
type RemoteFS interface {
	MkdirAll(ctx context.Context, remotePath string) error
	Lstat(ctx context.Context, remotePath string) (RemoteEntry, error)
	Symlink(ctx context.Context, linkPath, target string) error
	Remove(ctx context.Context, remotePath string) error
	ReadFile(ctx context.Context, remotePath string) ([]byte, error)
	WriteFile(ctx context.Context, remotePath string, data []byte, mode FileMode) error
}

// FileMode is a remote file permission bitmask (e.g. 0o644).
type FileMode uint32

const defaultFileMode FileMode = 0o644

// EntryKind classifies a remote filesystem path without following symlinks.
type EntryKind string

const (
	EntryMissing   EntryKind = "missing"
	EntryFile      EntryKind = "file"
	EntryDirectory EntryKind = "directory"
	EntrySymlink   EntryKind = "symlink"
	EntryOther     EntryKind = "other"
)

// RemoteEntry describes one remote path. Target is set for symlinks.
type RemoteEntry struct {
	Kind   EntryKind
	Target string
}

// SymlinkRequest describes one Mason-managed symlink mutation.
type SymlinkRequest struct {
	// LinkPath is the absolute remote path to create or replace.
	LinkPath string
	// LinkRel is the site-root-relative path for LinkPath. It is used to decide
	// whether an existing symlink already points at Mason's object store.
	LinkRel string
	// Target is the symlink target to write, relative to LinkRel's parent.
	Target string
}

// PathCollisionError reports that Mason would overwrite a non-Mason path.
type PathCollisionError struct {
	Path   string
	Entry  RemoteEntry
	Detail string
}

func (e PathCollisionError) Error() string {
	if e.Entry.Kind == EntrySymlink {
		return fmt.Sprintf("remote path %q already exists as a symlink to %q; refusing to replace non-Mason symlink", e.Path, e.Entry.Target)
	}
	if e.Detail != "" {
		return fmt.Sprintf("remote path %q already exists as %s; %s", e.Path, article(e.Entry.Kind), e.Detail)
	}
	return fmt.Sprintf("remote path %q already exists as %s; refusing to overwrite it", e.Path, article(e.Entry.Kind))
}

// SSHFS implements RemoteFS over SSH shell commands.
type SSHFS struct {
	runner CommandRunner
}

// NewSSHFS creates a remote filesystem backed by SSH commands.
func NewSSHFS(runner CommandRunner) *SSHFS {
	return &SSHFS{runner: runner}
}

// MkdirAll creates a remote directory and any missing parents.
func (f *SSHFS) MkdirAll(ctx context.Context, remotePath string) error {
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		return fmt.Errorf("remote path is required")
	}

	_, err := f.runner.Run(ctx, fmt.Sprintf("mkdir -p %s", shellQuote(remotePath)))
	return err
}

// Lstat reports the remote path kind without following symlinks.
func (f *SSHFS) Lstat(ctx context.Context, remotePath string) (RemoteEntry, error) {
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		return RemoteEntry{}, fmt.Errorf("remote path is required")
	}

	result, err := f.runner.Run(ctx, lstatCommand(remotePath))
	if err != nil {
		return RemoteEntry{}, err
	}
	entry, err := parseRemoteEntry(result.Stdout)
	if err != nil {
		return RemoteEntry{}, fmt.Errorf("parse remote path metadata for %q: %w", remotePath, err)
	}
	return entry, nil
}

// Symlink creates or replaces a symbolic link at linkPath pointing to target. It
// refuses to overwrite non-symlink paths; use CheckSafeSymlink before replacing
// an existing symlink so Mason does not clobber a user-owned link.
func (f *SSHFS) Symlink(ctx context.Context, linkPath, target string) error {
	linkPath = strings.TrimSpace(linkPath)
	target = strings.TrimSpace(target)
	if linkPath == "" {
		return fmt.Errorf("symlink path is required")
	}
	if target == "" {
		return fmt.Errorf("symlink target is required")
	}

	_, err := f.runner.Run(ctx, symlinkCommand(linkPath, target))
	return err
}

// Remove deletes a remote file or symlink.
func (f *SSHFS) Remove(ctx context.Context, remotePath string) error {
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		return fmt.Errorf("remote path is required")
	}

	_, err := f.runner.Run(ctx, fmt.Sprintf("rm -f %s", shellQuote(remotePath)))
	return err
}

// ReadFile returns the contents of a remote file.
func (f *SSHFS) ReadFile(ctx context.Context, remotePath string) ([]byte, error) {
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		return nil, fmt.Errorf("remote path is required")
	}

	result, err := f.runner.Run(ctx, fmt.Sprintf("cat %s", shellQuote(remotePath)))
	if err != nil {
		return nil, err
	}
	return []byte(result.Stdout), nil
}

// WriteFile writes data to a remote file, creating parent directories as needed.
func (f *SSHFS) WriteFile(ctx context.Context, remotePath string, data []byte, mode FileMode) error {
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		return fmt.Errorf("remote path is required")
	}
	if mode == 0 {
		mode = defaultFileMode
	}

	dir := path.Dir(remotePath)
	if dir != "." && dir != "/" {
		if err := f.MkdirAll(ctx, dir); err != nil {
			return err
		}
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	command := fmt.Sprintf(
		"printf %%s %s | base64 -d > %s && chmod %s %s",
		shellQuote(encoded),
		shellQuote(remotePath),
		formatFileMode(mode),
		shellQuote(remotePath),
	)
	_, err := f.runner.Run(ctx, command)
	return err
}

// CheckSafeSymlink rejects collisions before Mason writes a symlink. It permits
// missing paths and existing symlinks that already target Mason's object store.
func CheckSafeSymlink(ctx context.Context, fs RemoteFS, request SymlinkRequest) error {
	request, err := normalizeSymlinkRequest(request)
	if err != nil {
		return err
	}

	entry, err := fs.Lstat(ctx, request.LinkPath)
	if err != nil {
		return fmt.Errorf("check remote path %q: %w", request.LinkPath, err)
	}
	return validateSafeSymlinkCollision(request, entry)
}

// EnsureSafeSymlink checks then writes one Mason-managed symlink.
func EnsureSafeSymlink(ctx context.Context, fs RemoteFS, request SymlinkRequest) error {
	request, err := normalizeSymlinkRequest(request)
	if err != nil {
		return err
	}
	if err := CheckSafeSymlink(ctx, fs, request); err != nil {
		return err
	}
	if err := fs.Symlink(ctx, request.LinkPath, request.Target); err != nil {
		return fmt.Errorf("write symlink %q: %w", request.LinkPath, err)
	}
	return nil
}

// RemotePath joins a target root with a relative remote path.
func RemotePath(root, rel string) string {
	root = strings.TrimSpace(root)
	rel = strings.TrimLeft(strings.TrimSpace(rel), "/")
	if root == "" {
		return rel
	}
	root = strings.TrimRight(root, "/")
	if root == "" {
		root = "/"
	}
	if rel == "" {
		return root
	}
	if root == "/" {
		return root + rel
	}
	return root + "/" + rel
}

func normalizeSymlinkRequest(request SymlinkRequest) (SymlinkRequest, error) {
	request.LinkPath = strings.TrimSpace(request.LinkPath)
	request.LinkRel = strings.Trim(strings.TrimSpace(request.LinkRel), "/")
	request.Target = strings.TrimSpace(request.Target)
	if request.LinkPath == "" {
		return SymlinkRequest{}, fmt.Errorf("symlink path is required")
	}
	if request.LinkRel == "" {
		return SymlinkRequest{}, fmt.Errorf("symlink relative path is required")
	}
	if request.Target == "" {
		return SymlinkRequest{}, fmt.Errorf("symlink target is required")
	}
	return request, nil
}

func validateSafeSymlinkCollision(request SymlinkRequest, entry RemoteEntry) error {
	switch entry.Kind {
	case EntryMissing:
		return nil
	case EntrySymlink:
		if isManagedSymlinkTarget(request.LinkRel, entry.Target) {
			return nil
		}
		return PathCollisionError{Path: request.LinkPath, Entry: entry}
	case EntryFile, EntryDirectory, EntryOther:
		return PathCollisionError{Path: request.LinkPath, Entry: entry}
	default:
		return fmt.Errorf("remote path %q has unknown kind %q", request.LinkPath, entry.Kind)
	}
}

func isManagedSymlinkTarget(linkRel, target string) bool {
	resolved := path.Clean(path.Join(path.Dir(linkRel), target))
	parts := strings.Split(resolved, "/")
	if len(parts) != 4 || parts[0] != "objects" {
		return false
	}

	digest := parts[3]
	if len(parts[1]) != 2 || len(parts[2]) != 2 {
		return false
	}
	if digest == "" || !strings.HasPrefix(digest, parts[1]+parts[2]) {
		return false
	}
	_, err := domain.BlobHex(domain.BlobHashPrefix + digest)
	return err == nil
}

func parseRemoteEntry(stdout string) (RemoteEntry, error) {
	line := strings.TrimRight(stdout, "\n")
	if kind, target, ok := strings.Cut(line, "\t"); ok {
		if EntryKind(kind) != EntrySymlink {
			return RemoteEntry{}, fmt.Errorf("unexpected metadata %q", line)
		}
		if target == "" {
			return RemoteEntry{}, fmt.Errorf("symlink target is empty")
		}
		return RemoteEntry{Kind: EntrySymlink, Target: target}, nil
	}

	switch EntryKind(strings.TrimSpace(line)) {
	case EntryMissing:
		return RemoteEntry{Kind: EntryMissing}, nil
	case EntryFile:
		return RemoteEntry{Kind: EntryFile}, nil
	case EntryDirectory:
		return RemoteEntry{Kind: EntryDirectory}, nil
	case EntryOther:
		return RemoteEntry{Kind: EntryOther}, nil
	default:
		return RemoteEntry{}, fmt.Errorf("unknown metadata %q", stdout)
	}
}

func lstatCommand(remotePath string) string {
	quoted := shellQuote(remotePath)
	return fmt.Sprintf(
		"if [ -L %s ]; then printf 'symlink\\t'; readlink %s; elif [ -e %s ]; then if [ -d %s ]; then printf 'directory\\n'; elif [ -f %s ]; then printf 'file\\n'; else printf 'other\\n'; fi; else printf 'missing\\n'; fi",
		quoted,
		quoted,
		quoted,
		quoted,
		quoted,
	)
}

func symlinkCommand(linkPath, target string) string {
	quotedLink := shellQuote(linkPath)
	return fmt.Sprintf(
		"if [ -e %s ] && [ ! -L %s ]; then printf 'refusing to overwrite non-symlink: %%s\\n' %s >&2; exit 1; fi; ln -sfn %s %s",
		quotedLink,
		quotedLink,
		quotedLink,
		shellQuote(target),
		quotedLink,
	)
}

func article(kind EntryKind) string {
	switch kind {
	case EntryDirectory:
		return "a directory"
	case EntryOther:
		return "an unsupported filesystem entry"
	default:
		return "a " + string(kind)
	}
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func formatFileMode(mode FileMode) string {
	return fmt.Sprintf("%o", mode&0o777)
}
