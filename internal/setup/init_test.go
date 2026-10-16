package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitProjectWritesShortFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	build := DetectBuildDir(dir)
	if build != "dist" {
		t.Fatalf("DetectBuildDir = %q, want dist (first in preference order)", build)
	}

	path, err := InitProject(dir, InitInput{URL: "Blog.Example.com", Build: "./" + build})
	if err != nil {
		t.Fatalf("InitProject: %v", err)
	}
	data, _ := os.ReadFile(path)
	want := "# Mason project config — see CONFIG.md\nsite: https://blog.example.com\nbuild: dist\n"
	if string(data) != want {
		t.Fatalf("file =\n%s\nwant\n%s", data, want)
	}

	if _, err := InitProject(dir, InitInput{URL: "x.example.com", Build: "dist"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second InitProject error = %v", err)
	}
}

func TestInitProjectRejectsBadInput(t *testing.T) {
	for _, in := range []InitInput{
		{URL: "https://example.com/blog", Build: "dist"},
		{URL: "https://example.com", Build: ""},
		{URL: "https://example.com", Build: "dist", Server: "https://not-a-destination"},
	} {
		dir := t.TempDir()
		if _, err := InitProject(dir, in); err == nil {
			t.Fatalf("InitProject(%+v) succeeded, want error", in)
		}
		if _, err := os.Stat(filepath.Join(dir, ".mason")); !os.IsNotExist(err) {
			t.Fatalf("InitProject(%+v) left a file behind", in)
		}
	}
}

func TestDetectBuildDirNone(t *testing.T) {
	if got := DetectBuildDir(t.TempDir()); got != "" {
		t.Fatalf("DetectBuildDir = %q, want empty", got)
	}
}
