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
		"--json",
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

func TestRootHelpDropsV1Surface(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute help: %v", err)
	}
	out := stdout.String()
	for _, gone := range []string{"--target", "remote-init", "site-init", "  target "} {
		if strings.Contains(out, gone) {
			t.Fatalf("help still mentions %q:\n%s", gone, out)
		}
	}
	for _, want := range []string{"setup", "init", "provision", "deploy", "key", "doctor", "share"} {
		if !strings.Contains(out, "  "+want+" ") {
			t.Fatalf("help missing command %q:\n%s", want, out)
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
