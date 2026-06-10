package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runRemoteInitCommand(t *testing.T, args []string, run remoteInitRunFunc) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "remote-init" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newRemoteInitCommand(&stdout, &stderr, run))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestRemoteInitCommandPrintsScript(t *testing.T) {
	stdout, stderr, err := runRemoteInitCommand(t, []string{"remote-init"},
		func(_ context.Context, _ rootOptions, p remoteInitParams) (remoteInitOutcome, error) {
			if !p.reload {
				t.Fatal("reload should default true")
			}
			return remoteInitOutcome{script: "#!/bin/sh\nset -eu\n", keyPath: "/home/u/.ssh/id_ed25519"}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "#!/bin/sh\nset -eu\n" {
		t.Fatalf("stdout = %q, want the raw script", stdout)
	}
	if !strings.Contains(stderr, "--apply --as-admin") {
		t.Fatalf("stderr = %q, want apply hint", stderr)
	}
}

func TestRemoteInitCommandNoReloadFlag(t *testing.T) {
	_, _, err := runRemoteInitCommand(t, []string{"remote-init", "--no-reload"},
		func(_ context.Context, _ rootOptions, p remoteInitParams) (remoteInitOutcome, error) {
			if p.reload {
				t.Fatal("--no-reload should set reload=false")
			}
			return remoteInitOutcome{script: "x"}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestRemoteInitCommandPubKeyWarning(t *testing.T) {
	_, stderr, err := runRemoteInitCommand(t, []string{"remote-init"},
		func(context.Context, rootOptions, remoteInitParams) (remoteInitOutcome, error) {
			return remoteInitOutcome{script: "x", pubKeyMissing: true, keyPath: "/home/u/.ssh/id_ed25519"}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(stderr, "no public key") {
		t.Fatalf("stderr = %q, want missing-key warning", stderr)
	}
}

func TestRemoteInitCommandApplied(t *testing.T) {
	stdout, stderr, err := runRemoteInitCommand(t, []string{"remote-init", "--apply", "--as-admin", "root"},
		func(_ context.Context, _ rootOptions, p remoteInitParams) (remoteInitOutcome, error) {
			if !p.apply || p.adminUser != "root" {
				t.Fatalf("apply flags not threaded: %#v", p)
			}
			return remoteInitOutcome{script: "x", applied: true, output: "created mason\n"}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "created mason\n" {
		t.Fatalf("stdout = %q, want remote output", stdout)
	}
	if !strings.Contains(stderr, "applied") {
		t.Fatalf("stderr = %q, want applied confirmation", stderr)
	}
}

func TestRemoteInitCommandQuietPrintsScriptOnly(t *testing.T) {
	stdout, stderr, err := runRemoteInitCommand(t, []string{"--quiet", "remote-init"},
		func(context.Context, rootOptions, remoteInitParams) (remoteInitOutcome, error) {
			return remoteInitOutcome{script: "#!/bin/sh\n", pubKeyMissing: true, keyPath: "/k"}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "#!/bin/sh\n" {
		t.Fatalf("stdout = %q, want only the script for piping", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want hints suppressed under --quiet", stderr)
	}
}

func TestRemoteInitCommandApplyFailureShowsRemoteOutput(t *testing.T) {
	_, stderr, err := runRemoteInitCommand(t, []string{"remote-init", "--apply", "--as-admin", "root"},
		func(context.Context, rootOptions, remoteInitParams) (remoteInitOutcome, error) {
			return remoteInitOutcome{script: "x", output: "useradd: permission denied"},
				errors.New("apply remote-init: bootstrap script exited 1")
		})
	if err == nil {
		t.Fatal("execute err = nil, want apply failure")
	}
	if !strings.Contains(stderr, "useradd: permission denied") {
		t.Fatalf("stderr = %q, want captured remote output", stderr)
	}
	if !strings.Contains(stderr, "bootstrap script exited 1") {
		t.Fatalf("stderr = %q, want concise script-failure error", stderr)
	}
	if strings.Contains(stderr, "set -eu") {
		t.Fatalf("stderr = %q, must not echo the script body", stderr)
	}
}

func TestRemoteInitCommandJSON(t *testing.T) {
	stdout, _, err := runRemoteInitCommand(t, []string{"--json", "remote-init"},
		func(context.Context, rootOptions, remoteInitParams) (remoteInitOutcome, error) {
			return remoteInitOutcome{script: "SCRIPT", keyPath: "/k"}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": {
    "script": "SCRIPT",
    "applied": false,
    "public_key_path": "/k.pub",
    "public_key_found": true
  }
}
`
	if stdout != want {
		t.Fatalf("json output:\n got: %q\nwant: %q", stdout, want)
	}
}

// TestDefaultRemoteInitRunApplyRequiresAdmin exercises the real run against a temp
// config: --apply without --as-admin must fail before any SSH attempt.
func TestDefaultRemoteInitRunApplyRequiresAdmin(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".mason")
	cfg := "default: home\n" +
		"targets:\n" +
		"  home:\n" +
		"    host: share.example.com\n" +
		"    user: mason\n" +
		"    root: /srv/share\n" +
		"    base_url: https://share.example.com\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	root := rootOptions{config: cfgPath}
	_, err := defaultRemoteInitRun(context.Background(), root, remoteInitParams{apply: true, reload: true})
	if err == nil {
		t.Fatal("apply without --as-admin err = nil, want usage error")
	}
	if !strings.Contains(err.Error(), "--as-admin") {
		t.Fatalf("err = %v, want --as-admin guidance", err)
	}
}

// TestDefaultRemoteInitRunGeneratesScript verifies the real generator path produces
// a script (no apply, no SSH) from a temp config.
func TestDefaultRemoteInitRunGeneratesScript(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".mason")
	cfg := "default: home\n" +
		"targets:\n" +
		"  home:\n" +
		"    host: share.example.com\n" +
		"    user: mason\n" +
		"    root: /srv/share\n" +
		"    base_url: https://share.example.com\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := defaultRemoteInitRun(context.Background(), rootOptions{config: cfgPath}, remoteInitParams{reload: true})
	if err != nil {
		t.Fatalf("defaultRemoteInitRun: %v", err)
	}
	if !strings.Contains(outcome.script, "share.example.com {") {
		t.Fatalf("script missing caddy block:\n%s", outcome.script)
	}
	if outcome.applied {
		t.Fatal("applied = true without --apply")
	}
}
