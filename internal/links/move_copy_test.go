package links

import (
	"context"
	"errors"
	"testing"

	"github.com/norlinga/mason/internal/domain"
)

func shareFixture(id, blob, path, slug string, pathState, slugState domain.EndpointState) domain.Share {
	s := domain.Share{ID: id, Blob: blob}
	if path != "" {
		s.Path = &domain.Endpoint{Value: path, State: pathState}
	}
	if slug != "" {
		s.Slug = &domain.SlugEndpoint{Value: slug, Ext: ".mp3", State: slugState}
	}
	return s
}

func storeWith(shares ...domain.Share) *fakeStore {
	m := manifestWithBlobs(blobA, blobB)
	for _, sh := range shares {
		if sh.Slug != nil {
			m.ReserveSlug(sh.Slug.Value)
		}
		m.Shares = append(m.Shares, sh)
	}
	return &fakeStore{manifest: m}
}

func TestMoveActivePath(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK))
	fs := newFakeFS()
	fs.symlinks["/srv/share/inbox/foo.mp3"] = "../" + blobAPath
	svc := newService(t, fs, store)

	res, err := svc.Move(context.Background(), MoveRequest{Src: "inbox/foo.mp3", Dst: "archive/bar.mp3"})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if res.OldPath != "inbox/foo.mp3" || res.NewPath != "archive/bar.mp3" {
		t.Fatalf("result paths = %q → %q", res.OldPath, res.NewPath)
	}
	if got := fs.symlinks["/srv/share/archive/bar.mp3"]; got != "../"+blobAPath {
		t.Fatalf("new symlink = %q, want repointed to A", got)
	}
	if _, ok := fs.symlinks["/srv/share/inbox/foo.mp3"]; ok {
		t.Fatal("old symlink not removed")
	}
	got := store.manifest.Shares[0]
	if got.Path == nil || got.Path.Value != "archive/bar.mp3" || got.Path.State != domain.EndpointOK {
		t.Fatalf("manifest path = %#v, want archive/bar.mp3 ok", got.Path)
	}
	if got.Slug == nil || got.Slug.Value != "zzz999" || got.Slug.State != domain.EndpointOK {
		t.Fatalf("slug changed by move: %#v", got.Slug)
	}
}

func TestMoveBySlugRef(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK))
	fs := newFakeFS()
	fs.symlinks["/srv/share/inbox/foo.mp3"] = "../" + blobAPath
	svc := newService(t, fs, store)

	res, err := svc.Move(context.Background(), MoveRequest{Src: "s/zzz999", Dst: "archive/bar.mp3"})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if res.NewPath != "archive/bar.mp3" {
		t.Fatalf("NewPath = %q", res.NewPath)
	}
	if store.manifest.Shares[0].Path.Value != "archive/bar.mp3" {
		t.Fatal("slug-ref move did not re-home object path")
	}
}

func TestMoveRecreatesMissingPath(t *testing.T) {
	// Share has only a slug endpoint; mv recreates an active object-path endpoint.
	store := storeWith(shareFixture("existing", blobA, "", "zzz999", domain.EndpointOK, domain.EndpointOK))
	fs := newFakeFS()
	svc := newService(t, fs, store)

	res, err := svc.Move(context.Background(), MoveRequest{Src: "s/zzz999", Dst: "archive/bar.mp3"})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if res.OldPath != "" {
		t.Fatalf("OldPath = %q, want empty", res.OldPath)
	}
	if got := fs.symlinks["/srv/share/archive/bar.mp3"]; got != "../"+blobAPath {
		t.Fatalf("recreated symlink = %q", got)
	}
	if store.manifest.Shares[0].Path == nil || store.manifest.Shares[0].Path.State != domain.EndpointOK {
		t.Fatal("path endpoint not recreated active")
	}
}

func TestMoveDisabledPathStaysDisabled(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointDisabled, domain.EndpointOK))
	fs := newFakeFS()
	svc := newService(t, fs, store)

	if _, err := svc.Move(context.Background(), MoveRequest{Src: "inbox/foo.mp3", Dst: "archive/bar.mp3"}); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if len(fs.symlinks) != 0 {
		t.Fatalf("symlinks = %v, want none for disabled move", fs.symlinks)
	}
	got := store.manifest.Shares[0].Path
	if got == nil || got.Value != "archive/bar.mp3" || got.State != domain.EndpointDisabled {
		t.Fatalf("path = %#v, want archive/bar.mp3 disabled", got)
	}
}

func TestMoveToSamePathNoop(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK))
	fs := newFakeFS()
	svc := newService(t, fs, store)

	if _, err := svc.Move(context.Background(), MoveRequest{Src: "inbox/foo.mp3", Dst: "inbox/foo.mp3"}); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if store.puts != 0 {
		t.Fatalf("puts = %d, want 0 for same-path move", store.puts)
	}
	if len(fs.symlinks) != 0 || len(fs.removes) != 0 {
		t.Fatal("same-path move mutated remote links")
	}
}

