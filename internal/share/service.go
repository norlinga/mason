// Package share orchestrates the two-phase share workflow: ensure the blob exists
// on the server (hash locally, upload only when missing), then ensure the link
// endpoints exist via links.Service. It is the one share command that moves bytes.
package share

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/norlinga/mason/internal/config"
	"github.com/norlinga/mason/internal/domain"
	"github.com/norlinga/mason/internal/links"
	"github.com/norlinga/mason/internal/remote"
)

// Uploader transfers a local file to the remote host. remote.Rsync satisfies it.
type Uploader interface {
	Upload(ctx context.Context, request remote.RsyncRequest) (remote.ProcessResult, error)
}

// Linker creates or re-points a share's endpoints. *links.Service satisfies it.
type Linker interface {
	EnsureShare(ctx context.Context, req links.ShareRequest) (links.ShareResult, error)
}

// Service publishes local files as shares.
type Service struct {
	share    config.Share
	fs       remote.RemoteFS
	store    remote.ManifestStore
	uploader Uploader
	linker   Linker

	hashFile func(path string) (blob string, size int64, err error)
	now      func() time.Time
}

// NewService builds a share service for the configured share store.
func NewService(share config.Share, fs remote.RemoteFS, store remote.ManifestStore, uploader Uploader, linker Linker) *Service {
	return &Service{
		share:    share,
		fs:       fs,
		store:    store,
		uploader: uploader,
		linker:   linker,
		hashFile: hashLocalFile,
		now:      time.Now,
	}
}

// Request describes one share invocation.
type Request struct {
	// File is the local path to publish.
	File string
	// As is the raw --as value (may be empty to use the share store's inbox).
	As string
}

// Result reports the outcome of a share.
type Result struct {
	Action     domain.ShareAction
	Uploaded   bool
	ObjectPath string
	Slug       string
	Ext        string
	Hash       string
	Size       int64
	LongURL    string
	ShortURL   string
}

// Share runs the two-phase share for req.
func (s *Service) Share(ctx context.Context, req Request) (Result, error) {
	file := req.File
	if file == "" {
		return Result{}, fmt.Errorf("file is required")
	}

	objectPath, err := domain.ResolveSharePath(req.As, s.share.Inbox, filepath.Base(file))
	if err != nil {
		return Result{}, err
	}

	blob, size, err := s.hashFile(file)
	if err != nil {
		return Result{}, fmt.Errorf("hash %q: %w", file, err)
	}

	uploaded, err := s.ensureBlob(ctx, file, blob, size)
	if err != nil {
		return Result{}, err
	}

	linkRes, err := s.linker.EnsureShare(ctx, links.ShareRequest{Blob: blob, ObjectPath: objectPath})
	if err != nil {
		return Result{}, err
	}

	share := linkRes.Share
	result := Result{
		Action:     linkRes.Action,
		Uploaded:   uploaded,
		ObjectPath: objectPath,
		Hash:       blob,
		Size:       size,
		LongURL:    domain.ObjectURL(s.share.URL, objectPath),
	}
	if share.Slug != nil {
		result.Slug = share.Slug.Value
		result.Ext = share.Slug.Ext
		result.ShortURL = domain.SlugURL(s.share.URL, share.Slug.Value, share.Slug.Ext)
	}
	return result, nil
}

// ensureBlob is phase one: upload the file and record the blob only when it is not
// already present in the manifest.
func (s *Service) ensureBlob(ctx context.Context, file, blob string, size int64) (bool, error) {
	manifest, err := s.store.Fetch(ctx)
	if err != nil {
		return false, fmt.Errorf("fetch manifest: %w", err)
	}
	if _, ok := manifest.Blobs[blob]; ok {
		return false, nil
	}

	blobRel, err := domain.BlobPath(blob)
	if err != nil {
		return false, err
	}
	blobRemote := remote.RemotePath(s.share.Path, blobRel)

	if dir := filepath.ToSlash(filepath.Dir(blobRel)); dir != "." && dir != "/" {
		if err := s.fs.MkdirAll(ctx, remote.RemotePath(s.share.Path, dir)); err != nil {
			return false, fmt.Errorf("create blob dir: %w", err)
		}
	}

	sshOpts := remote.SSHOptionsFromServer(*s.share.Server)
	_, err = s.uploader.Upload(ctx, remote.RsyncRequest{
		Source:      file,
		Destination: remote.RsyncDestination(sshOpts, blobRemote),
		SSH:         &sshOpts,
	})
	if err != nil {
		return false, fmt.Errorf("upload blob: %w", err)
	}

	uploadedAt := s.now().UTC()
	if err := s.store.Update(ctx, func(m *domain.Manifest) error {
		if _, ok := m.Blobs[blob]; ok {
			return nil
		}
		m.Blobs[blob] = domain.Blob{Size: size, UploadedAt: uploadedAt}
		return nil
	}); err != nil {
		return false, fmt.Errorf("record blob: %w", err)
	}
	return true, nil
}

// hashLocalFile streams a file through SHA-256 and reports its blob key and size.
func hashLocalFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	blob, err := domain.HashReader(f)
	if err != nil {
		return "", 0, err
	}
	return blob, info.Size(), nil
}
