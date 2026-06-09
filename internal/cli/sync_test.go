package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/remote"
)

func runSyncCommand(t *testing.T, args []string, run syncRunFunc) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "sync" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newSyncCommand(&stdout, &stderr, run))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestSyncCommandUpdated(t *testing.T) {
	stdout, _, err := runSyncCommand(t, []string{"sync"},
		func(context.Context, rootOptions, io.Writer) (remote.SyncResult, error) {
			return remote.SyncResult{CacheUpdated: true}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "Local cache synced from remote.\n" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestSyncCommandUpToDate(t *testing.T) {
	stdout, _, err := runSyncCommand(t, []string{"sync"},
		func(context.Context, rootOptions, io.Writer) (remote.SyncResult, error) {
			return remote.SyncResult{CacheUpdated: false}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "Local cache already up to date.\n" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestSyncCommandJSON(t *testing.T) {
	stdout, _, err := runSyncCommand(t, []string{"--json", "sync"},
		func(context.Context, rootOptions, io.Writer) (remote.SyncResult, error) {
			return remote.SyncResult{CacheUpdated: true, Diverged: true}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": {
    "updated": true,
    "diverged": true
  }
}
`
	if stdout != want {
		t.Fatalf("json output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestSyncCommandPassesWarnWriter(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "sync" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newSyncCommand(&stdout, &stderr, func(_ context.Context, _ rootOptions, warn io.Writer) (remote.SyncResult, error) {
		_, _ = io.WriteString(warn, "warning: diverged\n")
		return remote.SyncResult{CacheUpdated: true, Diverged: true}, nil
	}))
	root.SetArgs([]string{"sync"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(stderr.String(), "diverged") {
		t.Fatalf("stderr = %q, want divergence warning routed to stderr", stderr.String())
	}
}

func TestSyncCommandError(t *testing.T) {
	_, stderr, err := runSyncCommand(t, []string{"sync"},
		func(context.Context, rootOptions, io.Writer) (remote.SyncResult, error) {
			return remote.SyncResult{}, errors.New("fetch failed")
		})
	if err == nil {
		t.Fatal("execute err = nil, want error")
	}
	if !strings.Contains(stderr, "fetch failed") {
		t.Fatalf("stderr = %q", stderr)
	}
}
