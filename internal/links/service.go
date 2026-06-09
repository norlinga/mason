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

// MoveRequest re-homes a share's object-path endpoint to Dst. Src may name either
// endpoint (object path or s/slug); the slug endpoint is left unchanged.
type MoveRequest struct {
	Src string
	Dst string
}

// MoveResult reports the moved share and the object paths involved. OldPath is
// empty when the share had no object-path endpoint and one was recreated at Dst.
type MoveResult struct {
	Share   domain.Share
	OldPath string
	NewPath string
}

// Move re-homes a share's object-path endpoint to a new path without moving bytes.
// The slug endpoint is untouched. A disabled path endpoint keeps its disabled state
// (no symlink), and a share with no path endpoint gains a fresh active one at Dst.
func (s *Service) Move(ctx context.Context, req MoveRequest) (MoveResult, error) {
	if s.fs == nil {
		return MoveResult{}, fmt.Errorf("remote filesystem is required")
	}
	if s.store == nil {
		return MoveResult{}, fmt.Errorf("manifest store is required")
	}

	dst, err := domain.NormalizeObjectPath(req.Dst)
	if err != nil {
		return MoveResult{}, err
	}

	manifest, err := s.store.Fetch(ctx)
	if err != nil {
		return MoveResult{}, fmt.Errorf("fetch manifest: %w", err)
	}

	share, err := s.resolveRef(manifest, req.Src)
	if err != nil {
		return MoveResult{}, err
	}
	if other, ok := manifest.FindShareByPath(dst); ok && other.ID != share.ID {
		return MoveResult{}, fmt.Errorf("object path %q is already in use", dst)
	}

	blobPath, err := domain.BlobPath(share.Blob)
	if err != nil {
		return MoveResult{}, err
	}

	oldPath := ""
	state := domain.EndpointOK
	if share.Path != nil {
		oldPath = share.Path.Value
		state = share.Path.State
		if oldPath == dst {
			return MoveResult{Share: *share, OldPath: oldPath, NewPath: dst}, nil
		}
	}

	var newLinks []remote.SymlinkRequest
	if state == domain.EndpointOK {
		newLinks = []remote.SymlinkRequest{s.objectPathLink(dst, blobPath)}
	}
	if err := s.ensureLinks(ctx, newLinks); err != nil {
		return MoveResult{}, err
	}

	var oldLinks []remote.SymlinkRequest
	if oldPath != "" && state == domain.EndpointOK {
		oldLinks = []remote.SymlinkRequest{s.objectPathLink(oldPath, blobPath)}
		if err := s.removeLinks(ctx, oldLinks); err != nil {
			rollbackErr := s.removeLinks(ctx, newLinks)
			return MoveResult{}, manifestRollbackError(err, rollbackErr)
		}
	}

	prevPath := share.Path
	prevUpdatedAt := share.UpdatedAt
	share.Path = &domain.Endpoint{Value: dst, State: state}
	share.UpdatedAt = s.now().UTC()

	if err := s.store.Put(ctx, manifest); err != nil {
		share.Path = prevPath
		share.UpdatedAt = prevUpdatedAt
		rollbackErr := s.removeLinks(ctx, newLinks)
		if len(oldLinks) > 0 {
			if restoreErr := s.ensureLinks(ctx, oldLinks); restoreErr != nil && rollbackErr == nil {
				rollbackErr = restoreErr
			} else if restoreErr != nil {
				rollbackErr = fmt.Errorf("%v; restore old link: %w", rollbackErr, restoreErr)
			}
		}
		return MoveResult{}, manifestRollbackError(err, rollbackErr)
	}

	return MoveResult{Share: *share, OldPath: oldPath, NewPath: dst}, nil
}

// CopyRequest creates a new share at Dst pointing at the same blob as Src, with a
// freshly minted slug. Src may name either endpoint.
type CopyRequest struct {
	Src string
	Dst string
}

// CopyResult reports the newly created share.
type CopyResult struct {
	Share domain.Share
}

// Copy duplicates a share: it creates a new object-path endpoint and a new slug at
// Dst over the same blob as Src. No bytes are moved. Dst must be unused.
func (s *Service) Copy(ctx context.Context, req CopyRequest) (CopyResult, error) {
	if s.fs == nil {
		return CopyResult{}, fmt.Errorf("remote filesystem is required")
	}
	if s.store == nil {
		return CopyResult{}, fmt.Errorf("manifest store is required")
	}

	dst, err := domain.NormalizeObjectPath(req.Dst)
	if err != nil {
		return CopyResult{}, err
	}

	manifest, err := s.store.Fetch(ctx)
	if err != nil {
		return CopyResult{}, fmt.Errorf("fetch manifest: %w", err)
	}

	src, err := s.resolveRef(manifest, req.Src)
	if err != nil {
		return CopyResult{}, err
	}
	if _, ok := manifest.FindShareByPath(dst); ok {
		return CopyResult{}, fmt.Errorf("object path %q is already in use", dst)
	}

	blob := src.Blob
	blobPath, err := domain.BlobPath(blob)
	if err != nil {
		return CopyResult{}, err
	}

	res, err := s.create(ctx, manifest, dst, blob, blobPath)
	if err != nil {
		return CopyResult{}, err
	}
	return CopyResult{Share: res.Share}, nil
}

