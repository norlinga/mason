package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/links"
)

func runPruneCommand(t *testing.T, args []string, run pruneRunFunc) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "prune" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newPruneCommand(&stdout, &stderr, run))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func samplePruneResult(deleted bool) links.PruneResult {
	return links.PruneResult{
		Orphaned: []links.PrunedBlob{
			{Blob: "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", Size: 4412345},
		},
		TotalSize: 4412345,
		Deleted:   deleted,
	}
}

func TestPruneCommandDryRunPlain(t *testing.T) {
	stdout, _, err := runPruneCommand(t, []string{"prune"},
		func(context.Context, rootOptions) (links.PruneResult, error) {
			return samplePruneResult(false), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "Would prune 1 orphaned blob(s) (4.2 MB):\n" +
		"  2cf24d…  4.2 MB\n" +
		"Run with --yes to delete.\n"
	if stdout != want {
		t.Fatalf("dry-run output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestPruneCommandDeletedPlain(t *testing.T) {
	stdout, _, err := runPruneCommand(t, []string{"--yes", "prune"},
		func(context.Context, rootOptions) (links.PruneResult, error) {
			return samplePruneResult(true), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "Pruned 1 orphaned blob(s) (4.2 MB):\n" +
		"  2cf24d…  4.2 MB\n"
	if stdout != want {
		t.Fatalf("deleted output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestPruneCommandNothingOrphaned(t *testing.T) {
	stdout, _, err := runPruneCommand(t, []string{"prune"},
		func(context.Context, rootOptions) (links.PruneResult, error) {
			return links.PruneResult{}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "No orphaned blobs.\n" {
		t.Fatalf("empty output = %q", stdout)
	}
}

func TestPruneCommandJSON(t *testing.T) {
	stdout, _, err := runPruneCommand(t, []string{"--json", "prune"},
		func(context.Context, rootOptions) (links.PruneResult, error) {
			return samplePruneResult(false), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": {
    "deleted": false,
    "total_size": 4412345,
    "blobs": [
      {
        "hash": "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
        "size": 4412345
      }
    ]
  }
}
`
	if stdout != want {
		t.Fatalf("json output:\n got: %q\nwant: %q", stdout, want)
	}
}

// TestPruneCommandConfirmFlag verifies that --yes (without --dry-run) asks the
// service to delete, while plain and --dry-run runs do not.
func TestPruneCommandConfirmFlag(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantConfirm bool
	}{
		{"default dry-run", []string{"prune"}, false},
		{"yes deletes", []string{"--yes", "prune"}, true},
		{"dry-run overrides yes", []string{"--yes", "--dry-run", "prune"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotConfirm bool
			_, _, err := runPruneCommand(t, tt.args,
				func(_ context.Context, root rootOptions) (links.PruneResult, error) {
					gotConfirm = root.yes && !root.dryRun
					return links.PruneResult{}, nil
				})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if gotConfirm != tt.wantConfirm {
				t.Fatalf("confirm = %v, want %v", gotConfirm, tt.wantConfirm)
			}
		})
	}
}

func TestPruneCommandError(t *testing.T) {
	_, stderr, err := runPruneCommand(t, []string{"--yes", "prune"},
		func(context.Context, rootOptions) (links.PruneResult, error) {
			return links.PruneResult{}, errors.New("ssh unreachable")
		})
	if err == nil {
		t.Fatal("execute err = nil, want error")
	}
	if !strings.Contains(stderr, "ssh unreachable") {
		t.Fatalf("stderr = %q", stderr)
	}
}
