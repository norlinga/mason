package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/norlinga/mason/internal/domain"
)

func infoManifestFixture() *domain.Manifest {
	m := domain.NewManifest()
	m.Blobs["sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"] = domain.Blob{Size: 4412345}
	m.Shares = []domain.Share{
		{
			ID:        "s1",
			Blob:      "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
			Path:      &domain.Endpoint{Value: "demos/test.mp3", State: domain.EndpointOK},
			Slug:      &domain.SlugEndpoint{Value: "abc123", Ext: ".mp3", State: domain.EndpointOK},
			CreatedAt: time.Date(2026, 6, 5, 9, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC),
		},
	}
	return m
}

func infoFixtureRun(context.Context, rootOptions) (lsData, error) {
	return lsData{manifest: infoManifestFixture(), baseURL: lsBaseURL}, nil
}

func runRefCommand(t *testing.T, name string, args []string, run lsRunFunc) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == name {
			root.RemoveCommand(c)
		}
	}
	switch name {
	case "info":
		root.AddCommand(newInfoCommand(&stdout, &stderr, run))
	case "url":
		root.AddCommand(newURLCommand(&stdout, &stderr, run))
	}
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestInfoCommandPlain(t *testing.T) {
	stdout, _, err := runRefCommand(t, "info", []string{"info", "demos/test.mp3"}, infoFixtureRun)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "Object path:  demos/test.mp3 (ok)\n" +
		"Short slug:   abc123 (ok)\n" +
		"Hash:         sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824\n" +
		"Size:         4.2 MB\n" +
		"Created:      2026-06-05T09:00:00Z\n" +
		"Updated:      2026-06-05T10:00:00Z\n" +
		"Long URL:     https://share.example.com/demos/test.mp3\n" +
		"Short URL:    https://share.example.com/s/abc123.mp3\n" +
		"Blob refs:    2\n"
	if stdout != want {
		t.Fatalf("info plain output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestInfoCommandResolvesBySlug(t *testing.T) {
	stdout, _, err := runRefCommand(t, "info", []string{"info", "s/abc123"}, infoFixtureRun)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(stdout, "Object path:  demos/test.mp3 (ok)") {
		t.Fatalf("slug ref did not resolve to share:\n%s", stdout)
	}
}

func TestInfoCommandJSON(t *testing.T) {
	stdout, _, err := runRefCommand(t, "info", []string{"--json", "info", "demos/test.mp3"}, infoFixtureRun)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": {
    "object_path": "demos/test.mp3",
    "path_state": "ok",
    "slug": "abc123",
    "ext": ".mp3",
    "slug_state": "ok",
    "hash": "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
    "size": 4412345,
    "created_at": "2026-06-05T09:00:00Z",
    "updated_at": "2026-06-05T10:00:00Z",
    "long_url": "https://share.example.com/demos/test.mp3",
    "short_url": "https://share.example.com/s/abc123.mp3",
    "blob_refs": 2
  }
}
`
	if stdout != want {
		t.Fatalf("info json output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestInfoCommandNotFound(t *testing.T) {
	_, stderr, err := runRefCommand(t, "info", []string{"info", "missing/file.mp3"}, infoFixtureRun)
	if err == nil {
		t.Fatal("execute err = nil for missing ref, want error")
	}
	if !strings.Contains(stderr, "no share found") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestURLCommandShortByDefault(t *testing.T) {
	stdout, _, err := runRefCommand(t, "url", []string{"url", "demos/test.mp3"}, infoFixtureRun)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "https://share.example.com/s/abc123.mp3\n" {
		t.Fatalf("url short output = %q", stdout)
	}
}

func TestURLCommandLong(t *testing.T) {
	stdout, _, err := runRefCommand(t, "url", []string{"url", "s/abc123", "--long"}, infoFixtureRun)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "https://share.example.com/demos/test.mp3\n" {
		t.Fatalf("url long output = %q", stdout)
	}
}

func TestURLCommandJSON(t *testing.T) {
	stdout, _, err := runRefCommand(t, "url", []string{"--json", "url", "demos/test.mp3"}, infoFixtureRun)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": {
    "url": "https://share.example.com/s/abc123.mp3"
  }
}
`
	if stdout != want {
		t.Fatalf("url json output:\n got: %q\nwant: %q", stdout, want)
	}
}
