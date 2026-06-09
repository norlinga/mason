package remote

import (
	"context"
	"fmt"
	"strings"

	"github.com/norlinga/mason/internal/domain"
)

const (
	manifestRelPath     = ".mason/manifest.json"
	manifestTempRelPath = ".mason/manifest.json.tmp"
	manifestFileMode    = FileMode(0o600)
)

// ManifestStore reads and writes the authoritative remote manifest.
type ManifestStore interface {
	Fetch(ctx context.Context) (*domain.Manifest, error)
	Put(ctx context.Context, manifest *domain.Manifest) error
	Update(ctx context.Context, mutate func(*domain.Manifest) error) error
}

// RemoteManifestStore persists the manifest at {root}/.mason/manifest.json over SSH.
type RemoteManifestStore struct {
	fs     RemoteFS
	runner CommandRunner
	root   string
}

// NewRemoteManifestStore creates a manifest store backed by a remote filesystem.
func NewRemoteManifestStore(root string, fs RemoteFS, runner CommandRunner) *RemoteManifestStore {
	return &RemoteManifestStore{
		fs:     fs,
		runner: runner,
		root:   strings.TrimSpace(root),
	}
}

// ManifestPath returns the absolute remote path to manifest.json under root.
func ManifestPath(root string) string {
	return RemotePath(root, manifestRelPath)
}

// Fetch returns the remote manifest, or an empty v1 manifest when none exists yet.
func (s *RemoteManifestStore) Fetch(ctx context.Context) (*domain.Manifest, error) {
	if s.root == "" {
		return nil, fmt.Errorf("manifest root is required")
	}
	if s.fs == nil {
		return nil, fmt.Errorf("manifest filesystem is required")
	}
	if s.runner == nil {
		return nil, fmt.Errorf("manifest command runner is required")
	}

	path := s.manifestPath()
	exists, err := s.fileExists(ctx, path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return domain.NewManifest(), nil
	}

	data, err := s.fs.ReadFile(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	manifest, err := domain.ParseManifest(data)
	if err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return manifest, nil
}

// Put writes manifest remotely using a temp file and atomic rename.
func (s *RemoteManifestStore) Put(ctx context.Context, manifest *domain.Manifest) error {
	if s.root == "" {
		return fmt.Errorf("manifest root is required")
	}
	if s.fs == nil {
		return fmt.Errorf("manifest filesystem is required")
	}
	if s.runner == nil {
		return fmt.Errorf("manifest command runner is required")
	}

	data, err := domain.MarshalManifest(manifest)
	if err != nil {
		return err
	}

	tempPath := s.manifestTempPath()
	if err := s.fs.WriteFile(ctx, tempPath, data, manifestFileMode); err != nil {
		return fmt.Errorf("write manifest temp: %w", err)
	}

	command := fmt.Sprintf(
		"mv -f %s %s",
		shellQuote(tempPath),
		shellQuote(s.manifestPath()),
	)
	if _, err := s.runner.Run(ctx, command); err != nil {
		return fmt.Errorf("commit manifest: %w", err)
	}
	return nil
}

// Update loads the remote manifest, applies mutate, and writes it back atomically.
func (s *RemoteManifestStore) Update(ctx context.Context, mutate func(*domain.Manifest) error) error {
	if mutate == nil {
		return fmt.Errorf("manifest mutate function is required")
	}

	manifest, err := s.Fetch(ctx)
	if err != nil {
		return err
	}
	if err := mutate(manifest); err != nil {
		return err
	}
	return s.Put(ctx, manifest)
}

func (s *RemoteManifestStore) manifestPath() string {
	return ManifestPath(s.root)
}

func (s *RemoteManifestStore) manifestTempPath() string {
	return RemotePath(s.root, manifestTempRelPath)
}

func (s *RemoteManifestStore) fileExists(ctx context.Context, path string) (bool, error) {
	result, err := s.runner.Run(ctx, fmt.Sprintf("test -f %s", shellQuote(path)))
	if err == nil {
		return true, nil
	}
	if result.ExitCode == 1 {
		return false, nil
	}
	return false, fmt.Errorf("check manifest exists: %w", err)
}
