package remote

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/norlinga/mason/internal/domain"
)

const testManifestRoot = "/srv/share"

func TestManifestPath(t *testing.T) {
	got := ManifestPath(testManifestRoot)
	want := "/srv/share/.mason/manifest.json"
	if got != want {
		t.Fatalf("ManifestPath() = %q, want %q", got, want)
	}
}

func TestManifestPathPreservesRootSlash(t *testing.T) {
	got := ManifestPath("/")
	want := "/.mason/manifest.json"
	if got != want {
		t.Fatalf("ManifestPath() = %q, want %q", got, want)
	}
}

func TestRemoteManifestStoreFetchMissingReturnsEmptyManifest(t *testing.T) {
	runner := newManifestTestRunner()
	store := NewRemoteManifestStore(testManifestRoot, newMemoryRemoteFS(runner), runner)

	manifest, err := store.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if manifest.Version != domain.ManifestVersion {
		t.Fatalf("Version = %d, want %d", manifest.Version, domain.ManifestVersion)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands length = %d, want 1", len(runner.commands))
	}
	if got, want := runner.commands[0], "test -f '/srv/share/.mason/manifest.json'"; got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestRemoteManifestStoreFetchExistingManifest(t *testing.T) {
	runner := newManifestTestRunner()
	runner.files["/srv/share/.mason/manifest.json"] = []byte(sampleManifestJSON())
	store := NewRemoteManifestStore(testManifestRoot, newMemoryRemoteFS(runner), runner)

	manifest, err := store.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(manifest.Shares) != 1 {
		t.Fatalf("Shares length = %d, want 1", len(manifest.Shares))
	}
	if manifest.Shares[0].ID != "share-1" {
		t.Fatalf("share id = %q, want share-1", manifest.Shares[0].ID)
	}
}

func TestRemoteManifestStoreFetchInvalidJSON(t *testing.T) {
	runner := newManifestTestRunner()
	runner.files["/srv/share/.mason/manifest.json"] = []byte("{not-json")
	store := NewRemoteManifestStore(testManifestRoot, newMemoryRemoteFS(runner), runner)

	_, err := store.Fetch(context.Background())
	if err == nil {
		t.Fatalf("Fetch succeeded, want error")
	}
	if !strings.Contains(err.Error(), "parse manifest") {
		t.Fatalf("error = %v, want parse failure", err)
	}
}

func TestRemoteManifestStorePutWritesTempThenRenames(t *testing.T) {
	runner := newManifestTestRunner()
	fs := newMemoryRemoteFS(runner)
	store := NewRemoteManifestStore(testManifestRoot, fs, runner)

	manifest := sampleManifest()
	if err := store.Put(context.Background(), manifest); err != nil {
		t.Fatalf("Put: %v", err)
	}

	wantCommands := []string{
		"mkdir -p '/srv/share/.mason'",
		"printf %s '" + base64.StdEncoding.EncodeToString(mustMarshalManifest(t, manifest)) + "' | base64 -d > '/srv/share/.mason/manifest.json.tmp' && chmod 600 '/srv/share/.mason/manifest.json.tmp'",
		"mv -f '/srv/share/.mason/manifest.json.tmp' '/srv/share/.mason/manifest.json'",
	}
	if len(runner.commands) != len(wantCommands) {
		t.Fatalf("commands = %#v, want %#v", runner.commands, wantCommands)
	}
	for i, want := range wantCommands {
		if runner.commands[i] != want {
			t.Fatalf("commands[%d] = %q, want %q", i, runner.commands[i], want)
		}
	}

	data, ok := runner.files["/srv/share/.mason/manifest.json"]
	if !ok {
		t.Fatalf("manifest file missing after Put")
	}
	if _, ok := runner.files["/srv/share/.mason/manifest.json.tmp"]; ok {
		t.Fatalf("temp manifest still present after rename")
	}
	got, err := domain.ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if got.Shares[0].ID != manifest.Shares[0].ID {
		t.Fatalf("stored share id = %q, want %q", got.Shares[0].ID, manifest.Shares[0].ID)
	}
}

func TestRemoteManifestStoreUpdateAppliesMutation(t *testing.T) {
	runner := newManifestTestRunner()
	runner.files["/srv/share/.mason/manifest.json"] = []byte(sampleManifestJSON())
	store := NewRemoteManifestStore(testManifestRoot, newMemoryRemoteFS(runner), runner)

	err := store.Update(context.Background(), func(manifest *domain.Manifest) error {
		manifest.Shares[0].Path.State = domain.EndpointDisabled
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := store.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Shares[0].Path.State != domain.EndpointDisabled {
		t.Fatalf("Path.State = %q, want %q", got.Shares[0].Path.State, domain.EndpointDisabled)
	}
}

func TestRemoteManifestStoreUpdatePropagatesMutationError(t *testing.T) {
	runner := newManifestTestRunner()
	runner.files["/srv/share/.mason/manifest.json"] = []byte(sampleManifestJSON())
	store := NewRemoteManifestStore(testManifestRoot, newMemoryRemoteFS(runner), runner)

	wantErr := errors.New("mutation failed")
	err := store.Update(context.Background(), func(*domain.Manifest) error {
		return wantErr
	})
	if err == nil {
		t.Fatalf("Update succeeded, want error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func TestRemoteManifestStoreRejectsMissingDependencies(t *testing.T) {
	manifest := sampleManifest()
	tests := []struct {
		name    string
		root    string
		fs      RemoteFS
		runner  CommandRunner
		method  string
		wantErr string
	}{
		{
			name:    "fetch missing root",
			root:    " ",
			fs:      newMemoryRemoteFS(newManifestTestRunner()),
			runner:  newManifestTestRunner(),
			method:  "fetch",
			wantErr: "manifest root is required",
		},
		{
			name:    "put missing runner",
			root:    testManifestRoot,
			fs:      newMemoryRemoteFS(newManifestTestRunner()),
			runner:  nil,
			method:  "put",
			wantErr: "manifest command runner is required",
		},
		{
			name:    "update missing mutate",
			root:    testManifestRoot,
			fs:      newMemoryRemoteFS(newManifestTestRunner()),
			runner:  newManifestTestRunner(),
			method:  "update-nil",
			wantErr: "manifest mutate function is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewRemoteManifestStore(tt.root, tt.fs, tt.runner)
			var err error
			switch tt.method {
			case "fetch":
				_, err = store.Fetch(context.Background())
			case "put":
				err = store.Put(context.Background(), manifest)
			case "update-nil":
				err = store.Update(context.Background(), nil)
			}
			if err == nil {
				t.Fatalf("%s succeeded, want error", tt.method)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func sampleManifest() *domain.Manifest {
	uploadedAt := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	return &domain.Manifest{
		Version: domain.ManifestVersion,
		Blobs: map[string]domain.Blob{
			"sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824": {
				Size:       5,
				UploadedAt: uploadedAt,
			},
		},
		Shares: []domain.Share{
			{
				ID:   "share-1",
				Blob: "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
				Path: &domain.Endpoint{
					Value: "inbox/foo.mp3",
					State: domain.EndpointOK,
				},
				Slug: &domain.SlugEndpoint{
					Value: "f76Ab1",
					Ext:   ".mp3",
					State: domain.EndpointOK,
				},
				CreatedAt: uploadedAt,
				UpdatedAt: uploadedAt,
			},
		},
		ReservedSlugs: []string{"f76Ab1"},
	}
}

func sampleManifestJSON() string {
	data, err := domain.MarshalManifest(sampleManifest())
	if err != nil {
		panic(err)
	}
	return string(data)
}

func mustMarshalManifest(t *testing.T, manifest *domain.Manifest) []byte {
	t.Helper()
	data, err := domain.MarshalManifest(manifest)
	if err != nil {
		t.Fatalf("MarshalManifest: %v", err)
	}
	return data
}

type manifestTestRunner struct {
	files    map[string][]byte
	commands []string
}

func newManifestTestRunner() *manifestTestRunner {
	return &manifestTestRunner{files: make(map[string][]byte)}
}

func (r *manifestTestRunner) Run(ctx context.Context, command string) (CommandResult, error) {
	_ = ctx
	r.commands = append(r.commands, command)

	switch {
	case strings.HasPrefix(command, "test -f "):
		path := unquoteShellWord(strings.TrimPrefix(command, "test -f "))
		if _, ok := r.files[path]; ok {
			return CommandResult{}, nil
		}
		return CommandResult{ExitCode: 1}, errors.New("exit status 1")
	case strings.HasPrefix(command, "mv -f "):
		rest := strings.TrimPrefix(command, "mv -f ")
		src, dst, ok := strings.Cut(rest, " ")
		if !ok {
			return CommandResult{ExitCode: 1}, errors.New("invalid mv command")
		}
		src = unquoteShellWord(src)
		dst = unquoteShellWord(dst)
		data, ok := r.files[src]
		if !ok {
			return CommandResult{ExitCode: 1}, errors.New("missing source")
		}
		r.files[dst] = data
		delete(r.files, src)
		return CommandResult{}, nil
	default:
		return CommandResult{ExitCode: 1}, errors.New("unexpected command: " + command)
	}
}

type memoryRemoteFS struct {
	runner *manifestTestRunner
}

func newMemoryRemoteFS(runner *manifestTestRunner) *memoryRemoteFS {
	return &memoryRemoteFS{runner: runner}
}

func (f *memoryRemoteFS) MkdirAll(ctx context.Context, remotePath string) error {
	_, err := f.runner.Run(ctx, "mkdir -p "+shellQuote(remotePath))
	return err
}

func (f *memoryRemoteFS) Symlink(ctx context.Context, linkPath, target string) error {
	_, err := f.runner.Run(ctx, "ln -sfn "+shellQuote(target)+" "+shellQuote(linkPath))
	return err
}

func (f *memoryRemoteFS) Remove(ctx context.Context, remotePath string) error {
	_, err := f.runner.Run(ctx, "rm -f "+shellQuote(remotePath))
	return err
}

func (f *memoryRemoteFS) ReadFile(_ context.Context, remotePath string) ([]byte, error) {
	data, ok := f.runner.files[remotePath]
	if !ok {
		return nil, errors.New("read remote file: missing file")
	}
	return append([]byte(nil), data...), nil
}

func (f *memoryRemoteFS) WriteFile(_ context.Context, remotePath string, data []byte, mode FileMode) error {
	if mode == 0 {
		mode = defaultFileMode
	}

	dir := path.Dir(remotePath)
	if dir != "." && dir != "/" {
		f.runner.commands = append(f.runner.commands, "mkdir -p "+shellQuote(dir))
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	f.runner.commands = append(f.runner.commands, fmt.Sprintf(
		"printf %%s %s | base64 -d > %s && chmod %s %s",
		shellQuote(encoded),
		shellQuote(remotePath),
		formatFileMode(mode),
		shellQuote(remotePath),
	))
	f.runner.files[remotePath] = append([]byte(nil), data...)
	return nil
}

func unquoteShellWord(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return strings.ReplaceAll(value[1:len(value)-1], "'\\''", "'")
	}
	return value
}
