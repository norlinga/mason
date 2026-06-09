package links

import (
	"context"
	"errors"
	"testing"

	"github.com/norlinga/mason/internal/domain"
)

// orphanStore builds a manifest where blobA is referenced by a share and blobB is
// orphaned (present in Blobs, referenced by no endpoint).
func orphanStore() *fakeStore {
	m := domain.NewManifest()
	m.Blobs[blobA] = domain.Blob{Size: 100}
	m.Blobs[blobB] = domain.Blob{Size: 250}
	m.ReserveSlug("zzz999")
	m.Shares = append(m.Shares, domain.Share{
		ID:   "live",
		Blob: blobA,
		Path: &domain.Endpoint{Value: "inbox/foo.mp3", State: domain.EndpointOK},
		Slug: &domain.SlugEndpoint{Value: "zzz999", Ext: ".mp3", State: domain.EndpointOK},
	})
	return &fakeStore{manifest: m}
}

func TestPruneDryRunReportsWithoutDeleting(t *testing.T) {
	store := orphanStore()
	fs := newFakeFS()
	svc := newService(t, fs, store)

	res, err := svc.Prune(context.Background(), PruneRequest{Confirm: false})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if res.Deleted {
		t.Fatal("Deleted = true, want false for dry-run")
	}
	if len(res.Orphaned) != 1 || res.Orphaned[0].Blob != blobB {
		t.Fatalf("orphaned = %#v, want only blobB", res.Orphaned)
	}
	if res.TotalSize != 250 {
		t.Fatalf("TotalSize = %d, want 250", res.TotalSize)
	}
	if _, ok := store.manifest.Blobs[blobB]; !ok {
		t.Fatal("dry-run deleted blobB from manifest")
	}
	if len(fs.removes) != 0 {
		t.Fatal("dry-run removed blob files")
	}
}

func TestPruneConfirmDeletesOrphans(t *testing.T) {
	store := orphanStore()
	fs := newFakeFS()
	svc := newService(t, fs, store)

	res, err := svc.Prune(context.Background(), PruneRequest{Confirm: true})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if !res.Deleted {
		t.Fatal("Deleted = false, want true")
	}
	if _, ok := store.manifest.Blobs[blobB]; ok {
		t.Fatal("blobB still in manifest after prune")
	}
	if _, ok := store.manifest.Blobs[blobA]; !ok {
		t.Fatal("live blobA removed by prune")
	}
	wantRemoved := "/srv/share/" + blobBPath
	found := false
	for _, r := range fs.removes {
		if r == wantRemoved {
			found = true
		}
	}
	if !found {
		t.Fatalf("blob file %q not removed; removes = %v", wantRemoved, fs.removes)
	}
}

func TestPruneNothingOrphaned(t *testing.T) {
	m := domain.NewManifest()
	m.Blobs[blobA] = domain.Blob{Size: 100}
	m.ReserveSlug("zzz999")
	m.Shares = append(m.Shares, domain.Share{
		ID:   "live",
		Blob: blobA,
		Slug: &domain.SlugEndpoint{Value: "zzz999", Ext: ".mp3", State: domain.EndpointOK},
	})
	store := &fakeStore{manifest: m}
	fs := newFakeFS()
	svc := newService(t, fs, store)

	res, err := svc.Prune(context.Background(), PruneRequest{Confirm: true})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if res.Deleted || len(res.Orphaned) != 0 {
		t.Fatalf("result = %#v, want empty undeleted", res)
	}
	if len(fs.removes) != 0 {
		t.Fatal("removed files when nothing orphaned")
	}
}

func TestPruneUpdateErrorStopsBeforeFileDeletion(t *testing.T) {
	store := orphanStore()
	store.putErr = errors.New("write failed")
	fs := newFakeFS()
	svc := newService(t, fs, store)

	if _, err := svc.Prune(context.Background(), PruneRequest{Confirm: true}); err == nil {
		t.Fatal("Prune err = nil on update failure, want error")
	}
	if _, ok := store.manifest.Blobs[blobB]; !ok {
		t.Fatal("manifest mutated despite update failure")
	}
	if len(fs.removes) != 0 {
		t.Fatal("blob files removed despite update failure")
	}
}

func TestPruneRemoveErrorAfterManifestUpdate(t *testing.T) {
	store := orphanStore()
	fs := newFakeFS()
	fs.err = errors.New("ssh failed")
	svc := newService(t, fs, store)

	if _, err := svc.Prune(context.Background(), PruneRequest{Confirm: true}); err == nil {
		t.Fatal("Prune err = nil on remove failure, want error")
	}
	if _, ok := store.manifest.Blobs[blobB]; ok {
		t.Fatal("orphan blobB still listed in manifest after confirmed prune")
	}
	if len(fs.removes) != 0 {
		t.Fatal("remove failed before any blob file deletion")
	}
}

func TestPruneDisabledEndpointKeepsBlob(t *testing.T) {
	// A disabled endpoint still counts as a reference, so its blob is not orphaned.
	m := domain.NewManifest()
	m.Blobs[blobA] = domain.Blob{Size: 100}
	m.ReserveSlug("zzz999")
	m.Shares = append(m.Shares, domain.Share{
		ID:   "live",
		Blob: blobA,
		Slug: &domain.SlugEndpoint{Value: "zzz999", Ext: ".mp3", State: domain.EndpointDisabled},
	})
	store := &fakeStore{manifest: m}
	svc := newService(t, newFakeFS(), store)

	res, err := svc.Prune(context.Background(), PruneRequest{Confirm: true})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(res.Orphaned) != 0 {
		t.Fatalf("orphaned = %#v, want none (disabled endpoint still references blob)", res.Orphaned)
	}
}
