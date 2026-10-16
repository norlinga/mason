package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/setup"
)

func runSetup(t *testing.T, args []string, run setupRunFunc) (string, string, error) {
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

func TestSetupCommandPassesFlags(t *testing.T) {
	stdout, _, err := runSetup(t, []string{"-y", "setup", "example.com", "--share", "files.example.com", "--admin", "admin"},
		func(_ context.Context, p setup.Params, _ io.Writer, _ func(string) (bool, error)) (setup.Result, error) {
			if p.Server != "example.com" || p.ShareURL != "files.example.com" || p.Admin != "admin" || !p.Yes || p.ControlDir != "" {
				t.Fatalf("params = %+v", p)
			}
			return setup.Result{Server: "mason@example.com", KeyPath: "/h/.ssh/mason", KeyGenerated: true, Bootstrapped: true, ConfigPath: "/h/.mason", ConfigChanged: true}, nil
		})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	want := "✓ Set up: this machine publishes to mason@example.com\n  key:    /h/.ssh/mason\n  config: /h/.mason\nNext: in a site's project folder, run 'mason init'\n"
	if stdout != want {
		t.Fatalf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

func TestSetupCommandAlreadySetUp(t *testing.T) {
	stdout, _, _ := runSetup(t, []string{"setup", "example.com"},
		func(context.Context, setup.Params, io.Writer, func(string) (bool, error)) (setup.Result, error) {
			return setup.Result{Server: "mason@example.com", ConfigPath: "/h/.mason"}, nil
		})
	if !strings.HasPrefix(stdout, "✓ Already set up:") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestSetupCommandPrintOnly(t *testing.T) {
	stdout, _, _ := runSetup(t, []string{"setup", "example.com", "--print"},
		func(_ context.Context, p setup.Params, _ io.Writer, _ func(string) (bool, error)) (setup.Result, error) {
			if !p.PrintOnly {
				t.Fatal("PrintOnly not set")
			}
			return setup.Result{Scripts: []string{"#!/bin/sh\n# one\n", "#!/bin/sh\n# two\n"}}, nil
		})
	if stdout != "#!/bin/sh\n# one\n\n#!/bin/sh\n# two\n" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestSetupCommandRequiresServer(t *testing.T) {
	_, _, err := runSetup(t, []string{"setup"}, func(context.Context, setup.Params, io.Writer, func(string) (bool, error)) (setup.Result, error) {
		t.Fatal("run must not be called")
		return setup.Result{}, nil
	})
	if err == nil {
		t.Fatal("setup without a server succeeded")
	}
}