func TestMoveRejectsPathInUse(t *testing.T) {
	store := storeWith(
		shareFixture("a", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK),
		shareFixture("b", blobB, "archive/bar.mp3", "yyy888", domain.EndpointOK, domain.EndpointOK),
	)
	fs := newFakeFS()
	svc := newService(t, fs, store)

	if _, err := svc.Move(context.Background(), MoveRequest{Src: "inbox/foo.mp3", Dst: "archive/bar.mp3"}); err == nil {
		t.Fatal("Move err = nil, want path-in-use error")
	}
	if store.puts != 0 {
		t.Fatal("collision committed a manifest write")
	}
}

func TestMoveUnknownRef(t *testing.T) {
	store := storeWith()
	svc := newService(t, newFakeFS(), store)
	if _, err := svc.Move(context.Background(), MoveRequest{Src: "nope.mp3", Dst: "archive/bar.mp3"}); err == nil {
		t.Fatal("Move err = nil, want not-found error")
	}
}

func TestMovePutErrorRollsBack(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK))
	store.putErr = errors.New("write failed")
	fs := newFakeFS()
	fs.symlinks["/srv/share/inbox/foo.mp3"] = "../" + blobAPath
	svc := newService(t, fs, store)

	if _, err := svc.Move(context.Background(), MoveRequest{Src: "inbox/foo.mp3", Dst: "archive/bar.mp3"}); err == nil {
		t.Fatal("Move err = nil on put failure, want error")
	}
	if store.manifest.Shares[0].Path.Value != "inbox/foo.mp3" {
		t.Fatal("manifest path not restored after failed put")
	}
	if _, ok := fs.symlinks["/srv/share/archive/bar.mp3"]; ok {
		t.Fatal("new symlink not rolled back after failed put")
	}
	if _, ok := fs.symlinks["/srv/share/inbox/foo.mp3"]; !ok {
		t.Fatal("old symlink should remain after failed put")
	}
}

func TestCopyCreatesNewShareNewSlug(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK))
	fs := newFakeFS()
	svc := newService(t, fs, store)

	res, err := svc.Copy(context.Background(), CopyRequest{Src: "inbox/foo.mp3", Dst: "archive/copy.mp3"})
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if res.Share.Blob != blobA {
		t.Fatalf("copy blob = %q, want same as src (A)", res.Share.Blob)
	}
	if res.Share.Slug == nil || res.Share.Slug.Value != "abc123" {
		t.Fatalf("copy slug = %#v, want freshly minted abc123", res.Share.Slug)
	}
	if res.Share.Slug.Value == "zzz999" {
		t.Fatal("copy reused the source slug")
	}
	if len(store.manifest.Shares) != 2 {
		t.Fatalf("shares = %d, want 2 after copy", len(store.manifest.Shares))
	}
	if got := fs.symlinks["/srv/share/archive/copy.mp3"]; got != "../"+blobAPath {
		t.Fatalf("copy object symlink = %q", got)
	}
	if got := fs.symlinks["/srv/share/s/abc123.mp3"]; got != "../"+blobAPath {
		t.Fatalf("copy slug symlink = %q", got)
	}
	if !store.manifest.SlugReserved("abc123") {
		t.Fatal("new slug not reserved")
	}
	// Source share untouched.
	if store.manifest.Shares[0].Path.Value != "inbox/foo.mp3" {
		t.Fatal("source share mutated by copy")
	}
}

func TestCopyBySlugRef(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK))
	fs := newFakeFS()
	svc := newService(t, fs, store)

	res, err := svc.Copy(context.Background(), CopyRequest{Src: "s/zzz999", Dst: "archive/copy.mp3"})
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if res.Share.Path == nil || res.Share.Path.Value != "archive/copy.mp3" {
		t.Fatalf("copy path = %#v", res.Share.Path)
	}
}

func TestCopyRejectsExistingDst(t *testing.T) {
	store := storeWith(
		shareFixture("a", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK),
		shareFixture("b", blobB, "archive/copy.mp3", "yyy888", domain.EndpointOK, domain.EndpointOK),
	)
	fs := newFakeFS()
	svc := newService(t, fs, store)

	if _, err := svc.Copy(context.Background(), CopyRequest{Src: "inbox/foo.mp3", Dst: "archive/copy.mp3"}); err == nil {
		t.Fatal("Copy err = nil, want dst-in-use error")
	}
	if store.puts != 0 {
		t.Fatal("copy onto existing dst committed a write")
	}
}

func TestCopyUnknownRef(t *testing.T) {
	store := storeWith()
	svc := newService(t, newFakeFS(), store)
	if _, err := svc.Copy(context.Background(), CopyRequest{Src: "nope.mp3", Dst: "archive/copy.mp3"}); err == nil {
		t.Fatal("Copy err = nil, want not-found error")
	}
}
