package links

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/norlinga/mason/internal/domain"
	"github.com/norlinga/mason/internal/remote"
)

const (
	testRoot  = "/srv/share"
	blobA     = "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	blobB     = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	blobAPath = "objects/2c/f2/2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	blobBPath = "objects/e3/b0/e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

// fakeFS records symlink and mkdir operations.
type fakeFS struct {
	mkdirs   []string
	removes  []string
	symlinks map[string]string
	entries  map[string]remote.RemoteEntry
	err      error
}

func newFakeFS() *fakeFS {
	return &fakeFS{
		symlinks: map[string]string{},
		entries:  map[string]remote.RemoteEntry{},
	}
}

func (f *fakeFS) MkdirAll(_ context.Context, p string) error {
	if f.err != nil {
		return f.err
	}
	f.mkdirs = append(f.mkdirs, p)
	return nil
}
func (f *fakeFS) Lstat(_ context.Context, p string) (remote.RemoteEntry, error) {
	if f.err != nil {
		return remote.RemoteEntry{}, f.err
	}
	if entry, ok := f.entries[p]; ok {
		return entry, nil
	}
	if target, ok := f.symlinks[p]; ok {
		return remote.RemoteEntry{Kind: remote.EntrySymlink, Target: target}, nil
	}
	return remote.RemoteEntry{Kind: remote.EntryMissing}, nil
}
func (f *fakeFS) Symlink(_ context.Context, link, target string) error {
	if f.err != nil {
		return f.err
	}
	f.symlinks[link] = target
	f.entries[link] = remote.RemoteEntry{Kind: remote.EntrySymlink, Target: target}
	return nil
}
func (f *fakeFS) Remove(_ context.Context, p string) error {
	if f.err != nil {
		return f.err
	}
	f.removes = append(f.removes, p)
	delete(f.symlinks, p)
	delete(f.entries, p)
	return nil
}
func (f *fakeFS) ReadFile(context.Context, string) ([]byte, error) { return nil, nil }
func (f *fakeFS) WriteFile(context.Context, string, []byte, remote.FileMode) error {
	return nil
}

// fakeStore holds a manifest in memory.
type fakeStore struct {
	manifest *domain.Manifest
	putErr   error
	puts     int
}

func (s *fakeStore) Fetch(context.Context) (*domain.Manifest, error) { return s.manifest, nil }
func (s *fakeStore) Put(_ context.Context, m *domain.Manifest) error {
	if s.putErr != nil {
		return s.putErr
	}
	s.manifest = m
	s.puts++
	return nil
}
func (s *fakeStore) Update(context.Context, func(*domain.Manifest) error) error { return nil }

func newService(t *testing.T, fs *fakeFS, store *fakeStore) *Service {
	t.Helper()
	svc := NewService(testRoot, fs, store)
	svc.now = func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	svc.newID = func() string { return "share-1" }
	svc.mint = func(reserved func(string) bool) (string, error) { return "abc123", nil }
	return svc
}

func manifestWithBlobs(blobs ...string) *domain.Manifest {
	m := domain.NewManifest()
	for _, b := range blobs {
		m.Blobs[b] = domain.Blob{Size: 5}
	}
	return m
}

func TestEnsureShareCreate(t *testing.T) {
	fs := newFakeFS()
	store := &fakeStore{manifest: manifestWithBlobs(blobA)}
	svc := newService(t, fs, store)

	res, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobA, ObjectPath: "inbox/foo.mp3"})
	if err != nil {
		t.Fatalf("EnsureShare: %v", err)
	}
	if res.Action != domain.ShareCreate {
		t.Fatalf("Action = %s, want create", res.Action)
	}

	wantLinks := map[string]string{
		"/srv/share/inbox/foo.mp3": "../" + blobAPath,
		"/srv/share/s/abc123.mp3":  "../" + blobAPath,
		"/srv/share/s/abc123":      "../" + blobAPath,
	}
	for link, target := range wantLinks {
		if got := fs.symlinks[link]; got != target {
			t.Fatalf("symlink %q = %q, want %q", link, got, target)
		}
	}
	if len(fs.symlinks) != len(wantLinks) {
		t.Fatalf("symlinks = %v, want %d entries", fs.symlinks, len(wantLinks))
	}

	if store.puts != 1 {
		t.Fatalf("manifest puts = %d, want 1", store.puts)
	}
	if len(store.manifest.Shares) != 1 {
		t.Fatalf("Shares = %d, want 1", len(store.manifest.Shares))
	}
	share := store.manifest.Shares[0]
	if share.ID != "share-1" || share.Blob != blobA {
		t.Fatalf("share = %#v, want id share-1 blob A", share)
	}
	if share.Slug == nil || share.Slug.Value != "abc123" || share.Slug.Ext != ".mp3" {
		t.Fatalf("slug = %#v, want abc123/.mp3", share.Slug)
	}
	if !store.manifest.SlugReserved("abc123") {
		t.Fatal("slug abc123 not reserved")
	}
}

