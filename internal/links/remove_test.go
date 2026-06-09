package links

import (
	"context"
	"errors"
	"testing"

	"github.com/norlinga/mason/internal/domain"
)

func TestRemovePathEndpointKeepsSlug(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK))
	fs := newFakeFS()
	fs.symlinks["/srv/share/inbox/foo.mp3"] = "../" + blobAPath
	svc := newService(t, fs, store)

	res, err := svc.Remove(context.Background(), RemoveRequest{Ref: "inbox/foo.mp3"})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if res.Endpoint != "path" {
		t.Fatalf("Endpoint = %q, want path", res.Endpoint)
	}
	if res.ShareRemoved {
		t.Fatal("ShareRemoved = true, want false (slug remains)")
	}
	if res.BlobOrphaned {
		t.Fatal("BlobOrphaned = true, want false (slug still references blob)")
	}
	if _, ok := fs.symlinks["/srv/share/inbox/foo.mp3"]; ok {
		t.Fatal("object symlink not removed")
	}
	got := store.manifest.Shares[0]
	if got.Path != nil {
		t.Fatalf("path endpoint = %#v, want nil", got.Path)
	}
	if got.Slug == nil || got.Slug.Value != "zzz999" {
		t.Fatalf("slug endpoint = %#v, want preserved", got.Slug)
	}
}

func TestRemoveSlugEndpointRemovesBothSlugLinks(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK))
	fs := newFakeFS()
	fs.symlinks["/srv/share/s/zzz999.mp3"] = "../" + blobAPath
	fs.symlinks["/srv/share/s/zzz999"] = "../" + blobAPath
	svc := newService(t, fs, store)

	res, err := svc.Remove(context.Background(), RemoveRequest{Ref: "s/zzz999"})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if res.Endpoint != "slug" {
		t.Fatalf("Endpoint = %q, want slug", res.Endpoint)
	}
	for _, link := range []string{"/srv/share/s/zzz999.mp3", "/srv/share/s/zzz999"} {
		if _, ok := fs.symlinks[link]; ok {
			t.Fatalf("slug symlink %q not removed", link)
		}
	}
	if store.manifest.Shares[0].Slug != nil {
		t.Fatal("slug endpoint not cleared")
	}
	// Slug stays reserved even after the endpoint is removed.
	if !store.manifest.SlugReserved("zzz999") {
		t.Fatal("slug reservation dropped on rm")
	}
}

func TestRemoveLastEndpointDeletesShareAndOrphansBlob(t *testing.T) {
	// Share with only an object-path endpoint; removing it deletes the share.
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "", domain.EndpointOK, domain.EndpointOK))
	fs := newFakeFS()
	fs.symlinks["/srv/share/inbox/foo.mp3"] = "../" + blobAPath
	svc := newService(t, fs, store)

	res, err := svc.Remove(context.Background(), RemoveRequest{Ref: "inbox/foo.mp3"})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !res.ShareRemoved {
		t.Fatal("ShareRemoved = false, want true")
	}
	if !res.BlobOrphaned {
		t.Fatal("BlobOrphaned = false, want true")
	}
	if len(store.manifest.Shares) != 0 {
		t.Fatalf("shares = %d, want 0", len(store.manifest.Shares))
	}
}

func TestRemoveOnlyTouchesReferencedShare(t *testing.T) {
	store := storeWith(
		shareFixture("a", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK),
		shareFixture("b", blobA, "inbox/bar.mp3", "yyy888", domain.EndpointOK, domain.EndpointOK),
	)
	fs := newFakeFS()
	fs.symlinks["/srv/share/inbox/foo.mp3"] = "../" + blobAPath
	svc := newService(t, fs, store)

	res, err := svc.Remove(context.Background(), RemoveRequest{Ref: "inbox/foo.mp3"})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	// blob A is still referenced by share a's slug and by share b, so not orphaned.
	if res.BlobOrphaned {
		t.Fatal("BlobOrphaned = true, want false (slug + share b still reference blob A)")
	}
	if res.ShareRemoved {
		t.Fatal("ShareRemoved = true, want false (share a's slug remains)")
	}
	if len(store.manifest.Shares) != 2 {
		t.Fatalf("shares = %d, want 2 (a keeps its slug, b untouched)", len(store.manifest.Shares))
	}
	a, b := store.manifest.Shares[0], store.manifest.Shares[1]
	if a.ID != "a" || a.Path != nil || a.Slug == nil {
		t.Fatalf("share a = %#v, want path removed, slug kept", a)
	}
	if b.ID != "b" || b.Path == nil || b.Path.Value != "inbox/bar.mp3" {
		t.Fatalf("share b mutated: %#v", b)
	}
}

func TestRemoveUnknownRef(t *testing.T) {
	store := storeWith()
	svc := newService(t, newFakeFS(), store)
	if _, err := svc.Remove(context.Background(), RemoveRequest{Ref: "nope.mp3"}); err == nil {
		t.Fatal("Remove err = nil, want not-found error")
	}
}

func TestRemovePutErrorRestoresManifest(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK))
	store.putErr = errors.New("write failed")
	fs := newFakeFS()
	fs.symlinks["/srv/share/inbox/foo.mp3"] = "../" + blobAPath
	svc := newService(t, fs, store)

	if _, err := svc.Remove(context.Background(), RemoveRequest{Ref: "inbox/foo.mp3"}); err == nil {
		t.Fatal("Remove err = nil on put failure, want error")
	}
	if store.manifest.Shares[0].Path == nil {
		t.Fatal("path endpoint not restored after failed put")
	}
	if _, ok := fs.symlinks["/srv/share/inbox/foo.mp3"]; !ok {
		t.Fatal("symlink removed despite failed manifest write")
	}
}
