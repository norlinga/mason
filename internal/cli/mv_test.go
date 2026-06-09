package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/domain"
	"github.com/norlinga/mason/internal/links"
)

func runMvCommand(t *testing.T, args []string, run mvRunFunc) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "mv" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newMvCommand(&stdout, &stderr, run))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func sampleMvOutcome() mvOutcome {
	return mvOutcome{
		res: links.MoveResult{
			Share: domain.Share{
				Blob: "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
				Path: &domain.Endpoint{Value: "archive/bar.mp3", State: domain.EndpointOK},
				Slug: &domain.SlugEndpoint{Value: "abc123", Ext: ".mp3", State: domain.EndpointOK},
			},
			OldPath: "inbox/foo.mp3",
			NewPath: "archive/bar.mp3",
		},
		baseURL: "https://share.example.com",
	}
}

func TestMvCommandPlain(t *testing.T) {
	stdout, _, err := runMvCommand(t, []string{"mv", "inbox/foo.mp3", "archive/bar.mp3"},
		func(context.Context, rootOptions, string, string) (mvOutcome, error) {
			return sampleMvOutcome(), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "Moved inbox/foo.mp3 → archive/bar.mp3\n" +
		"  Long:  https://share.example.com/archive/bar.mp3\n"
	if stdout != want {
		t.Fatalf("plain output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestMvCommandJSON(t *testing.T) {
	stdout, _, err := runMvCommand(t, []string{"--json", "mv", "inbox/foo.mp3", "archive/bar.mp3"},
		func(context.Context, rootOptions, string, string) (mvOutcome, error) {
			return sampleMvOutcome(), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": {
    "old_path": "inbox/foo.mp3",
    "object_path": "archive/bar.mp3",
    "slug": "abc123",
    "hash": "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
    "long_url": "https://share.example.com/archive/bar.mp3"
  }
}
`
	if stdout != want {
		t.Fatalf("json output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestMvCommandError(t *testing.T) {
	_, stderr, err := runMvCommand(t, []string{"mv", "inbox/foo.mp3", "archive/bar.mp3"},
		func(context.Context, rootOptions, string, string) (mvOutcome, error) {
			return mvOutcome{}, errors.New("object path \"archive/bar.mp3\" is already in use")
		})
	if err == nil {
		t.Fatal("execute err = nil, want error")
	}
	if !strings.Contains(stderr, "already in use") {
		t.Fatalf("stderr = %q, want the error message", stderr)
	}
}

func TestMvCommandQuiet(t *testing.T) {
	stdout, _, err := runMvCommand(t, []string{"--quiet", "mv", "inbox/foo.mp3", "archive/bar.mp3"},
		func(context.Context, rootOptions, string, string) (mvOutcome, error) {
			return sampleMvOutcome(), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "https://share.example.com/archive/bar.mp3\n" {
		t.Fatalf("quiet output = %q", stdout)
	}
}

func TestMvCommandRequiresTwoArgs(t *testing.T) {
	_, _, err := runMvCommand(t, []string{"mv", "inbox/foo.mp3"},
		func(context.Context, rootOptions, string, string) (mvOutcome, error) {
			t.Fatal("runner should not run without two args")
			return mvOutcome{}, nil
		})
	if err == nil {
		t.Fatal("execute err = nil, want usage error")
	}
}
