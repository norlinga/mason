package cli

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/norlinga/mason/internal/domain"
)

const lsBaseURL = "https://share.example.com"

func lsManifestFixture() *domain.Manifest {
	m := domain.NewManifest()
	m.Blobs["sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"] = domain.Blob{Size: 4412345}
	m.Blobs["sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"] = domain.Blob{Size: 8493465}
	m.Shares = []domain.Share{
		{
			ID:        "s1",
			Blob:      "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
			Path:      &domain.Endpoint{Value: "demos/test.mp3", State: domain.EndpointOK},
			Slug:      &domain.SlugEndpoint{Value: "abc123", Ext: ".mp3", State: domain.EndpointOK},
			CreatedAt: time.Date(2026, 6, 5, 9, 0, 0, 0, time.UTC),
		},
		{
			ID:        "s2",
			Blob:      "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			Path:      &domain.Endpoint{Value: "inbox/bar.wav", State: domain.EndpointOK},
			Slug:      &domain.SlugEndpoint{Value: "q1z000", Ext: ".wav", State: domain.EndpointOK},
			CreatedAt: time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC),
		},
		{
			ID:        "s3",
			Blob:      "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
			Path:      &domain.Endpoint{Value: "old/leak.mp3", State: domain.EndpointDisabled},
			Slug:      nil,
			CreatedAt: time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC),
		},
		{
			ID:        "s4",
			Blob:      "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
			Path:      &domain.Endpoint{Value: "mixed/state.mp3", State: domain.EndpointOK},
			Slug:      &domain.SlugEndpoint{Value: "off999", Ext: ".mp3", State: domain.EndpointDisabled},
			CreatedAt: time.Date(2026, 6, 7, 9, 0, 0, 0, time.UTC),
		},
	}
	return m
}

func runLsCommand(t *testing.T, args []string, run lsRunFunc) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "ls" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newLsCommand(&stdout, &stderr, run))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func fixtureRun(context.Context, rootOptions) (lsData, error) {
	return lsData{manifest: lsManifestFixture(), baseURL: lsBaseURL}, nil
}

func TestLsCommandPlain(t *testing.T) {
	stdout, _, err := runLsCommand(t, []string{"ls"}, fixtureRun)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "PATH             SLUG    SIZE    HASH     SHARED      PATH_STATE  SLUG_STATE\n" +
		"demos/test.mp3   abc123  4.2 MB  2cf24d…  2026-06-05  ok          ok\n" +
		"inbox/bar.wav    q1z000  8.1 MB  e3b0c4…  2026-06-06  ok          ok\n" +
		"mixed/state.mp3  off999  4.2 MB  2cf24d…  2026-06-07  ok          disabled\n" +
		"old/leak.mp3     —       4.2 MB  2cf24d…  2026-05-01  disabled    —\n"
	if stdout != want {
		t.Fatalf("ls plain output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestLsCommandPrefixFilter(t *testing.T) {
	stdout, _, err := runLsCommand(t, []string{"ls", "demos"}, fixtureRun)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "PATH            SLUG    SIZE    HASH     SHARED      PATH_STATE  SLUG_STATE\n" +
		"demos/test.mp3  abc123  4.2 MB  2cf24d…  2026-06-05  ok          ok\n"
	if stdout != want {
		t.Fatalf("ls prefix output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestLsCommandEmpty(t *testing.T) {
	stdout, _, err := runLsCommand(t, []string{"ls"}, func(context.Context, rootOptions) (lsData, error) {
		return lsData{manifest: domain.NewManifest(), baseURL: lsBaseURL}, nil
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "No shares.\n" {
		t.Fatalf("ls empty output = %q", stdout)
	}
}

func TestLsCommandJSON(t *testing.T) {
	stdout, _, err := runLsCommand(t, []string{"--json", "ls", "demos"}, fixtureRun)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": [
    {
      "object_path": "demos/test.mp3",
      "path_state": "ok",
      "slug": "abc123",
      "ext": ".mp3",
      "slug_state": "ok",
      "hash": "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
      "size": 4412345,
      "created_at": "2026-06-05T09:00:00Z",
      "long_url": "https://share.example.com/demos/test.mp3",
      "short_url": "https://share.example.com/s/abc123.mp3"
    }
  ]
}
`
	if stdout != want {
		t.Fatalf("ls json output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestLsCommandJSONEmptyIsArray(t *testing.T) {
	stdout, _, err := runLsCommand(t, []string{"--json", "ls"}, func(context.Context, rootOptions) (lsData, error) {
		return lsData{manifest: domain.NewManifest(), baseURL: lsBaseURL}, nil
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": []
}
`
	if stdout != want {
		t.Fatalf("ls json empty:\n got: %q\nwant: %q", stdout, want)
	}
}
