package remote

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testTargetID = "prod"

func TestLocalCacheReadMissing(t *testing.T) {
	cache := NewLocalCacheAt(t.TempDir())

	manifest, ok, err := cache.Read(testTargetID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if ok {
		t.Fatalf("Read ok = true on empty cache, want false")
	}
	if manifest != nil {
		t.Fatalf("Read manifest = %#v, want nil", manifest)
	}
}

func TestLocalCacheWriteThenRead(t *testing.T) {
	cache := NewLocalCacheAt(t.TempDir())

	if err := cache.Write(testTargetID, sampleManifest()); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, ok, err := cache.Read(testTargetID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !ok {
		t.Fatal("Read ok = false after Write, want true")
	}
	if len(got.Shares) != 1 || got.Shares[0].ID != "share-1" {
		t.Fatalf("Shares = %#v, want one share-1", got.Shares)
	}
	if got.Shares[0].Slug == nil || got.Shares[0].Slug.Value != "f76Ab1" {
		t.Fatalf("Slug = %#v, want f76Ab1", got.Shares[0].Slug)
	}
}

func TestLocalCacheWriteIsAtomicAndOverwrites(t *testing.T) {
	root := t.TempDir()
	cache := NewLocalCacheAt(root)

	if err := cache.Write(testTargetID, sampleManifest()); err != nil {
		t.Fatalf("first Write: %v", err)
	}

	updated := sampleManifest()
	updated.Shares[0].Path.Value = "inbox/renamed.mp3"
	if err := cache.Write(testTargetID, updated); err != nil {
		t.Fatalf("second Write: %v", err)
	}

	got, _, err := cache.Read(testTargetID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Shares[0].Path.Value != "inbox/renamed.mp3" {
		t.Fatalf("Path = %q, want overwritten value", got.Shares[0].Path.Value)
	}

	// No temp files left behind after the atomic rename.
	entries, err := os.ReadDir(filepath.Join(root, testTargetID))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover temp file %q", e.Name())
		}
	}
	if len(entries) != 1 || entries[0].Name() != cacheManifestName {
		t.Fatalf("cache dir entries = %v, want only %q", entries, cacheManifestName)
	}
}

func TestLocalCacheFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes")
	}
	root := t.TempDir()
	cache := NewLocalCacheAt(root)
	if err := cache.Write(testTargetID, sampleManifest()); err != nil {
		t.Fatalf("Write: %v", err)
	}

	info, err := os.Stat(filepath.Join(root, testTargetID, cacheManifestName))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != cacheFileMode {
		t.Fatalf("manifest perm = %o, want %o", perm, cacheFileMode)
	}
}

func TestLocalCacheReadRejectsCorruptJSON(t *testing.T) {
	root := t.TempDir()
	cache := NewLocalCacheAt(root)
	dir := filepath.Join(root, testTargetID)
	if err := os.MkdirAll(dir, cacheDirMode); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, cacheManifestName), []byte("{not json"), cacheFileMode); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, _, err := cache.Read(testTargetID); err == nil {
		t.Fatal("Read err = nil on corrupt cache, want error")
	}
}

func TestLocalCacheManifestPath(t *testing.T) {
	cache := NewLocalCacheAt("/home/u/.cache/mason")
	got, err := cache.ManifestPath("prod")
	if err != nil {
		t.Fatalf("ManifestPath: %v", err)
	}
	want := filepath.Join("/home/u/.cache/mason", "prod", "manifest.json")
	if got != want {
		t.Fatalf("ManifestPath = %q, want %q", got, want)
	}
}

func TestLocalCacheRejectsEmptyRoot(t *testing.T) {
	cache := NewLocalCacheAt(" ")

	if _, err := cache.ManifestPath(testTargetID); err == nil || !strings.Contains(err.Error(), "cache root is required") {
		t.Fatalf("ManifestPath err = %v, want cache root required", err)
	}
	if _, _, err := cache.Read(testTargetID); err == nil || !strings.Contains(err.Error(), "cache root is required") {
		t.Fatalf("Read err = %v, want cache root required", err)
	}
	if err := cache.Write(testTargetID, sampleManifest()); err == nil || !strings.Contains(err.Error(), "cache root is required") {
		t.Fatalf("Write err = %v, want cache root required", err)
	}
}

func TestLocalCacheRejectsBadTargetID(t *testing.T) {
	cache := NewLocalCacheAt(t.TempDir())
	tests := []struct {
		name    string
		cacheID string
	}{
		{name: "empty", cacheID: ""},
		{name: "whitespace", cacheID: "   "},
		{name: "surrounding whitespace", cacheID: " prod "},
		{name: "path separator", cacheID: "a/b"},
		{name: "parent traversal", cacheID: ".."},
		{name: "dot", cacheID: "."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := cache.ManifestPath(tt.cacheID); err == nil {
				t.Fatalf("ManifestPath(%q) err = nil, want error", tt.cacheID)
			}
			if err := cache.Write(tt.cacheID, sampleManifest()); err == nil {
				t.Fatalf("Write(%q) err = nil, want error", tt.cacheID)
			}
		})
	}
}

func TestNewLocalCacheUsesXDGCacheHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG_CACHE_HOME is unix-specific")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	cache, err := NewLocalCache()
	if err != nil {
		t.Fatalf("NewLocalCache: %v", err)
	}
	want := filepath.Join(dir, "mason")
	if cache.root != want {
		t.Fatalf("cache root = %q, want %q", cache.root, want)
	}
}