// RemoveRequest names the endpoint to remove via a ref (object path or s/slug).
// rm acts on the referenced endpoint, not the whole share.
type RemoveRequest struct {
	Ref string
}

// RemoveResult reports the outcome of removing one endpoint.
type RemoveResult struct {
	// Endpoint is "path" or "slug" — the endpoint that was removed.
	Endpoint string
	// ShareRemoved is true when the removed endpoint was the share's last one.
	ShareRemoved bool
	// BlobOrphaned is true when no manifest endpoint references the blob anymore.
	BlobOrphaned bool
	Blob         string
}

// Remove deletes the single endpoint named by req.Ref: the object-path endpoint for
// a path ref, or the slug endpoint for an s/slug ref. The share survives while its
// other endpoint exists; when both are gone the share is dropped. The slug stays in
// the reservation table so it is never auto-reused for different content.
func (s *Service) Remove(ctx context.Context, req RemoveRequest) (RemoveResult, error) {
	if s.fs == nil {
		return RemoveResult{}, fmt.Errorf("remote filesystem is required")
	}
	if s.store == nil {
		return RemoveResult{}, fmt.Errorf("manifest store is required")
	}

	parsed, err := domain.ParseRef(req.Ref)
	if err != nil {
		return RemoveResult{}, err
	}

	manifest, err := s.store.Fetch(ctx)
	if err != nil {
		return RemoveResult{}, fmt.Errorf("fetch manifest: %w", err)
	}

	share, err := s.resolveRef(manifest, req.Ref)
	if err != nil {
		return RemoveResult{}, err
	}

	blob := share.Blob
	blobPath, err := domain.BlobPath(blob)
	if err != nil {
		return RemoveResult{}, err
	}

	var (
		links    []remote.SymlinkRequest
		endpoint string
	)
	if parsed.IsSlug() {
		endpoint = "slug"
		links = s.slugLinks(share.Slug.Value, share.Slug.Ext, blobPath)
	} else {
		endpoint = "path"
		links = []remote.SymlinkRequest{s.objectPathLink(share.Path.Value, blobPath)}
	}

	previousShares := append([]domain.Share(nil), manifest.Shares...)
	if parsed.IsSlug() {
		share.Slug = nil
	} else {
		share.Path = nil
	}
	share.UpdatedAt = s.now().UTC()

	shareRemoved := false
	if share.Path == nil && share.Slug == nil {
		manifest.Shares = removeShareByID(manifest.Shares, share.ID)
		shareRemoved = true
	}

	if err := s.removeLinks(ctx, links); err != nil {
		manifest.Shares = previousShares
		return RemoveResult{}, err
	}

	if err := s.store.Put(ctx, manifest); err != nil {
		manifest.Shares = previousShares
		rollbackErr := s.ensureLinks(ctx, links)
		return RemoveResult{}, manifestRollbackError(err, rollbackErr)
	}

	return RemoveResult{
		Endpoint:     endpoint,
		ShareRemoved: shareRemoved,
		BlobOrphaned: manifest.BlobRefCount(blob) == 0,
		Blob:         blob,
	}, nil
}

// PruneRequest controls a prune run.
type PruneRequest struct {
	// Confirm performs deletion; when false, prune reports without changing anything.
	Confirm bool
}

// PrunedBlob describes one orphaned blob and its reclaimable size.
type PrunedBlob struct {
	Blob string
	Size int64
}

// PruneResult reports the orphaned blobs and whether they were deleted.
type PruneResult struct {
	Orphaned  []PrunedBlob
	TotalSize int64
	// Deleted is true when blobs were actually removed (Confirm and orphans present).
	Deleted bool
}

