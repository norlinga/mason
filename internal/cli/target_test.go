package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/config"
)

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".mason")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const twoTargetYAML = `default: home
targets:
  home:
    host: h.example.com
    user: mason
    root: /srv/share
    base_url: https://h.example.com
  work:
    host: w.example.com
    user: mason
    root: /srv/share
    base_url: https://w.example.com
`

func runTargetCommand(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func reloadConfig(t *testing.T, path string) *config.Config {
	t.Helper()
	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	return cfg
}

func TestTargetLsPlain(t *testing.T) {
	path := writeTempConfig(t, twoTargetYAML)
	stdout, _, err := runTargetCommand(t, []string{"--config", path, "target", "ls"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, want := range []string{"NAME", "DEFAULT", "home", "work", "h.example.com"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("ls output missing %q:\n%s", want, stdout)
		}
	}
	// the default target is flagged
	if !strings.Contains(stdout, "*") {
		t.Fatalf("ls output missing default marker:\n%s", stdout)
	}
}

func TestTargetLsJSON(t *testing.T) {
	path := writeTempConfig(t, twoTargetYAML)
	stdout, _, err := runTargetCommand(t, []string{"--config", path, "--json", "target", "ls"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": [
    {
      "name": "home",
      "default": true,
      "host": "h.example.com",
      "user": "mason",
      "base_url": "https://h.example.com"
    },
    {
      "name": "work",
      "default": false,
      "host": "w.example.com",
      "user": "mason",
      "base_url": "https://w.example.com"
    }
  ]
}
`
	if stdout != want {
		t.Fatalf("json output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestTargetAdd(t *testing.T) {
	path := writeTempConfig(t, twoTargetYAML)
	stdout, _, err := runTargetCommand(t, []string{"--config", path, "target", "add", "prod",
		"--host", "p.example.com", "--root", "/srv/share", "--base-url", "https://p.example.com"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(stdout, `Added target "prod"`) {
		t.Fatalf("output = %q", stdout)
	}
	cfg := reloadConfig(t, path)
	if _, ok := cfg.Targets["prod"]; !ok {
		t.Fatal("prod not persisted to config")
	}
	if cfg.Targets["prod"].User != "mason" {
		t.Fatalf("prod user = %q, want defaulted mason", cfg.Targets["prod"].User)
	}
	if cfg.Default != "home" {
		t.Fatalf("default changed unexpectedly: %q", cfg.Default)
	}
}

func TestTargetAddMakeDefault(t *testing.T) {
	path := writeTempConfig(t, twoTargetYAML)
	stdout, _, err := runTargetCommand(t, []string{"--config", path, "target", "add", "prod",
		"--host", "p", "--root", "/srv/share", "--base-url", "https://p", "--default"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(stdout, "now default") {
		t.Fatalf("output = %q, want default note", stdout)
	}
	if reloadConfig(t, path).Default != "prod" {
		t.Fatal("prod not set as default")
	}
}

func TestTargetAddDuplicateErrors(t *testing.T) {
	path := writeTempConfig(t, twoTargetYAML)
	_, stderr, err := runTargetCommand(t, []string{"--config", path, "target", "add", "home",
		"--host", "x", "--root", "/srv/share", "--base-url", "https://x"})
	if err == nil {
		t.Fatal("execute err = nil, want duplicate error")
	}
	if !strings.Contains(stderr, "already exists") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestTargetUse(t *testing.T) {
	path := writeTempConfig(t, twoTargetYAML)
	stdout, _, err := runTargetCommand(t, []string{"--config", path, "target", "use", "work"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(stdout, `Default target is now "work"`) {
		t.Fatalf("output = %q", stdout)
	}
	if reloadConfig(t, path).Default != "work" {
		t.Fatal("default not persisted")
	}
}

func TestTargetUseUnknownErrors(t *testing.T) {
	path := writeTempConfig(t, twoTargetYAML)
	_, stderr, err := runTargetCommand(t, []string{"--config", path, "target", "use", "nope"})
	if err == nil {
		t.Fatal("execute err = nil, want unknown-target error")
	}
	if !strings.Contains(stderr, "not defined") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestTargetRmDefaultReassigns(t *testing.T) {
	path := writeTempConfig(t, twoTargetYAML)
	stdout, _, err := runTargetCommand(t, []string{"--config", path, "target", "rm", "home"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(stdout, `Removed target "home" (default is now "work")`) {
		t.Fatalf("output = %q", stdout)
	}
	cfg := reloadConfig(t, path)
	if _, ok := cfg.Targets["home"]; ok {
		t.Fatal("home not removed from config")
	}
	if cfg.Default != "work" {
		t.Fatalf("default = %q, want work", cfg.Default)
	}
}

func TestTargetRmOnlyTargetRefused(t *testing.T) {
	single := `default: home
targets:
  home:
    host: h
    user: mason
    root: /srv/share
    base_url: https://h
`
	path := writeTempConfig(t, single)
	_, stderr, err := runTargetCommand(t, []string{"--config", path, "target", "rm", "home"})
	if err == nil {
		t.Fatal("execute err = nil, want refusal")
	}
	if !strings.Contains(stderr, "only target") {
		t.Fatalf("stderr = %q", stderr)
	}
}
