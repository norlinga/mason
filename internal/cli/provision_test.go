package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/config"
)

func runProvision(t *testing.T, args []string, run provisionRunFunc) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "provision" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newProvisionCommand(&stdout, &stderr, run))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

var provSite = config.Site{Name: "live", URL: "https://shop.example.com", Host: "shop.example.com", Path: "/var/www/shop.example.com",
	Server: &config.Server{User: "mason", Host: "example.com"}}

func TestProvisionPrintWritesScriptOnly(t *testing.T) {
	stdout, stderr, err := runProvision(t, []string{"provision", "live", "--print", "--spa", "--apex"},
		func(_ context.Context, _ rootOptions, p provisionParams, _ func(string) (bool, error)) (provisionOutcome, error) {
			if !p.print || p.site != "live" || !p.spa || p.domainMode != "apex" {
				t.Fatalf("params = %+v", p)
			}
			return provisionOutcome{site: provSite, script: "#!/bin/sh\necho hi\n"}, nil
		})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if stdout != "#!/bin/sh\necho hi\n" || !strings.Contains(stderr, "Review the script") {
		t.Fatalf("stdout %q stderr %q", stdout, stderr)
	}
}

func TestProvisionAppliedSummary(t *testing.T) {
	stdout, _, err := runProvision(t, []string{"provision"},
		func(context.Context, rootOptions, provisionParams, func(string) (bool, error)) (provisionOutcome, error) {
			return provisionOutcome{site: provSite, applied: true}, nil
		})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	want := "✓ Provisioned https://shop.example.com (example.com:/var/www/shop.example.com, /etc/caddy/mason.d/shop.example.com.caddy)\nNext: mason deploy\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

func TestProvisionHandEditedShowsDiff(t *testing.T) {
	_, stderr, err := runProvision(t, []string{"provision"},
		func(context.Context, rootOptions, provisionParams, func(string) (bool, error)) (provisionOutcome, error) {
			return provisionOutcome{site: provSite, output: "--- a\n+++ b\n+\troot * /x"}, errHandEdited
		})
	if !errors.Is(err, errHandEdited) || !strings.Contains(stderr, "+\troot * /x") || !strings.Contains(stderr, "--force") {
		t.Fatalf("err %v stderr %q", err, stderr)
	}
}

func TestProvisionApexSubdomainExclusive(t *testing.T) {
	_, _, err := runProvision(t, []string{"provision", "--apex", "--subdomain"},
		func(context.Context, rootOptions, provisionParams, func(string) (bool, error)) (provisionOutcome, error) {
			t.Fatal("run must not be called")
			return provisionOutcome{}, nil
		})
	if err == nil {
		t.Fatal("conflicting flags accepted")
	}
}

func TestConfirmPrompt(t *testing.T) {
	var stderr bytes.Buffer
	for in, want := range map[string]bool{"y\n": true, "YES\n": true, "n\n": false, "\n": false} {
		got, err := confirmPrompt(&stderr, strings.NewReader(in), "Go?")
		if err != nil || got != want {
			t.Fatalf("confirmPrompt(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := confirmPrompt(&stderr, strings.NewReader(""), "Go?"); err == nil || !strings.Contains(err.Error(), "-y") {
		t.Fatalf("closed stdin err = %v", err)
	}
}
