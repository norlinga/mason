package remote

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/domain"
)

// fakeManifestStore is a ManifestStore returning a fixed remote manifest.
type fakeManifestStore struct {
	manifest *domain.Manifest
	err      error
}

func (f fakeManifestStore) Fetch(context.Context) (*domain.Manifest, error) {
	return f.manifest, f.err
}
func (f fakeManifestStore) Put(context.Context, *domain.Manifest) error { return nil }
func (f fakeManifestStore) Update(context.Context, func(*domain.Manifest) error) error {
	return nil
}

func TestSyncColdCachePopulatesWithoutDivergence(t *testing.T) {
	cache := NewLocalCacheAt(t.TempDir())
	store := fakeManifestStore{manifest: sampleManifest()}
	var warn bytes.Buffer

	res, err := Sync(context.Background(), store, cache, testTargetID, &warn)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !res.CacheUpdated {
		t.Fatal("CacheUpdated = false on cold cache, want true")
	}
	if res.Diverged {
		t.Fatal("Diverged = true on cold cache, want false")
	}
	if warn.Len() != 0 {
		t.Fatalf("warn = %q, want empty on cold cache", warn.String())
	}

	got, ok, err := cache.Read(testTargetID)
	if err != nil || !ok {
		t.Fatalf("cache.Read after sync: ok=%v err=%v", ok, err)
	}
	if got.Shares[0].ID != "share-1" {
		t.Fatalf("cached share = %#v, want share-1", got.Shares[0])
	}
}

func TestSyncInSyncIsNoop(t *testing.T) {
	cache := NewLocalCacheAt(t.TempDir())
	if err := cache.Write(testTargetID, sampleManifest()); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	store := fakeManifestStore{manifest: sampleManifest()}
	var warn bytes.Buffer

	res, err := Sync(context.Background(), store, cache, testTargetID, &warn)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.CacheUpdated {
		t.Fatal("CacheUpdated = true when already in sync, want false")
	}
	if res.Diverged {
		t.Fatal("Diverged = true when already in sync, want false")
	}
	if warn.Len() != 0 {
		t.Fatalf("warn = %q, want empty when in sync", warn.String())
	}
}

func TestSyncRemoteWinsOnDivergence(t *testing.T) {
	cache := NewLocalCacheAt(t.TempDir())
	stale := sampleManifest()
	stale.Shares[0].Path.Value = "inbox/stale.mp3"
	if err := cache.Write(testTargetID, stale); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	store := fakeManifestStore{manifest: sampleManifest()}
	var warn bytes.Buffer

	res, err := Sync(context.Background(), store, cache, testTargetID, &warn)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !res.CacheUpdated || !res.Diverged {
		t.Fatalf("CacheUpdated=%v Diverged=%v, want both true", res.CacheUpdated, res.Diverged)
	}
	if !strings.Contains(warn.String(), "diverged") {
		t.Fatalf("warn = %q, want a divergence warning", warn.String())
	}

	got, _, err := cache.Read(testTargetID)
	if err != nil {
		t.Fatalf("cache.Read: %v", err)
	}
	if got.Shares[0].Path.Value != "inbox/foo.mp3" {
		t.Fatalf("cached path = %q, want remote value inbox/foo.mp3", got.Shares[0].Path.Value)
	}
}

func TestSyncRepairsCorruptCache(t *testing.T) {
	root := t.TempDir()
	cache := NewLocalCacheAt(root)
	dir := filepath.Join(root, testTargetID)
	if err := os.MkdirAll(dir, cacheDirMode); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, cacheManifestName), []byte("{corrupt"), cacheFileMode); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	store := fakeManifestStore{manifest: sampleManifest()}
	var warn bytes.Buffer

	res, err := Sync(context.Background(), store, cache, testTargetID, &warn)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !res.Diverged {
		t.Fatal("Diverged = false on corrupt cache, want true")
	}

	got, ok, err := cache.Read(testTargetID)
	if err != nil || !ok {
		t.Fatalf("cache repaired Read: ok=%v err=%v", ok, err)
	}
	if got.Shares[0].ID != "share-1" {
		t.Fatalf("repaired cache = %#v, want share-1", got.Shares[0])
	}
}

func TestSyncFetchErrorPropagates(t *testing.T) {
	cache := NewLocalCacheAt(t.TempDir())
	store := fakeManifestStore{err: errors.New("ssh down")}

	if _, err := Sync(context.Background(), store, cache, testTargetID, nil); err == nil {
		t.Fatal("Sync err = nil on fetch failure, want error")
	}
}

func TestSyncNilWarnDoesNotPanic(t *testing.T) {
	cache := NewLocalCacheAt(t.TempDir())
	stale := sampleManifest()
	stale.Shares[0].Path.Value = "inbox/stale.mp3"
	if err := cache.Write(testTargetID, stale); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	store := fakeManifestStore{manifest: sampleManifest()}

	res, err := Sync(context.Background(), store, cache, testTargetID, nil)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !res.Diverged {
		t.Fatal("Diverged = false, want true")
	}
}

func TestSyncRequiresStoreAndCache(t *testing.T) {
	cache := NewLocalCacheAt(t.TempDir())
	if _, err := Sync(context.Background(), nil, cache, testTargetID, nil); err == nil {
		t.Fatal("Sync with nil store err = nil, want error")
	}
	store := fakeManifestStore{manifest: sampleManifest()}
	if _, err := Sync(context.Background(), store, nil, testTargetID, nil); err == nil {
		t.Fatal("Sync with nil cache err = nil, want error")
	}
}
