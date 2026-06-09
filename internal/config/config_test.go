package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validConfig = `
default: home
targets:
  home:
    host: share.example.com
    user: mason
    root: /srv/share
    base_url: https://share.example.com
    key_path: ~/.ssh/id_ed25519
    port: 22
    share:
      default_path: inbox/
    deploy:
      build_dir: ./public
      remote_path: /srv/site
`

func TestParseValidConfig(t *testing.T) {
	cfg, err := Parse([]byte(validConfig))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	target, err := cfg.ResolveTarget("")
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if target.Name != "home" {
		t.Fatalf("target name = %q, want home", target.Name)
	}
	if target.Target.Share.DefaultPath != "inbox/" {
		t.Fatalf("share.default_path = %q, want inbox/", target.Target.Share.DefaultPath)
	}
	if target.Target.Deploy.BuildDir != "./public" {
		t.Fatalf("deploy.build_dir = %q, want ./public", target.Target.Deploy.BuildDir)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := Parse([]byte(`
default: home
targets:
  home:
    host: share.example.com
    user: mason
    root: /srv/share
    base_url: https://share.example.com
    unexpected: true
`))
	if err == nil {
		t.Fatalf("Parse succeeded, want unknown field error")
	}
	if !strings.Contains(err.Error(), "field unexpected not found") {
		t.Fatalf("error = %q, want unknown field message", err)
	}
}

func TestValidateRejectsInvalidConfigs(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "missing default",
			yaml: `
targets:
  home:
    host: share.example.com
    user: mason
    root: /srv/share
    base_url: https://share.example.com
`,
			want: "default target is required",
		},
		{
			name: "default missing from targets",
			yaml: `
default: home
targets:
  work:
    host: share.example.com
    user: mason
    root: /srv/share
    base_url: https://share.example.com
`,
			want: `default target "home" is not defined`,
		},
		{
			name: "missing target field",
			yaml: `
default: home
targets:
  home:
    user: mason
    root: /srv/share
    base_url: https://share.example.com
`,
			want: `target "home" requires host`,
		},
		{
			name: "invalid port",
			yaml: `
default: home
targets:
  home:
    host: share.example.com
    user: mason
    root: /srv/share
    base_url: https://share.example.com
    port: 70000
`,
			want: `target "home" has invalid port 70000`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil {
				t.Fatalf("Parse succeeded, want error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestResolveTargetOverride(t *testing.T) {
	cfg, err := Parse([]byte(`
default: home
targets:
  home:
    host: share.example.com
    user: mason
    root: /srv/share
    base_url: https://share.example.com
  work:
    host: work.example.com
    user: deploy
    root: /srv/work
    base_url: https://work.example.com
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	target, err := cfg.ResolveTarget("work")
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if target.Name != "work" {
		t.Fatalf("target name = %q, want work", target.Name)
	}
	if target.Target.Host != "work.example.com" {
		t.Fatalf("host = %q, want work.example.com", target.Target.Host)
	}

	_, err = cfg.ResolveTarget("missing")
	if err == nil {
		t.Fatalf("ResolveTarget missing succeeded, want error")
	}
}

func TestDiscoverFindsNearestProjectConfig(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	parentConfig := filepath.Join(root, projectConfigName)
	writeFile(t, parentConfig, validConfig)

	child := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}

	got, err := Discover(child)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got != parentConfig {
		t.Fatalf("Discover = %q, want %q", got, parentConfig)
	}
}

func TestDiscoverFallsBackToHomeConfig(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	homeConfig := filepath.Join(home, projectConfigName)
	writeFile(t, homeConfig, validConfig)

	got, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got != homeConfig {
		t.Fatalf("Discover = %q, want %q", got, homeConfig)
	}
}

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), projectConfigName)
	writeFile(t, path, validConfig)

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Default != "home" {
		t.Fatalf("default = %q, want home", cfg.Default)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
