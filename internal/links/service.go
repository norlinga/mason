// Package links performs remote-link mutations: it creates and updates a share's
// object-path and slug symlinks and patches the authoritative manifest. It never
// moves blob bytes — the caller ensures the blob exists before linking.
package links

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/norlinga/mason/internal/domain"
	"github.com/norlinga/mason/internal/remote"
)

// slugDir is the site-relative directory holding short-slug symlinks.
const slugDir = "s"

// Service creates and updates share endpoints against a remote filesystem and the
// authoritative manifest.
type Service struct {
	root  string
	fs    remote.RemoteFS
	store remote.ManifestStore

	now   func() time.Time
	newID func() string
	mint  func(reserved func(string) bool) (string, error)
}

// NewService builds a link service rooted at the target's site root.
func NewService(root string, fs remote.RemoteFS, store remote.ManifestStore) *Service {
	return &Service{
		root:  strings.TrimSpace(root),
		fs:    fs,
		store: store,
		now:   time.Now,
		newID: randomID,
		mint:  domain.MintSlug,
	}
}

// ShareRequest names the blob (already uploaded) and the curated object path at
// which to publish it.
type ShareRequest struct {
	Blob       string
	ObjectPath string
}

// ShareResult reports the action taken and a snapshot of the resulting share.
type ShareResult struct {
	Action domain.ShareAction
	Share  domain.Share
}

// EnsureShare creates or re-points the endpoints for blob at objectPath and commits
// the manifest. The blob must already be present in the manifest (two-phase share:
// ensure blob, then ensure link). A path already pointing at the same blob is a no-op.
func (s *Service) EnsureShare(ctx context.Context, req ShareRequest) (ShareResult, error) {
	if s.fs == nil {
		return ShareResult{}, fmt.Errorf("remote filesystem is required")
	}
	if s.store == nil {
		return ShareResult{}, fmt.Errorf("manifest store is required")
	}

	objectPath, err := domain.NormalizeObjectPath(req.ObjectPath)
	if err != nil {
		return ShareResult{}, err
	}
	blob := strings.TrimSpace(req.Blob)
	blobPath, err := domain.BlobPath(blob)
	if err != nil {
		return ShareResult{}, err
	}

	manifest, err := s.store.Fetch(ctx)
	if err != nil {
		return ShareResult{}, fmt.Errorf("fetch manifest: %w", err)
	}
	if _, ok := manifest.Blobs[blob]; !ok {
		return ShareResult{}, fmt.Errorf("blob %q not found in manifest; upload it first", blob)
	}

	switch manifest.DecideShare(objectPath, blob).Action {
	case domain.ShareNoop:
		existing, _ := manifest.FindShareByPath(objectPath)
		return ShareResult{Action: domain.ShareNoop, Share: *existing}, nil
	case domain.ShareRepoint:
		return s.repoint(ctx, manifest, objectPath, blob, blobPath)
	default:
		return s.create(ctx, manifest, objectPath, blob, blobPath)
	}
}

func (s *Service) create(ctx context.Context, manifest *domain.Manifest, objectPath, blob, blobPath string) (ShareResult, error) {
	slug, err := s.mint(manifest.SlugReserved)
	if err != nil {
		return ShareResult{}, fmt.Errorf("mint slug: %w", err)
	}
	ext := path.Ext(objectPath)

	links := append(
		[]remote.SymlinkRequest{s.objectPathLink(objectPath, blobPath)},
		s.slugLinks(slug, ext, blobPath)...,
	)
	if err := s.ensureLinks(ctx, links); err != nil {
		return ShareResult{}, err
	}

	now := s.now().UTC()
	share := domain.Share{
		ID:        s.newID(),
		Blob:      blob,
		Path:      &domain.Endpoint{Value: objectPath, State: domain.EndpointOK},
		Slug:      &domain.SlugEndpoint{Value: slug, Ext: ext, State: domain.EndpointOK},
		CreatedAt: now,
		UpdatedAt: now,
	}
	manifest.Shares = append(manifest.Shares, share)
	manifest.ReserveSlug(slug)

	if err := s.store.Put(ctx, manifest); err != nil {
		return ShareResult{}, fmt.Errorf("write manifest: %w", err)
	}
	return ShareResult{Action: domain.ShareCreate, Share: share}, nil
}

func (s *Service) repoint(ctx context.Context, manifest *domain.Manifest, objectPath, blob, blobPath string) (ShareResult, error) {
	existing, ok := manifest.FindShareByPath(objectPath)
	if !ok {
		return ShareResult{}, fmt.Errorf("share for object path %q not found", objectPath)
	}

	var links []remote.SymlinkRequest
	if existing.Path != nil && existing.Path.State == domain.EndpointOK {
		links = append(links, s.objectPathLink(objectPath, blobPath))
	}
	if existing.Slug != nil && existing.Slug.State == domain.EndpointOK {
		links = append(links, s.slugLinks(existing.Slug.Value, existing.Slug.Ext, blobPath)...)
	}
	if err := s.ensureLinks(ctx, links); err != nil {
		return ShareResult{}, err
	}

	existing.Blob = blob
	existing.UpdatedAt = s.now().UTC()

	if err := s.store.Put(ctx, manifest); err != nil {
		return ShareResult{}, fmt.Errorf("write manifest: %w", err)
	}
	return ShareResult{Action: domain.ShareRepoint, Share: *existing}, nil
}

func (s *Service) ensureLinks(ctx context.Context, links []remote.SymlinkRequest) error {
	for _, link := range links {
		if err := remote.CheckSafeSymlink(ctx, s.fs, link); err != nil {
			return err
		}
	}
	for _, link := range links {
		if dir := path.Dir(link.LinkRel); dir != "." && dir != "/" {
			if err := s.fs.MkdirAll(ctx, remote.RemotePath(s.root, dir)); err != nil {
				return fmt.Errorf("create link dir: %w", err)
			}
		}
		if err := remote.EnsureSafeSymlink(ctx, s.fs, link); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) objectPathLink(objectPath, blobPath string) remote.SymlinkRequest {
	return remote.SymlinkRequest{
		LinkPath: remote.RemotePath(s.root, objectPath),
		LinkRel:  objectPath,
		Target:   symlinkTarget(objectPath, blobPath),
	}
}

func (s *Service) slugLinks(slug, ext, blobPath string) []remote.SymlinkRequest {
	canonical := path.Join(slugDir, slug+ext)
	links := []remote.SymlinkRequest{{
		LinkPath: remote.RemotePath(s.root, canonical),
		LinkRel:  canonical,
		Target:   symlinkTarget(canonical, blobPath),
	}}
	if ext != "" {
		bare := path.Join(slugDir, slug)
		links = append(links, remote.SymlinkRequest{
			LinkPath: remote.RemotePath(s.root, bare),
			LinkRel:  bare,
			Target:   symlinkTarget(bare, blobPath),
		})
	}
	return links
}

// symlinkTarget returns the blob path relative to the directory containing linkRel,
// so symlinks stay portable under the site root (e.g. inbox/foo.mp3 → ../objects/...).
func symlinkTarget(linkRel, blobRel string) string {
	dir := path.Dir(linkRel)
	depth := 0
	if trimmed := strings.Trim(dir, "/"); trimmed != "" && trimmed != "." {
		depth = len(strings.Split(trimmed, "/"))
	}
	return strings.Repeat("../", depth) + blobRel
}

// randomID returns a 24-char hex share identifier.
func randomID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%024x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
