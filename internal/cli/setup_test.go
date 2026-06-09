package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/setup"
)

func runSetupCommand(t *testing.T, args []string, run setupRunFunc) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "setup" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newSetupCommand(&stdout, &stderr, run))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestSetupCommandPlainTestedOK(t *testing.T) {
	stdout, _, err := runSetupCommand(t,
		[]string{"-y", "setup", "--host", "h", "--root", "/srv/share", "--base-url", "https://h"},
		func(_ context.Context, _ rootOptions, p setup.Params) (setup.Result, error) {
			if p.Input.Host != "h" {
				t.Fatalf("host flag not threaded: %#v", p.Input)
			}
			return setup.Result{
				ConfigPath: "/home/u/.mason",
				KeyPath:    "/home/u/.ssh/id_ed25519",
				KeyExists:  true,
				Tested:     true,
				TestOK:     true,
				Warnings:   []string{},
			}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "Wrote config to /home/u/.mason\n" +
		"SSH key: /home/u/.ssh/id_ed25519 (found)\n" +
		"Connection test: ok\n"
	if stdout != want {
		t.Fatalf("plain output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestSetupCommandPlainGeneratedFailedWithWarning(t *testing.T) {
	stdout, _, err := runSetupCommand(t,
		[]string{"-y", "setup", "--host", "h", "--root", "/srv/share", "--base-url", "http://h", "--user", "root"},
		func(context.Context, rootOptions, setup.Params) (setup.Result, error) {
			return setup.Result{
				ConfigPath:   "/home/u/.mason",
				KeyPath:      "/home/u/.ssh/id_ed25519",
				KeyGenerated: true,
				KeyExists:    true,
				Tested:       true,
				TestOK:       false,
				TestErr:      "connection refused",
				Warnings:     []string{"user is 'root': prefer a dedicated unprivileged 'mason' service account"},
			}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "Wrote config to /home/u/.mason\n" +
		"SSH key: /home/u/.ssh/id_ed25519 (generated)\n" +
		"Connection test: failed: connection refused\n" +
		"warning: user is 'root': prefer a dedicated unprivileged 'mason' service account\n"
	if stdout != want {
		t.Fatalf("plain output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestSetupCommandJSON(t *testing.T) {
	stdout, _, err := runSetupCommand(t,
		[]string{"-y", "--json", "setup", "--host", "h", "--root", "/srv/share", "--base-url", "https://h", "--no-test"},
		func(_ context.Context, _ rootOptions, p setup.Params) (setup.Result, error) {
			if p.Test {
				t.Fatal("--no-test should set Test=false")
			}
			return setup.Result{
				ConfigPath: "/home/u/.mason",
				KeyPath:    "/home/u/.ssh/id_ed25519",
				KeyExists:  true,
				Warnings:   []string{},
			}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": {
    "config_path": "/home/u/.mason",
    "key_path": "/home/u/.ssh/id_ed25519",
    "key_exists": true,
    "key_generated": false,
    "tested": false,
    "test_ok": false,
    "warnings": []
  }
}
`
	if stdout != want {
		t.Fatalf("json output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestSetupCommandError(t *testing.T) {
	_, stderr, err := runSetupCommand(t,
		[]string{"-y", "setup", "--root", "/srv/share", "--base-url", "https://h"},
		func(context.Context, rootOptions, setup.Params) (setup.Result, error) {
			return setup.Result{}, errors.New("target \"home\" requires host")
		})
	if err == nil {
		t.Fatal("execute err = nil, want error")
	}
	if !strings.Contains(stderr, "requires host") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestSetupCommandThreadsParams(t *testing.T) {
	_, _, err := runSetupCommand(t,
		[]string{"--config", "/tmp/proj/.mason", "-y", "setup",
			"--name", "prod", "--host", "h", "--root", "/srv/share", "--base-url", "https://h",
			"--force"},
		func(_ context.Context, _ rootOptions, p setup.Params) (setup.Result, error) {
			if p.ConfigPath != "/tmp/proj/.mason" {
				t.Fatalf("ConfigPath = %q, want from --config", p.ConfigPath)
			}
			if !p.GenerateKey {
				t.Fatal("GenerateKey = false, want true by default (setup ensures a key)")
			}
			if !p.Force {
				t.Fatalf("flags not threaded: %#v", p)
			}
			if p.Input.TargetName != "prod" {
				t.Fatalf("name = %q", p.Input.TargetName)
			}
			return setup.Result{ConfigPath: p.ConfigPath, KeyPath: "k", KeyExists: true, Warnings: []string{}}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestSetupCommandNoGenerateKey(t *testing.T) {
	_, _, err := runSetupCommand(t,
		[]string{"-y", "setup", "--host", "h", "--root", "/srv/share", "--base-url", "https://h",
			"--no-generate-key"},
		func(_ context.Context, _ rootOptions, p setup.Params) (setup.Result, error) {
			if p.GenerateKey {
				t.Fatal("GenerateKey = true despite --no-generate-key")
			}
			return setup.Result{ConfigPath: "/c", KeyPath: "k", Warnings: []string{}}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestSetupCommandQuiet(t *testing.T) {
	stdout, _, err := runSetupCommand(t,
		[]string{"-y", "--quiet", "setup", "--host", "h", "--root", "/srv/share", "--base-url", "https://h"},
		func(context.Context, rootOptions, setup.Params) (setup.Result, error) {
			return setup.Result{
				ConfigPath: "/home/u/.mason",
				KeyPath:    "/home/u/.ssh/id_ed25519",
				KeyExists:  true,
				Tested:     true,
				TestOK:     true,
				Warnings:   []string{},
			}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout != "" {
		t.Fatalf("quiet output = %q, want empty", stdout)
	}
}
