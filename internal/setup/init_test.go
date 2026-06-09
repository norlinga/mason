package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/norlinga/mason/internal/config"
)

func TestProjectConfigTemplateIsValid(t *testing.T) {
	cfg, err := config.Parse(ProjectConfigTemplate())
	if err != nil {
		t.Fatalf("Parse template: %v", err)
	}
	if cfg.Default != "home" {
		t.Fatalf("default = %q, want home", cfg.Default)
	}
	if cfg.Targets["home"].Deploy.BuildDir != "./public" {
		t.Fatalf("build_dir = %q, want ./public", cfg.Targets["home"].Deploy.BuildDir)
	}
}

func TestInitProjectWritesConfig(t *testing.T) {
	dir := t.TempDir()
	path, err := InitProject(dir)
	if err != nil {
		t.Fatalf("InitProject: %v", err)
	}
	want := filepath.Join(dir, ".mason")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if _, err := config.Parse(data); err != nil {
		t.Fatalf("Parse written config: %v", err)
	}
}

func TestInitProjectRejectsExistingFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := InitProject(dir); err != nil {
		t.Fatalf("first InitProject: %v", err)
	}
	if _, err := InitProject(dir); err == nil {
		t.Fatal("second InitProject succeeded, want error")
	}
}
