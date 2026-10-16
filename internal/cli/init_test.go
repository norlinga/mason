package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/config"
)

func runInit(t *testing.T, dir string, args []string, stdin string, prov provisionRunFunc) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "init" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newInitCommand(&stdout, &stderr, initDeps{Dir: func() (string, error) { return dir, nil }, Provision: prov}))
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func noProvision(t *testing.T) provisionRunFunc {
	return func(context.Context, rootOptions, provisionParams, func(string) (bool, error)) (provisionOutcome, error) {
		t.Fatal("provision must not run")
		return provisionOutcome{}, nil
	}
}

func TestInitDefaultsFromFolderAndBuildDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "recipes.example.com")
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Two empty answers accept the suggested URL and build folder.
	stdout, stderr, err := runInit(t, dir, []string{"init", "--no-provision"}, "\n\n", noProvision(t))
	if err != nil {
		t.Fatalf("init: %v (%s)", err, stderr)
	}
	if !strings.Contains(stderr, "Site URL [https://recipes.example.com]") || !strings.Contains(stderr, "Build folder [dist]") {
		t.Fatalf("prompts = %q", stderr)
	}
	got, _ := os.ReadFile(filepath.Join(dir, ".mason"))
	if !strings.Contains(string(got), "site: https://recipes.example.com\nbuild: dist\n") {
		t.Fatalf(".mason = %q", got)
	}
	if !strings.Contains(stdout, "Next: mason provision, then mason deploy") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestInitThenProvisions(t *testing.T) {
	dir := t.TempDir()
	var gotConfig string
	prov := func(_ context.Context, root rootOptions, _ provisionParams, _ func(string) (bool, error)) (provisionOutcome, error) {
		gotConfig = root.config
		return provisionOutcome{site: config.Site{URL: "https://blog.example.com"}, applied: true}, nil
	}
	stdout, _, err := runInit(t, dir, []string{"-y", "init", "blog.example.com", "--build", "public"}, "", prov)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if gotConfig != filepath.Join(dir, ".mason") {
		t.Fatalf("provision used config %q, want the new project file", gotConfig)
	}
	if !strings.Contains(stdout, "✓ Provisioned https://blog.example.com\nNext: mason deploy") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestInitProvisionFailureKeepsFile(t *testing.T) {
	dir := t.TempDir()
	prov := func(context.Context, rootOptions, provisionParams, func(string) (bool, error)) (provisionOutcome, error) {
		return provisionOutcome{}, errors.New("no server for blog.example.com")
	}
	_, stderr, err := runInit(t, dir, []string{"-y", "init", "blog.example.com", "--build", "public"}, "", prov)
	if err != nil {
		t.Fatalf("init should succeed even if provisioning does not: %v", err)
	}
	if !strings.Contains(stderr, "Not provisioned: no server") || !strings.Contains(stderr, "mason provision") {
		t.Fatalf("stderr = %q", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mason")); err != nil {
		t.Fatal(".mason removed after provisioning failure")
	}
}

func TestInitNonInteractiveNeedsAURL(t *testing.T) {
	_, _, err := runInit(t, filepath.Join(t.TempDir(), "myproject"), []string{"-y", "init"}, "", noProvision(t))
	if err == nil {
		t.Fatal("init without a URL or domain-like folder succeeded")
	}
}