func TestEnsureShareCreateNoExtensionSkipsBareSlug(t *testing.T) {
	fs := newFakeFS()
	store := &fakeStore{manifest: manifestWithBlobs(blobA)}
	svc := newService(t, fs, store)

	if _, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobA, ObjectPath: "inbox/README"}); err != nil {
		t.Fatalf("EnsureShare: %v", err)
	}

	if _, ok := fs.symlinks["/srv/share/s/abc123"]; !ok {
		t.Fatal("canonical extensionless slug link missing")
	}
	// With no extension the canonical and bare links coincide; only one slug link.
	slugLinks := 0
	for link := range fs.symlinks {
		if link == "/srv/share/s/abc123" {
			slugLinks++
		}
	}
	if slugLinks != 1 {
		t.Fatalf("slug links = %d, want 1", slugLinks)
	}
}

func TestEnsureShareCreateAtRoot(t *testing.T) {
	fs := newFakeFS()
	store := &fakeStore{manifest: manifestWithBlobs(blobA)}
	svc := newService(t, fs, store)

	if _, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobA, ObjectPath: "foo.mp3"}); err != nil {
		t.Fatalf("EnsureShare: %v", err)
	}
	if got := fs.symlinks["/srv/share/foo.mp3"]; got != blobAPath {
		t.Fatalf("root object symlink = %q, want %q", got, blobAPath)
	}
	for _, dir := range fs.mkdirs {
		if dir == "/srv/share" || dir == "/srv/share/." {
			t.Fatalf("unexpected mkdir for root object path: %q", dir)
		}
	}
}

func TestEnsureShareDeepPathTargetDepth(t *testing.T) {
	fs := newFakeFS()
	store := &fakeStore{manifest: manifestWithBlobs(blobA)}
	svc := newService(t, fs, store)

	if _, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobA, ObjectPath: "super/rad/podcasts/episode-01.mp3"}); err != nil {
		t.Fatalf("EnsureShare: %v", err)
	}
	want := "../../../" + blobAPath
	if got := fs.symlinks["/srv/share/super/rad/podcasts/episode-01.mp3"]; got != want {
		t.Fatalf("deep object symlink = %q, want %q", got, want)
	}
}

func TestEnsureShareNoop(t *testing.T) {
	store := &fakeStore{manifest: manifestWithBlobs(blobA)}
	store.manifest.Shares = append(store.manifest.Shares, domain.Share{
		ID:   "existing",
		Blob: blobA,
		Path: &domain.Endpoint{Value: "inbox/foo.mp3", State: domain.EndpointOK},
		Slug: &domain.SlugEndpoint{Value: "zzz999", Ext: ".mp3", State: domain.EndpointOK},
	})
	fs := newFakeFS()
	svc := newService(t, fs, store)

	res, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobA, ObjectPath: "inbox/foo.mp3"})
	if err != nil {
		t.Fatalf("EnsureShare: %v", err)
	}
	if res.Action != domain.ShareNoop {
		t.Fatalf("Action = %s, want noop", res.Action)
	}
	if len(fs.symlinks) != 0 {
		t.Fatalf("symlinks = %v, want none on noop", fs.symlinks)
	}
	if store.puts != 0 {
		t.Fatalf("puts = %d, want 0 on noop", store.puts)
	}
}

