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

func runSiteInitCommand(t *testing.T, args []string, run siteInitRunFunc) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "site-init" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newSiteInitCommand(&stdout, &stderr, run))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestSiteInitCommandPrintsScript(t *testing.T) {
	stdout, stderr, err := runSiteInitCommand(t, []string{"site-init"},
		func(_ context.Context, _ rootOptions, p siteInitParams) (siteInitOutcome, error) {
			if !p.reload {
				t.Fatal("reload should default true")
			}
			return siteInitOutcome{script: "#!/bin/sh\nset -eu\n"}, nil
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

func TestSiteInitCommandFlagsThreaded(t *testing.T) {
	_, _, err := runSiteInitCommand(t, []string{"site-init", "--no-reload", "--spa", "--host", "h.example.com", "--root", "/srv/x"},
		func(_ context.Context, _ rootOptions, p siteInitParams) (siteInitOutcome, error) {
			if p.reload {
				t.Fatal("--no-reload should set reload=false")
			}
			if !p.spa {
				t.Fatal("--spa not threaded")
			}
			if p.host != "h.example.com" || p.root != "/srv/x" {
				t.Fatalf("host/root not threaded: %#v", p)
			}
			return siteInitOutcome{script: "x"}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestSiteInitCommandApplied(t *testing.T) {
	stdout, stderr, err := runSiteInitCommand(t, []string{"site-init", "--apply", "--as-admin", "root"},
		func(_ context.Context, _ rootOptions, p siteInitParams) (siteInitOutcome, error) {
			if !p.apply || p.adminUser != "root" {
				t.Fatalf("apply flags not threaded: %#v", p)
			}
			return siteInitOutcome{script: "x", applied: true, output: "provisioned site\n"}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "provisioned site\n" {
		t.Fatalf("stdout = %q, want remote output", stdout)
	}
	if !strings.Contains(stderr, "applied") {
		t.Fatalf("stderr = %q, want applied confirmation", stderr)
	}
}

func TestSiteInitCommandQuietPrintsScriptOnly(t *testing.T) {
	stdout, stderr, err := runSiteInitCommand(t, []string{"--quiet", "site-init"},
		func(context.Context, rootOptions, siteInitParams) (siteInitOutcome, error) {
			return siteInitOutcome{script: "#!/bin/sh\n"}, nil
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

func TestSiteInitCommandApplyFailureShowsRemoteOutput(t *testing.T) {
	_, stderr, err := runSiteInitCommand(t, []string{"site-init", "--apply", "--as-admin", "root"},
		func(context.Context, rootOptions, siteInitParams) (siteInitOutcome, error) {
			return siteInitOutcome{script: "set -eu", output: "caddy: validation failed"},
				errors.New("apply site-init: provisioning script exited 1")
		})
	if err == nil {
		t.Fatal("execute err = nil, want apply failure")
	}
	if !strings.Contains(stderr, "caddy: validation failed") {
		t.Fatalf("stderr = %q, want captured remote output", stderr)
	}
	if !strings.Contains(stderr, "provisioning script exited 1") {
		t.Fatalf("stderr = %q, want concise script-failure error", stderr)
	}
	if strings.Contains(stderr, "set -eu") {
		t.Fatalf("stderr = %q, must not echo the script body", stderr)
	}
}

func TestSiteInitCommandJSON(t *testing.T) {
	stdout, _, err := runSiteInitCommand(t, []string{"--json", "site-init"},
		func(context.Context, rootOptions, siteInitParams) (siteInitOutcome, error) {
			return siteInitOutcome{script: "SCRIPT"}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": {
    "script": "SCRIPT",
    "applied": false
  }
}
`
	if stdout != want {
		t.Fatalf("json output:\n got: %q\nwant: %q", stdout, want)
	}
}

// TestDefaultSiteInitRunApplyRequiresAdmin exercises the real run against a temp
// config: --apply without --as-admin must fail before any SSH attempt.
func TestDefaultSiteInitRunApplyRequiresAdmin(t *testing.T) {
	cfgPath := writeStaticConfig(t)
	root := rootOptions{config: cfgPath}
	_, err := defaultSiteInitRun(context.Background(), root, siteInitParams{apply: true, reload: true})
	if err == nil {
		t.Fatal("apply without --as-admin err = nil, want usage error")
	}
	if !strings.Contains(err.Error(), "--as-admin") {
		t.Fatalf("err = %v, want --as-admin guidance", err)
	}
}

// TestDefaultSiteInitRunGeneratesScript verifies the real generator path produces a
// static script (no apply, no SSH) from a temp config.
func TestDefaultSiteInitRunGeneratesScript(t *testing.T) {
	cfgPath := writeStaticConfig(t)
	outcome, err := defaultSiteInitRun(context.Background(), rootOptions{config: cfgPath}, siteInitParams{reload: true})
	if err != nil {
		t.Fatalf("defaultSiteInitRun: %v", err)
	}
	if !strings.Contains(outcome.script, "travel.example.com {") {
		t.Fatalf("script missing caddy block:\n%s", outcome.script)
	}
	if strings.Contains(outcome.script, "@mason_private") {
		t.Fatalf("static script must not carry share matchers:\n%s", outcome.script)
	}
	if outcome.applied {
		t.Fatal("applied = true without --apply")
	}
}

// TestDefaultSiteInitRunMissingRoot verifies a deploy-less target gives a clear error.
func TestDefaultSiteInitRunMissingRoot(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".mason")
	cfg := "default: home\n" +
		"targets:\n" +
		"  home:\n" +
		"    host: travel.example.com\n" +
		"    user: mason\n" +
		"    root: /var/www/travel.example.com\n" +
		"    base_url: https://travel.example.com\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := defaultSiteInitRun(context.Background(), rootOptions{config: cfgPath}, siteInitParams{reload: true})
	if err == nil {
		t.Fatal("missing web root err = nil, want error")
	}
	if !strings.Contains(err.Error(), "web root") {
		t.Fatalf("err = %v, want web-root guidance", err)
	}
}

func writeStaticConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".mason")
	cfg := "default: home\n" +
		"targets:\n" +
		"  home:\n" +
		"    host: travel.example.com\n" +
		"    user: mason\n" +
		"    root: /var/www/travel.example.com\n" +
		"    base_url: https://travel.example.com\n" +
		"    deploy:\n" +
		"      build_dir: .\n" +
		"      remote_path: /var/www/travel.example.com\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}
