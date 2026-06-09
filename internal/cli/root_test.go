package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelpListsGlobalFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute help: %v", err)
	}

	out := stdout.String()
	for _, want := range []string{
		"--target",
		"--json",
		"--dry-run",
		"--yes",
		"--quiet",
		"--verbose",
		"--config",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help output missing %q:\n%s", want, out)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute version: %v", err)
	}

	if got, want := stdout.String(), "mason dev\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}
