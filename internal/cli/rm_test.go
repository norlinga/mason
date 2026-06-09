package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/links"
)

func runRmCommand(t *testing.T, args []string, run rmRunFunc) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "rm" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newRmCommand(&stdout, &stderr, run))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestRmCommandPlainEndpoint(t *testing.T) {
	stdout, _, err := runRmCommand(t, []string{"rm", "inbox/foo.mp3"},
		func(context.Context, rootOptions, string) (links.RemoveResult, error) {
			return links.RemoveResult{Endpoint: "path"}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "Removed inbox/foo.mp3\n" {
		t.Fatalf("plain output = %q", stdout)
	}
}

func TestRmCommandPlainShareRemovedAndOrphaned(t *testing.T) {
	stdout, _, err := runRmCommand(t, []string{"rm", "inbox/foo.mp3"},
		func(context.Context, rootOptions, string) (links.RemoveResult, error) {
			return links.RemoveResult{Endpoint: "path", ShareRemoved: true, BlobOrphaned: true}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "Removed inbox/foo.mp3\n" +
		"Share has no remaining endpoints.\n" +
		"Blob is now unreferenced — run 'mason prune' to reclaim space.\n"
	if stdout != want {
		t.Fatalf("plain output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestRmCommandJSON(t *testing.T) {
	stdout, _, err := runRmCommand(t, []string{"--json", "rm", "s/abc123"},
		func(context.Context, rootOptions, string) (links.RemoveResult, error) {
			return links.RemoveResult{
				Endpoint:     "slug",
				ShareRemoved: false,
				BlobOrphaned: false,
				Blob:         "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
			}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": {
    "ref": "s/abc123",
    "endpoint": "slug",
    "share_removed": false,
    "blob_orphaned": false,
    "hash": "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
  }
}
`
	if stdout != want {
		t.Fatalf("json output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestRmCommandQuiet(t *testing.T) {
	stdout, _, err := runRmCommand(t, []string{"--quiet", "rm", "inbox/foo.mp3"},
		func(context.Context, rootOptions, string) (links.RemoveResult, error) {
			return links.RemoveResult{Endpoint: "path"}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "" {
		t.Fatalf("quiet output = %q, want empty", stdout)
	}
}

func TestRmCommandError(t *testing.T) {
	_, stderr, err := runRmCommand(t, []string{"rm", "nope.mp3"},
		func(context.Context, rootOptions, string) (links.RemoveResult, error) {
			return links.RemoveResult{}, errors.New("no share found for \"nope.mp3\"")
		})
	if err == nil {
		t.Fatal("execute err = nil, want error")
	}
	if !strings.Contains(stderr, "no share found") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestRmCommandRequiresRef(t *testing.T) {
	_, _, err := runRmCommand(t, []string{"rm"},
		func(context.Context, rootOptions, string) (links.RemoveResult, error) {
			t.Fatal("runner should not run without a ref")
			return links.RemoveResult{}, nil
		})
	if err == nil {
		t.Fatal("execute err = nil, want usage error")
	}
}