func TestEnsureShareRepoint(t *testing.T) {
	store := &fakeStore{manifest: manifestWithBlobs(blobA, blobB)}
	store.manifest.Shares = append(store.manifest.Shares, domain.Share{
		ID:   "existing",
		Blob: blobA,
		Path: &domain.Endpoint{Value: "inbox/foo.mp3", State: domain.EndpointOK},
		Slug: &domain.SlugEndpoint{Value: "zzz999", Ext: ".mp3", State: domain.EndpointOK},
	})
	fs := newFakeFS()
	svc := newService(t, fs, store)

	res, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobB, ObjectPath: "inbox/foo.mp3"})
	if err != nil {
		t.Fatalf("EnsureShare: %v", err)
	}
	if res.Action != domain.ShareRepoint {
		t.Fatalf("Action = %s, want repoint", res.Action)
	}
	if store.manifest.Shares[0].Blob != blobB {
		t.Fatalf("share blob = %q, want repointed to B", store.manifest.Shares[0].Blob)
	}
	if got := fs.symlinks["/srv/share/inbox/foo.mp3"]; got != "../"+blobBPath {
		t.Fatalf("object symlink = %q, want repointed to B", got)
	}
	if got := fs.symlinks["/srv/share/s/zzz999.mp3"]; got != "../"+blobBPath {
		t.Fatalf("slug symlink = %q, want repointed to B", got)
	}
	if store.puts != 1 {
		t.Fatalf("puts = %d, want 1", store.puts)
	}
}

func TestEnsureShareRequiresUploadedBlob(t *testing.T) {
	fs := newFakeFS()
	store := &fakeStore{manifest: domain.NewManifest()} // blob A absent
	svc := newService(t, fs, store)

	_, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobA, ObjectPath: "inbox/foo.mp3"})
	if err == nil {
		t.Fatal("EnsureShare err = nil, want missing-blob error")
	}
	if len(fs.symlinks) != 0 || store.puts != 0 {
		t.Fatal("missing-blob path mutated remote state")
	}
}

func TestEnsureShareCreateRejectsExistingRemoteObjectPath(t *testing.T) {
	fs := newFakeFS()
	fs.entries["/srv/share/inbox/foo.mp3"] = remote.RemoteEntry{Kind: remote.EntryFile}
	store := &fakeStore{manifest: manifestWithBlobs(blobA)}
	svc := newService(t, fs, store)

	_, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobA, ObjectPath: "inbox/foo.mp3"})
	if err == nil {
		t.Fatal("EnsureShare err = nil, want remote path collision")
	}
	if !strings.Contains(err.Error(), "already exists as a file") {
		t.Fatalf("err = %q, want file collision", err)
	}
	if len(fs.symlinks) != 0 || store.puts != 0 {
		t.Fatal("collision mutated remote links or manifest")
	}
}

func TestEnsureShareCreateRejectsExistingRemoteSlugBeforeMutation(t *testing.T) {
	fs := newFakeFS()
	fs.entries["/srv/share/s/abc123.mp3"] = remote.RemoteEntry{Kind: remote.EntryDirectory}
	store := &fakeStore{manifest: manifestWithBlobs(blobA)}
	svc := newService(t, fs, store)

	_, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobA, ObjectPath: "inbox/foo.mp3"})
	if err == nil {
		t.Fatal("EnsureShare err = nil, want remote slug collision")
	}
	if !strings.Contains(err.Error(), "already exists as a directory") {
		t.Fatalf("err = %q, want directory collision", err)
	}
	if len(fs.symlinks) != 0 {
		t.Fatalf("symlinks = %v, want none before rejected slug collision", fs.symlinks)
	}
	if store.puts != 0 {
		t.Fatal("manifest committed despite slug collision")
	}
}

func TestEnsureShareRejectsBadInputs(t *testing.T) {
	fs := newFakeFS()
	store := &fakeStore{manifest: manifestWithBlobs(blobA)}
	svc := newService(t, fs, store)

	if _, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: "not-a-blob", ObjectPath: "inbox/foo.mp3"}); err == nil {
		t.Fatal("invalid blob key err = nil, want error")
	}
	if _, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobA, ObjectPath: "../escape.mp3"}); err == nil {
		t.Fatal("traversal object path err = nil, want error")
	}
}

func TestEnsureShareSymlinkErrorPropagates(t *testing.T) {
	fs := newFakeFS()
	fs.err = errors.New("ssh failed")
	store := &fakeStore{manifest: manifestWithBlobs(blobA)}
	svc := newService(t, fs, store)

	if _, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobA, ObjectPath: "inbox/foo.mp3"}); err == nil {
		t.Fatal("EnsureShare err = nil on fs failure, want error")
	}
	if store.puts != 0 {
		t.Fatal("manifest committed despite symlink failure")
	}
}