// Prune garbage-collects blobs with zero manifest endpoint references. It is dry-run
// unless req.Confirm is set: it always reports the orphaned blobs, and only when
// confirmed does it delete blob files and drop their manifest entries.
func (s *Service) Prune(ctx context.Context, req PruneRequest) (PruneResult, error) {
	if s.fs == nil {
		return PruneResult{}, fmt.Errorf("remote filesystem is required")
	}
	if s.store == nil {
		return PruneResult{}, fmt.Errorf("manifest store is required")
	}

	manifest, err := s.store.Fetch(ctx)
	if err != nil {
		return PruneResult{}, fmt.Errorf("fetch manifest: %w", err)
	}

	orphans := manifest.OrphanedBlobs()
	result := PruneResult{}
	for _, blob := range orphans {
		size := manifest.Blobs[blob].Size
		result.Orphaned = append(result.Orphaned, PrunedBlob{Blob: blob, Size: size})
		result.TotalSize += size
	}

	if !req.Confirm || len(orphans) == 0 {
		return result, nil
	}

	if err := s.store.Update(ctx, func(m *domain.Manifest) error {
		for _, blob := range orphans {
			// Re-check under RMW so a concurrently re-referenced blob is never dropped.
			if m.BlobRefCount(blob) == 0 {
				delete(m.Blobs, blob)
			}
		}
		return nil
	}); err != nil {
		return PruneResult{}, fmt.Errorf("write manifest: %w", err)
	}

	for _, blob := range orphans {
		rel, err := domain.BlobPath(blob)
		if err != nil {
			return PruneResult{}, err
		}
		if err := s.fs.Remove(ctx, remote.RemotePath(s.root, rel)); err != nil {
			return PruneResult{}, fmt.Errorf("remove blob %q: %w", blob, err)
		}
	}

	result.Deleted = true
	return result, nil
}

// EndpointRequest names a single endpoint to toggle via a ref (object path or s/slug).
type EndpointRequest struct {
	Ref string
}

// EndpointResult reports the toggled endpoint's kind and resulting state.
type EndpointResult struct {
	// Endpoint is "path" or "slug".
	Endpoint string
	State    domain.EndpointState
	// Changed is false when the endpoint was already in the requested state.
	Changed bool
}

// Disable removes the referenced endpoint's symlink(s) from the web tree and marks
// it disabled in the manifest so Caddy 404s it. The state is reversible (see Enable)
// and a disabled slug stays reserved. Disabling an already-disabled endpoint is a no-op.
func (s *Service) Disable(ctx context.Context, req EndpointRequest) (EndpointResult, error) {
	manifest, share, parsed, links, err := s.endpointLinks(ctx, req.Ref)
	if err != nil {
		return EndpointResult{}, err
	}
	kind := endpointKind(parsed)
	if endpointState(share, parsed) == domain.EndpointDisabled {
		return EndpointResult{Endpoint: kind, State: domain.EndpointDisabled}, nil
	}

	if err := s.removeLinks(ctx, links); err != nil {
		return EndpointResult{}, err
	}

	prevUpdatedAt := share.UpdatedAt
	setEndpointState(share, parsed, domain.EndpointDisabled)
	share.UpdatedAt = s.now().UTC()

	if err := s.store.Put(ctx, manifest); err != nil {
		setEndpointState(share, parsed, domain.EndpointOK)
		share.UpdatedAt = prevUpdatedAt
		rollbackErr := s.ensureLinks(ctx, links)
		return EndpointResult{}, manifestRollbackError(err, rollbackErr)
	}
	return EndpointResult{Endpoint: kind, State: domain.EndpointDisabled, Changed: true}, nil
}

// Enable recreates the referenced endpoint's symlink(s) and marks it ok in the
// manifest, the exact inverse of Disable. Enabling an already-enabled endpoint is a no-op.
func (s *Service) Enable(ctx context.Context, req EndpointRequest) (EndpointResult, error) {
	manifest, share, parsed, links, err := s.endpointLinks(ctx, req.Ref)
	if err != nil {
		return EndpointResult{}, err
	}
	kind := endpointKind(parsed)
	if endpointState(share, parsed) == domain.EndpointOK {
		return EndpointResult{Endpoint: kind, State: domain.EndpointOK}, nil
	}

	if err := s.ensureLinks(ctx, links); err != nil {
		return EndpointResult{}, err
	}

	prevUpdatedAt := share.UpdatedAt
	setEndpointState(share, parsed, domain.EndpointOK)
	share.UpdatedAt = s.now().UTC()

	if err := s.store.Put(ctx, manifest); err != nil {
		setEndpointState(share, parsed, domain.EndpointDisabled)
		share.UpdatedAt = prevUpdatedAt
		rollbackErr := s.removeLinks(ctx, links)
		return EndpointResult{}, manifestRollbackError(err, rollbackErr)
	}
	return EndpointResult{Endpoint: kind, State: domain.EndpointOK, Changed: true}, nil
}

