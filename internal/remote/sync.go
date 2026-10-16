package remote

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/norlinga/mason/internal/domain"
)

// SyncResult reports the outcome of reconciling the local cache with the remote.
type SyncResult struct {
	// Manifest is the authoritative remote manifest, now mirrored into the cache.
	Manifest *domain.Manifest
	// CacheUpdated is true when the local cache was written (it was absent, stale,
	// or unreadable).
	CacheUpdated bool
	// Diverged is true when a pre-existing cache differed from the remote and was
	// overwritten. A cold (absent) cache is not a divergence. This is the condition
	// worth warning about.
	Diverged bool
}

// Sync reconciles the local cache for cacheID against the authoritative remote
// manifest. The remote always wins: on any difference — including an unreadable or
// corrupt cache — the cache is overwritten with the remote copy. When a pre-existing
// cache diverged and warn is non-nil, a warning is written to warn (typically stderr).
func Sync(ctx context.Context, store ManifestStore, cache *LocalCache, cacheID string, warn io.Writer) (SyncResult, error) {
	if store == nil {
		return SyncResult{}, fmt.Errorf("manifest store is required")
	}
	if cache == nil {
		return SyncResult{}, fmt.Errorf("local cache is required")
	}

	remote, err := store.Fetch(ctx)
	if err != nil {
		return SyncResult{}, fmt.Errorf("sync fetch remote: %w", err)
	}

	cached, ok, readErr := cache.Read(cacheID)

	update := true
	diverged := false
	switch {
	case readErr != nil:
		// Corrupt or unreadable cache: remote wins and repairs it.
		diverged = true
	case !ok:
		// Cold cache: populate it, but this is not a conflict.
	case manifestsEqual(remote, cached):
		update = false
	default:
		diverged = true
	}

	if update {
		if err := cache.Write(cacheID, remote); err != nil {
			return SyncResult{}, fmt.Errorf("sync write cache: %w", err)
		}
	}

	if diverged && warn != nil {
		fmt.Fprintf(warn, "warning: local cache for %q diverged from remote; replaced with remote copy\n", cacheID)
	}

	return SyncResult{Manifest: remote, CacheUpdated: update, Diverged: diverged}, nil
}

// manifestsEqual compares two manifests by their canonical serialized form.
func manifestsEqual(a, b *domain.Manifest) bool {
	aData, err := domain.MarshalManifest(a)
	if err != nil {
		return false
	}
	bData, err := domain.MarshalManifest(b)
	if err != nil {
		return false
	}
	return bytes.Equal(aData, bData)
}