func TestEnsureSharePutErrorPropagates(t *testing.T) {
	fs := newFakeFS()
	store := &fakeStore{manifest: manifestWithBlobs(blobA), putErr: errors.New("write failed")}
	svc := newService(t, fs, store)

	if _, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobA, ObjectPath: "inbox/foo.mp3"}); err == nil {
		t.Fatal("EnsureShare err = nil on put failure, want error")
	}
	if len(fs.symlinks) != 0 {
		t.Fatalf("symlinks = %v, want rollback to remove created links", fs.symlinks)
	}
	wantRemoves := map[string]bool{
		"/srv/share/inbox/foo.mp3": false,
		"/srv/share/s/abc123.mp3":  false,
		"/srv/share/s/abc123":      false,
	}
	for _, removed := range fs.removes {
		if _, ok := wantRemoves[removed]; ok {
			wantRemoves[removed] = true
		}
	}
	for path, removed := range wantRemoves {
		if !removed {
			t.Fatalf("rollback did not remove %q; removes = %v", path, fs.removes)
		}
	}
	if len(store.manifest.Shares) != 0 {
		t.Fatalf("manifest shares = %v, want restored after failed put", store.manifest.Shares)
	}
	if store.manifest.SlugReserved("abc123") {
		t.Fatal("slug reservation remained after failed create")
	}
}

func TestEnsureShareRepointPutErrorRollsBackSymlinksAndManifest(t *testing.T) {
	store := &fakeStore{manifest: manifestWithBlobs(blobA, blobB), putErr: errors.New("write failed")}
	store.manifest.Shares = append(store.manifest.Shares, domain.Share{
		ID:   "existing",
		Blob: blobA,
		Path: &domain.Endpoint{Value: "inbox/foo.mp3", State: domain.EndpointOK},
		Slug: &domain.SlugEndpoint{Value: "zzz999", Ext: ".mp3", State: domain.EndpointOK},
	})
	fs := newFakeFS()
	fs.symlinks["/srv/share/inbox/foo.mp3"] = "../" + blobAPath
	fs.symlinks["/srv/share/s/zzz999.mp3"] = "../" + blobAPath
	fs.symlinks["/srv/share/s/zzz999"] = "../" + blobAPath
	svc := newService(t, fs, store)

	if _, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobB, ObjectPath: "inbox/foo.mp3"}); err == nil {
		t.Fatal("EnsureShare err = nil on put failure, want error")
	}
	if store.manifest.Shares[0].Blob != blobA {
		t.Fatalf("manifest blob = %q, want restored to A", store.manifest.Shares[0].Blob)
	}
	wantLinks := map[string]string{
		"/srv/share/inbox/foo.mp3": "../" + blobAPath,
		"/srv/share/s/zzz999.mp3":  "../" + blobAPath,
		"/srv/share/s/zzz999":      "../" + blobAPath,
	}
	for link, target := range wantLinks {
		if got := fs.symlinks[link]; got != target {
			t.Fatalf("symlink %q = %q, want rolled back to %q", link, got, target)
		}
	}
}

func TestEnsureShareRequiresDependencies(t *testing.T) {
	store := &fakeStore{manifest: manifestWithBlobs(blobA)}
	if _, err := NewService(testRoot, nil, store).EnsureShare(context.Background(), ShareRequest{Blob: blobA, ObjectPath: "inbox/foo.mp3"}); err == nil {
		t.Fatal("nil fs err = nil, want error")
	}
	if _, err := NewService(testRoot, newFakeFS(), nil).EnsureShare(context.Background(), ShareRequest{Blob: blobA, ObjectPath: "inbox/foo.mp3"}); err == nil {
		t.Fatal("nil store err = nil, want error")
	}
}

func TestEnsureShareMintErrorPropagates(t *testing.T) {
	fs := newFakeFS()
	store := &fakeStore{manifest: manifestWithBlobs(blobA)}
	svc := newService(t, fs, store)
	svc.mint = func(func(string) bool) (string, error) { return "", errors.New("no slug") }

	if _, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobA, ObjectPath: "inbox/foo.mp3"}); err == nil {
		t.Fatal("mint error err = nil, want error")
	}
	if len(fs.symlinks) != 0 || store.puts != 0 {
		t.Fatal("mint failure mutated remote state")
	}
}

func TestEnsureShareRepointWithoutSlug(t *testing.T) {
	store := &fakeStore{manifest: manifestWithBlobs(blobA, blobB)}
	store.manifest.Shares = append(store.manifest.Shares, domain.Share{
		ID:   "existing",
		Blob: blobA,
		Path: &domain.Endpoint{Value: "inbox/foo.mp3", State: domain.EndpointOK},
		// slug endpoint removed
	})
	fs := newFakeFS()
	svc := newService(t, fs, store)

	res, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobB, ObjectPath: "inbox/foo.mp3"})
	if err != nil {
		t.Fatalf("EnsureShare: %v", err)
	}
	if res.Action != domain.ShareRepoint {
		t.Fatalf("Action = %s, want repoint", res.Action)
	}
	if got := fs.symlinks["/srv/share/inbox/foo.mp3"]; got != "../"+blobBPath {
		t.Fatalf("object symlink = %q, want repointed to B", got)
	}
	// No slug endpoint, so no slug symlinks are touched.
	for link := range fs.symlinks {
		if link == "/srv/share/s/zzz999.mp3" || link == "/srv/share/s/zzz999" {
			t.Fatalf("unexpected slug symlink %q", link)
		}
	}
}

