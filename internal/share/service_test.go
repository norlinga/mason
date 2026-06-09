package share

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/norlinga/mason/internal/config"
	"github.com/norlinga/mason/internal/domain"
	"github.com/norlinga/mason/internal/links"
	"github.com/norlinga/mason/internal/remote"
)

// "hello" hashes to this blob; size 5.
const (
	helloBlob     = "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	helloBlobPath = "objects/2c/f2/2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
)

type fakeFS struct{ mkdirs []string }

func (f *fakeFS) MkdirAll(_ context.Context, p string) error {
	f.mkdirs = append(f.mkdirs, p)
	return nil
}
func (f *fakeFS) Lstat(context.Context, string) (remote.RemoteEntry, error) {
	return remote.RemoteEntry{Kind: remote.EntryMissing}, nil
}
func (f *fakeFS) Symlink(context.Context, string, string) error                    { return nil }
func (f *fakeFS) Remove(context.Context, string) error                             { return nil }
func (f *fakeFS) ReadFile(context.Context, string) ([]byte, error)                 { return nil, nil }
func (f *fakeFS) WriteFile(context.Context, string, []byte, remote.FileMode) error { return nil }

type fakeStore struct {
	manifest *domain.Manifest
	updates  int
}

func (s *fakeStore) Fetch(context.Context) (*domain.Manifest, error) { return s.manifest, nil }
func (s *fakeStore) Put(_ context.Context, m *domain.Manifest) error { s.manifest = m; return nil }
func (s *fakeStore) Update(_ context.Context, mutate func(*domain.Manifest) error) error {
	s.updates++
	return mutate(s.manifest)
}

type fakeUploader struct {
	requests []remote.RsyncRequest
	err      error
}

func (u *fakeUploader) Upload(_ context.Context, req remote.RsyncRequest) (remote.ProcessResult, error) {
	u.requests = append(u.requests, req)
	if u.err != nil {
		return remote.ProcessResult{ExitCode: -1}, u.err
	}
	return remote.ProcessResult{}, nil
}

type fakeLinker struct {
	got    links.ShareRequest
	result links.ShareResult
	err    error
}

func (l *fakeLinker) EnsureShare(_ context.Context, req links.ShareRequest) (links.ShareResult, error) {
	l.got = req
	if l.err != nil {
		return links.ShareResult{}, l.err
	}
	return l.result, nil
}

func testTarget() config.Target {
	return config.Target{
		Host:    "host.example",
		User:    "mason",
		Root:    "/srv/share",
		BaseURL: "https://share.example.com",
		Share:   config.ShareConfig{DefaultPath: "inbox/"},
	}
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "foo.mp3")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func linkResult(objectPath, slug, ext string) links.ShareResult {
	return links.ShareResult{
		Action: domain.ShareCreate,
		Share: domain.Share{
			ID:   "share-1",
			Blob: helloBlob,
			Path: &domain.Endpoint{Value: objectPath, State: domain.EndpointOK},
			Slug: &domain.SlugEndpoint{Value: slug, Ext: ext, State: domain.EndpointOK},
		},
	}
}

func TestShareUploadsWhenBlobMissing(t *testing.T) {
	file := writeFile(t, "hello")
	fs := &fakeFS{}
	store := &fakeStore{manifest: domain.NewManifest()}
	uploader := &fakeUploader{}
	linker := &fakeLinker{result: linkResult("demos/test.mp3", "abc123", ".mp3")}
	svc := NewService(testTarget(), fs, store, uploader, linker)

	res, err := svc.Share(context.Background(), Request{File: file, As: "demos/test.mp3"})
	if err != nil {
		t.Fatalf("Share: %v", err)
	}

	if !res.Uploaded {
		t.Fatal("Uploaded = false, want true when blob missing")
	}
	if len(uploader.requests) != 1 {
		t.Fatalf("upload count = %d, want 1", len(uploader.requests))
	}
	wantDest := "mason@host.example:/srv/share/" + helloBlobPath
	if got := uploader.requests[0].Destination; got != wantDest {
		t.Fatalf("upload dest = %q, want %q", got, wantDest)
	}
	if uploader.requests[0].Source != file {
		t.Fatalf("upload source = %q, want %q", uploader.requests[0].Source, file)
	}
	if _, ok := store.manifest.Blobs[helloBlob]; !ok {
		t.Fatal("blob not recorded in manifest after upload")
	}
	if store.manifest.Blobs[helloBlob].Size != 5 {
		t.Fatalf("recorded size = %d, want 5", store.manifest.Blobs[helloBlob].Size)
	}
	if linker.got.ObjectPath != "demos/test.mp3" || linker.got.Blob != helloBlob {
		t.Fatalf("linker req = %#v, want demos/test.mp3 + hello blob", linker.got)
	}
	if res.LongURL != "https://share.example.com/demos/test.mp3" {
		t.Fatalf("LongURL = %q", res.LongURL)
	}
	if res.ShortURL != "https://share.example.com/s/abc123.mp3" {
		t.Fatalf("ShortURL = %q", res.ShortURL)
	}
	// Blob dir created before upload.
	foundDir := false
	for _, d := range fs.mkdirs {
		if d == "/srv/share/objects/2c/f2" {
			foundDir = true
		}
	}
	if !foundDir {
		t.Fatalf("blob dir not created; mkdirs = %v", fs.mkdirs)
	}
}

