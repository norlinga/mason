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

func runCpCommand(t *testing.T, args []string, run cpRunFunc) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "cp" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newCpCommand(&stdout, &stderr, run))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func sampleCpOutcome() cpOutcome {
	return cpOutcome{
		res: links.CopyResult{
			Share: domain.Share{
				Blob: "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
				Path: &domain.Endpoint{Value: "archive/copy.mp3", State: domain.EndpointOK},
				Slug: &domain.SlugEndpoint{Value: "xyz789", Ext: ".mp3", State: domain.EndpointOK},
			},
		},
		baseURL: "https://share.example.com",
	}
}

func TestCpCommandPlain(t *testing.T) {
	stdout, _, err := runCpCommand(t, []string{"cp", "inbox/foo.mp3", "archive/copy.mp3"},
		func(context.Context, rootOptions, string, string) (cpOutcome, error) {
			return sampleCpOutcome(), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "Copied → archive/copy.mp3\n" +
		"  Long:  https://share.example.com/archive/copy.mp3\n" +
		"  Short: https://share.example.com/s/xyz789.mp3\n"
	if stdout != want {
		t.Fatalf("plain output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestCpCommandJSON(t *testing.T) {
	stdout, _, err := runCpCommand(t, []string{"--json", "cp", "inbox/foo.mp3", "archive/copy.mp3"},
		func(context.Context, rootOptions, string, string) (cpOutcome, error) {
			return sampleCpOutcome(), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": {
    "object_path": "archive/copy.mp3",
    "slug": "xyz789",
    "ext": ".mp3",
    "hash": "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
    "long_url": "https://share.example.com/archive/copy.mp3",
    "short_url": "https://share.example.com/s/xyz789.mp3"
  }
}
`
	if stdout != want {
		t.Fatalf("json output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestCpCommandQuiet(t *testing.T) {
	stdout, _, err := runCpCommand(t, []string{"--quiet", "cp", "inbox/foo.mp3", "archive/copy.mp3"},
		func(context.Context, rootOptions, string, string) (cpOutcome, error) {
			return sampleCpOutcome(), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "https://share.example.com/s/xyz789.mp3\n" {
		t.Fatalf("quiet output = %q", stdout)
	}
}

func TestCpCommandError(t *testing.T) {
	_, stderr, err := runCpCommand(t, []string{"cp", "inbox/foo.mp3", "archive/copy.mp3"},
		func(context.Context, rootOptions, string, string) (cpOutcome, error) {
			return cpOutcome{}, errors.New("object path \"archive/copy.mp3\" is already in use")
		})
	if err == nil {
		t.Fatal("execute err = nil, want error")
	}
	if !strings.Contains(stderr, "already in use") {
		t.Fatalf("stderr = %q, want the error message", stderr)
	}
}

func TestCpCommandRequiresTwoArgs(t *testing.T) {
	_, _, err := runCpCommand(t, []string{"cp", "inbox/foo.mp3"},
		func(context.Context, rootOptions, string, string) (cpOutcome, error) {
			t.Fatal("runner should not run without two args")
			return cpOutcome{}, nil
		})
	if err == nil {
		t.Fatal("execute err = nil, want usage error")
	}
}