func TestEnsureShareRepointDoesNotRelinkDisabledPath(t *testing.T) {
	store := &fakeStore{manifest: manifestWithBlobs(blobA, blobB)}
	store.manifest.Shares = append(store.manifest.Shares, domain.Share{
		ID:   "existing",
		Blob: blobA,
		Path: &domain.Endpoint{Value: "inbox/foo.mp3", State: domain.EndpointDisabled},
		Slug: &domain.SlugEndpoint{Value: "zzz999", Ext: ".mp3", State: domain.EndpointOK},
	})
	fs := newFakeFS()
	svc := newService(t, fs, store)

	res, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobB, ObjectPath: "inbox/foo.mp3"})
	if err != nil {
		t.Fatalf("EnsureShare: %v", err)
	}
	if res.Action != domain.ShareRepoint {
		t.Fatalf("Action = %s, want repoint", res.Action)
	}
	if store.manifest.Shares[0].Blob != blobB {
		t.Fatalf("share blob = %q, want repointed to B", store.manifest.Shares[0].Blob)
	}
	if _, ok := fs.symlinks["/srv/share/inbox/foo.mp3"]; ok {
		t.Fatal("disabled path endpoint was relinked")
	}
	if got := fs.symlinks["/srv/share/s/zzz999.mp3"]; got != "../"+blobBPath {
		t.Fatalf("enabled slug symlink = %q, want repointed to B", got)
	}
	if got := fs.symlinks["/srv/share/s/zzz999"]; got != "../"+blobBPath {
		t.Fatalf("enabled bare slug symlink = %q, want repointed to B", got)
	}
}

func TestEnsureShareRepointDoesNotRelinkDisabledSlug(t *testing.T) {
	store := &fakeStore{manifest: manifestWithBlobs(blobA, blobB)}
	store.manifest.Shares = append(store.manifest.Shares, domain.Share{
		ID:   "existing",
		Blob: blobA,
		Path: &domain.Endpoint{Value: "inbox/foo.mp3", State: domain.EndpointOK},
		Slug: &domain.SlugEndpoint{Value: "zzz999", Ext: ".mp3", State: domain.EndpointDisabled},
	})
	fs := newFakeFS()
	svc := newService(t, fs, store)

	res, err := svc.EnsureShare(context.Background(), ShareRequest{Blob: blobB, ObjectPath: "inbox/foo.mp3"})
	if err != nil {
		t.Fatalf("EnsureShare: %v", err)
	}
	if res.Action != domain.ShareRepoint {
		t.Fatalf("Action = %s, want repoint", res.Action)
	}
	if got := fs.symlinks["/srv/share/inbox/foo.mp3"]; got != "../"+blobBPath {
		t.Fatalf("enabled object symlink = %q, want repointed to B", got)
	}
	for link := range fs.symlinks {
		if link == "/srv/share/s/zzz999.mp3" || link == "/srv/share/s/zzz999" {
			t.Fatalf("disabled slug endpoint was relinked: %q", link)
		}
	}
}

func TestSymlinkTarget(t *testing.T) {
	tests := []struct {
		linkRel string
		want    string
	}{
		{"foo.mp3", blobAPath},
		{"inbox/foo.mp3", "../" + blobAPath},
		{"super/rad/podcasts/episode-01.mp3", "../../../" + blobAPath},
		{"s/abc123.mp3", "../" + blobAPath},
	}
	for _, tt := range tests {
		if got := symlinkTarget(tt.linkRel, blobAPath); got != tt.want {
			t.Fatalf("symlinkTarget(%q) = %q, want %q", tt.linkRel, got, tt.want)
		}
	}
}

func TestRandomIDUnique(t *testing.T) {
	a, b := randomID(), randomID()
	if a == b {
		t.Fatalf("randomID collision: %q", a)
	}
	if len(a) != 24 {
		t.Fatalf("randomID len = %d, want 24", len(a))
	}
}