func TestShareSkipsUploadWhenBlobPresent(t *testing.T) {
	file := writeFile(t, "hello")
	fs := &fakeFS{}
	manifest := domain.NewManifest()
	manifest.Blobs[helloBlob] = domain.Blob{Size: 5}
	store := &fakeStore{manifest: manifest}
	uploader := &fakeUploader{}
	linker := &fakeLinker{result: linkResult("demos/test.mp3", "abc123", ".mp3")}
	svc := NewService(testTarget(), fs, store, uploader, linker)

	res, err := svc.Share(context.Background(), Request{File: file, As: "demos/test.mp3"})
	if err != nil {
		t.Fatalf("Share: %v", err)
	}
	if res.Uploaded {
		t.Fatal("Uploaded = true, want false when blob present")
	}
	if len(uploader.requests) != 0 {
		t.Fatalf("upload count = %d, want 0", len(uploader.requests))
	}
	if store.updates != 0 {
		t.Fatalf("manifest updates = %d, want 0 (no blob record needed)", store.updates)
	}
}

func TestShareUsesDefaultPathWhenNoAs(t *testing.T) {
	file := writeFile(t, "hello")
	store := &fakeStore{manifest: domain.NewManifest()}
	linker := &fakeLinker{result: linkResult("inbox/foo.mp3", "abc123", ".mp3")}
	svc := NewService(testTarget(), &fakeFS{}, store, &fakeUploader{}, linker)

	if _, err := svc.Share(context.Background(), Request{File: file}); err != nil {
		t.Fatalf("Share: %v", err)
	}
	if linker.got.ObjectPath != "inbox/foo.mp3" {
		t.Fatalf("object path = %q, want inbox/foo.mp3 from default_path", linker.got.ObjectPath)
	}
}

func TestShareMissingFile(t *testing.T) {
	svc := NewService(testTarget(), &fakeFS{}, &fakeStore{manifest: domain.NewManifest()}, &fakeUploader{}, &fakeLinker{})
	if _, err := svc.Share(context.Background(), Request{File: "/no/such/file.mp3", As: "x.mp3"}); err == nil {
		t.Fatal("Share err = nil for missing file, want error")
	}
}

func TestShareUploadErrorPropagates(t *testing.T) {
	file := writeFile(t, "hello")
	store := &fakeStore{manifest: domain.NewManifest()}
	uploader := &fakeUploader{err: errors.New("rsync failed")}
	svc := NewService(testTarget(), &fakeFS{}, store, uploader, &fakeLinker{})

	if _, err := svc.Share(context.Background(), Request{File: file, As: "demos/test.mp3"}); err == nil {
		t.Fatal("Share err = nil on upload failure, want error")
	}
	if _, ok := store.manifest.Blobs[helloBlob]; ok {
		t.Fatal("blob recorded despite upload failure")
	}
}

func TestShareLinkerErrorPropagates(t *testing.T) {
	file := writeFile(t, "hello")
	manifest := domain.NewManifest()
	manifest.Blobs[helloBlob] = domain.Blob{Size: 5}
	store := &fakeStore{manifest: manifest}
	linker := &fakeLinker{err: errors.New("link failed")}
	svc := NewService(testTarget(), &fakeFS{}, store, &fakeUploader{}, linker)

	if _, err := svc.Share(context.Background(), Request{File: file, As: "demos/test.mp3"}); err == nil {
		t.Fatal("Share err = nil on linker failure, want error")
	}
}
