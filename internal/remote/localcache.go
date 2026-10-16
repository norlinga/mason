package remote

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/norlinga/mason/internal/domain"
)

const (
	cacheManifestName = "manifest.json"
	cacheDirMode      = 0o700
	cacheFileMode     = 0o600
)

// LocalCache mirrors remote manifests on disk under the user cache directory:
//
//	{cacheDir}/mason/{cache-id}/manifest.json  (cache id = share store host)
//
// The cache is authoritative for nothing — it is a fast local copy of the remote
// manifest, refreshed by sync (remote wins).
type LocalCache struct {
	root string
}

// NewLocalCache roots the cache at the mason directory under the OS user cache
// directory, honoring XDG_CACHE_HOME on Unix.
func NewLocalCache() (*LocalCache, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("resolve cache dir: %w", err)
	}
	return &LocalCache{root: filepath.Join(base, "mason")}, nil
}

// NewLocalCacheAt roots the cache at an explicit mason cache directory. Useful for
// tests and for callers that resolve the cache location themselves.
func NewLocalCacheAt(root string) *LocalCache {
	return &LocalCache{root: strings.TrimSpace(root)}
}

// ManifestPath returns the cache file path for a share store's manifest.
func (c *LocalCache) ManifestPath(cacheID string) (string, error) {
	root, err := c.cacheRoot()
	if err != nil {
		return "", err
	}
	if err := validateCacheID(cacheID); err != nil {
		return "", err
	}
	return filepath.Join(root, cacheID, cacheManifestName), nil
}

// Read loads the cached manifest for cacheID. It reports ok=false (with no error)
// when nothing has been cached yet.
func (c *LocalCache) Read(cacheID string) (manifest *domain.Manifest, ok bool, err error) {
	path, err := c.ManifestPath(cacheID)
	if err != nil {
		return nil, false, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read cache manifest: %w", err)
	}

	parsed, err := domain.ParseManifest(data)
	if err != nil {
		return nil, false, fmt.Errorf("parse cache manifest: %w", err)
	}
	return parsed, true, nil
}

// Write atomically replaces the cached manifest for cacheID (temp + rename).
func (c *LocalCache) Write(cacheID string, manifest *domain.Manifest) error {
	if err := validateCacheID(cacheID); err != nil {
		return err
	}

	data, err := domain.MarshalManifest(manifest)
	if err != nil {
		return err
	}

	root, err := c.cacheRoot()
	if err != nil {
		return err
	}

	dir := filepath.Join(root, cacheID)
	if err := os.MkdirAll(dir, cacheDirMode); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	temp, err := os.CreateTemp(dir, cacheManifestName+".*.tmp")
	if err != nil {
		return fmt.Errorf("create cache temp: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath) // no-op once the rename succeeds

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write cache temp: %w", err)
	}
	if err := temp.Chmod(cacheFileMode); err != nil {
		temp.Close()
		return fmt.Errorf("chmod cache temp: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close cache temp: %w", err)
	}

	if err := os.Rename(tempPath, filepath.Join(dir, cacheManifestName)); err != nil {
		return fmt.Errorf("commit cache manifest: %w", err)
	}
	return nil
}

func (c *LocalCache) cacheRoot() (string, error) {
	if c == nil || strings.TrimSpace(c.root) == "" {
		return "", fmt.Errorf("cache root is required")
	}
	return c.root, nil
}

// validateCacheID guards against empty ids and path traversal in the cache key.
func validateCacheID(cacheID string) error {
	id := strings.TrimSpace(cacheID)
	if id == "" {
		return fmt.Errorf("cache id is required")
	}
	if id != cacheID {
		return fmt.Errorf("cache id %q has surrounding whitespace", cacheID)
	}
	if strings.ContainsRune(id, '/') || strings.ContainsRune(id, filepath.Separator) {
		return fmt.Errorf("cache id %q cannot contain a path separator", cacheID)
	}
	if id == "." || id == ".." {
		return fmt.Errorf("cache id %q is not a valid cache key", cacheID)
	}
	return nil
}
