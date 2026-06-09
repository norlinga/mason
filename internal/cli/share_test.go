package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/domain"
	"github.com/norlinga/mason/internal/share"
)

func sampleShareResult(uploaded bool) share.Result {
	return share.Result{
		Action:     domain.ShareCreate,
		Uploaded:   uploaded,
		ObjectPath: "demos/test.mp3",
		Slug:       "abc123",
		Ext:        ".mp3",
		Hash:       "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		Size:       4412345,
		LongURL:    "https://share.example.com/demos/test.mp3",
		ShortURL:   "https://share.example.com/s/abc123.mp3",
	}
}

func runShareCommand(t *testing.T, args []string, run shareRunFunc) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	// Replace the wired share command with one using the injected runner.
	for _, c := range root.Commands() {
		if c.Name() == "share" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newShareCommand(&stdout, &stderr, run))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestShareCommandPlainUploaded(t *testing.T) {
	stdout, _, err := runShareCommand(t, []string{"share", "./foo.mp3", "--as", "demos/test.mp3"},
		func(context.Context, rootOptions, string, string) (share.Result, error) {
			return sampleShareResult(true), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "Sharing 'foo.mp3' → demos/test.mp3\n" +
		"Uploaded 4.2 MB.\n" +
		"  Long:  https://share.example.com/demos/test.mp3\n" +
		"  Short: https://share.example.com/s/abc123.mp3\n"
	if stdout != want {
		t.Fatalf("plain uploaded output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestShareCommandPlainLinkOnly(t *testing.T) {
	stdout, _, err := runShareCommand(t, []string{"share", "./foo.mp3", "--as", "demos/test.mp3"},
		func(context.Context, rootOptions, string, string) (share.Result, error) {
			return sampleShareResult(false), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "Sharing 'foo.mp3' → demos/test.mp3\n" +
		"Content already on server — linking only.\n" +
		"  Long:  https://share.example.com/demos/test.mp3\n" +
		"  Short: https://share.example.com/s/abc123.mp3\n"
	if stdout != want {
		t.Fatalf("plain link-only output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestShareCommandJSON(t *testing.T) {
	stdout, _, err := runShareCommand(t, []string{"--json", "share", "./foo.mp3", "--as", "demos/test.mp3"},
		func(context.Context, rootOptions, string, string) (share.Result, error) {
			return sampleShareResult(true), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": {
    "action": "create",
    "uploaded": true,
    "object_path": "demos/test.mp3",
    "slug": "abc123",
    "ext": ".mp3",
    "hash": "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
    "size": 4412345,
    "long_url": "https://share.example.com/demos/test.mp3",
    "short_url": "https://share.example.com/s/abc123.mp3"
  }
}
`
	if stdout != want {
		t.Fatalf("json output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestShareCommandQuiet(t *testing.T) {
	stdout, _, err := runShareCommand(t, []string{"--quiet", "share", "./foo.mp3"},
		func(context.Context, rootOptions, string, string) (share.Result, error) {
			return sampleShareResult(true), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "https://share.example.com/s/abc123.mp3\n" {
		t.Fatalf("quiet output = %q", stdout)
	}
}

func TestShareCommandErrorPlain(t *testing.T) {
	stdout, stderr, err := runShareCommand(t, []string{"share", "./foo.mp3"},
		func(context.Context, rootOptions, string, string) (share.Result, error) {
			return share.Result{}, errors.New("ssh unreachable")
		})
	if err == nil {
		t.Fatal("execute err = nil, want error")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty on error", stdout)
	}
	if !strings.Contains(stderr, "ssh unreachable") {
		t.Fatalf("stderr = %q, want the error message", stderr)
	}
}

func TestShareCommandErrorJSON(t *testing.T) {
	stdout, _, err := runShareCommand(t, []string{"--json", "share", "./foo.mp3"},
		func(context.Context, rootOptions, string, string) (share.Result, error) {
			return share.Result{}, errors.New("ssh unreachable")
		})
	if err == nil {
		t.Fatal("execute err = nil, want error")
	}
	if !strings.Contains(stdout, `"ok": false`) || !strings.Contains(stdout, "ssh unreachable") {
		t.Fatalf("json error envelope = %q", stdout)
	}
}

func TestShareCommandRequiresFileArg(t *testing.T) {
	_, _, err := runShareCommand(t, []string{"share"},
		func(context.Context, rootOptions, string, string) (share.Result, error) {
			t.Fatal("runner should not be called without a file arg")
			return share.Result{}, nil
		})
	if err == nil {
		t.Fatal("execute err = nil, want usage error for missing arg")
	}
}