// endpointLinks resolves a ref to its share and the symlink request(s) for the named
// endpoint. It validates that fs/store are present and the endpoint exists.
func (s *Service) endpointLinks(ctx context.Context, ref string) (*domain.Manifest, *domain.Share, domain.Ref, []remote.SymlinkRequest, error) {
	if s.fs == nil {
		return nil, nil, domain.Ref{}, nil, fmt.Errorf("remote filesystem is required")
	}
	if s.store == nil {
		return nil, nil, domain.Ref{}, nil, fmt.Errorf("manifest store is required")
	}
	parsed, err := domain.ParseRef(ref)
	if err != nil {
		return nil, nil, domain.Ref{}, nil, err
	}
	manifest, err := s.store.Fetch(ctx)
	if err != nil {
		return nil, nil, domain.Ref{}, nil, fmt.Errorf("fetch manifest: %w", err)
	}
	share, err := s.resolveRef(manifest, ref)
	if err != nil {
		return nil, nil, domain.Ref{}, nil, err
	}
	blobPath, err := domain.BlobPath(share.Blob)
	if err != nil {
		return nil, nil, domain.Ref{}, nil, err
	}
	var links []remote.SymlinkRequest
	if parsed.IsSlug() {
		links = s.slugLinks(share.Slug.Value, share.Slug.Ext, blobPath)
	} else {
		links = []remote.SymlinkRequest{s.objectPathLink(share.Path.Value, blobPath)}
	}
	return manifest, share, parsed, links, nil
}

func endpointKind(parsed domain.Ref) string {
	if parsed.IsSlug() {
		return "slug"
	}
	return "path"
}

func endpointState(share *domain.Share, parsed domain.Ref) domain.EndpointState {
	if parsed.IsSlug() {
		return share.Slug.State
	}
	return share.Path.State
}

func setEndpointState(share *domain.Share, parsed domain.Ref, state domain.EndpointState) {
	if parsed.IsSlug() {
		share.Slug.State = state
		return
	}
	share.Path.State = state
}

func removeShareByID(shares []domain.Share, id string) []domain.Share {
	out := make([]domain.Share, 0, len(shares))
	for _, sh := range shares {
		if sh.ID == id {
			continue
		}
		out = append(out, sh)
	}
	return out
}

// resolveRef finds the share named by ref (object path or s/slug) in manifest.
func (s *Service) resolveRef(manifest *domain.Manifest, ref string) (*domain.Share, error) {
	parsed, err := domain.ParseRef(ref)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("no share found for %q", ref)
	}
	return share, nil
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

	previousShares := append([]domain.Share(nil), manifest.Shares...)
	previousReservedSlugs := append([]string(nil), manifest.ReservedSlugs...)
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
		manifest.Shares = previousShares
		manifest.ReservedSlugs = previousReservedSlugs
		rollbackErr := s.removeLinks(ctx, links)
		return ShareResult{}, manifestRollbackError(err, rollbackErr)
	}
	return ShareResult{Action: domain.ShareCreate, Share: share}, nil
}

func (s *Service) repoint(ctx context.Context, manifest *domain.Manifest, objectPath, blob, blobPath string) (ShareResult, error) {
	existing, ok := manifest.FindShareByPath(objectPath)
	if !ok {
		return ShareResult{}, fmt.Errorf("share for object path %q not found", objectPath)
	}

	oldBlob := existing.Blob
	oldUpdatedAt := existing.UpdatedAt
	oldBlobPath, err := domain.BlobPath(oldBlob)
	if err != nil {
		return ShareResult{}, err
	}

	links := s.activeEndpointLinks(existing, blobPath)
	rollbackLinks := s.activeEndpointLinks(existing, oldBlobPath)
	if err := s.ensureLinks(ctx, links); err != nil {
		return ShareResult{}, err
	}

	existing.Blob = blob
	existing.UpdatedAt = s.now().UTC()

	if err := s.store.Put(ctx, manifest); err != nil {
		existing.Blob = oldBlob
		existing.UpdatedAt = oldUpdatedAt
		rollbackErr := s.ensureLinks(ctx, rollbackLinks)
		return ShareResult{}, manifestRollbackError(err, rollbackErr)
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

func (s *Service) removeLinks(ctx context.Context, links []remote.SymlinkRequest) error {
	for _, link := range links {
		if err := s.fs.Remove(ctx, link.LinkPath); err != nil {
			return fmt.Errorf("remove link %q: %w", link.LinkPath, err)
		}
	}
	return nil
}

func (s *Service) activeEndpointLinks(share *domain.Share, blobPath string) []remote.SymlinkRequest {
	var links []remote.SymlinkRequest
	if share.Path != nil && share.Path.State == domain.EndpointOK {
		links = append(links, s.objectPathLink(share.Path.Value, blobPath))
	}
	if share.Slug != nil && share.Slug.State == domain.EndpointOK {
		links = append(links, s.slugLinks(share.Slug.Value, share.Slug.Ext, blobPath)...)
	}
	return links
}

func manifestRollbackError(commitErr, rollbackErr error) error {
	if rollbackErr == nil {
		return fmt.Errorf("write manifest: %w", commitErr)
	}
	return fmt.Errorf("write manifest: %w; rollback failed: %v", commitErr, rollbackErr)
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
