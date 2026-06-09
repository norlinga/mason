package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCommandPlain(t *testing.T) {
	stdout, _, err := execInitCommand(t, []string{"init"}, func(_ context.Context, dir string) (string, error) {
		return filepath.Join(dir, ".mason"), nil
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.HasPrefix(stdout, "Created ") || !strings.HasSuffix(stdout, ".mason\n") {
		t.Fatalf("stdout = %q, want Created .../.mason", stdout)
	}
}

func TestInitCommandJSON(t *testing.T) {
	stdout, _, err := execInitCommand(t, []string{"--json", "init"}, func(_ context.Context, _ string) (string, error) {
		return "/tmp/project/.mason", nil
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": {
    "path": "/tmp/project/.mason"
  }
}
`
	if stdout != want {
		t.Fatalf("json output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestInitCommandErrorPlain(t *testing.T) {
	stdout, stderr, err := execInitCommand(t, []string{"init"}, func(context.Context, string) (string, error) {
		return "", errors.New(".mason already exists")
	})
	if err == nil {
		t.Fatal("execute err = nil, want error")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty on error", stdout)
	}
	if !strings.Contains(stderr, ".mason already exists") {
		t.Fatalf("stderr = %q, want error message", stderr)
	}
}

func execInitCommand(t *testing.T, args []string, run initRunFunc) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "init" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newInitCommand(&stdout, &stderr, run))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}
