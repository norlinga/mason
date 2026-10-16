package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture lays out a fake home with ~/.mason and an optional project .mason, returning
// LoadOptions that start discovery in a subdirectory of the project.
func fixture(t *testing.T, user, project string) LoadOptions {
	t.Helper()
	home := t.TempDir()
	userPath := filepath.Join(home, FileName)
	if user != "" {
		write(t, userPath, user)
	}
	proj := filepath.Join(home, "code", "site")
	sub := filepath.Join(proj, "src", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if project != "" {
		write(t, filepath.Join(proj, FileName), project)
	}
	return LoadOptions{Dir: sub, UserFile: userPath}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const userConfig = "server: example.com\nshare: https://files.example.com\n"

func TestTwoLineProjectInheritsServer(t *testing.T) {
	cfg, err := Load(fixture(t, userConfig, "site: https://recipes.example.com\nbuild: dist\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	site, err := cfg.Site("")
	if err != nil {
		t.Fatalf("Site: %v", err)
	}
	want := Site{
		Name: "recipes.example.com", URL: "https://recipes.example.com", Host: "recipes.example.com",
		Path: "/var/www/recipes.example.com", Build: "dist",
	}
	if site.Server == nil || site.Server.String() != "mason@example.com" {
		t.Fatalf("server = %v, want mason@example.com", site.Server)
	}
	site.Server = nil
	if site != want {
		t.Fatalf("site = %+v\nwant   %+v", site, want)
	}
}

func TestEnvironmentsFirstIsDefault(t *testing.T) {
	cfg, err := Load(fixture(t, userConfig, `build: dist
sites:
  staging: https://staging.shop.example.com
  live: https://shop.example.com
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	def, err := cfg.Site("")
	if err != nil || def.Name != "staging" {
		t.Fatalf("default = %q, %v; want staging", def.Name, err)
	}
	for _, sel := range []string{"live", "shop.example.com", "https://shop.example.com"} {
		live, err := cfg.Site(sel)
		if err != nil || live.Name != "live" || live.Path != "/var/www/shop.example.com" {
			t.Fatalf("Site(%q) = %+v, %v", sel, live, err)
		}
	}
	if _, err := cfg.Site("prod"); err == nil || !strings.Contains(err.Error(), "have: staging, live") {
		t.Fatalf("unknown site error = %v", err)
	}
}

func TestSiteOverrides(t *testing.T) {
	cfg, err := Load(fixture(t, userConfig, `build: .
site:
  url: family.example.com
  path: /var/www/family
  server: deploy@hon.example.com:2222
  spa: true
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	site, err := cfg.Site("")
	if err != nil {
		t.Fatalf("Site: %v", err)
	}
	if site.URL != "https://family.example.com" || site.Path != "/var/www/family" || !site.SPA {
		t.Fatalf("site = %+v", site)
	}
	if got := site.Server.String(); got != "deploy@hon.example.com:2222" {
		t.Fatalf("server = %s", got)
	}
}

func TestShareComesOnlyFromUserFile(t *testing.T) {
	cfg, err := Load(fixture(t, `server: example.com
key: ~/.ssh/mason
share:
  url: https://files.example.com
  path: /var/www/share
  inbox: inbox/
`, "site: https://recipes.example.com\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	share, err := cfg.RequireShare()
	if err != nil {
		t.Fatalf("RequireShare: %v", err)
	}
	if share.Path != "/var/www/share" || share.Inbox != "inbox/" || share.Host != "files.example.com" {
		t.Fatalf("share = %+v", share)
	}
	if share.Server.Key != "~/.ssh/mason" {
		t.Fatalf("share key = %q", share.Server.Key)
	}

	_, err = Load(fixture(t, userConfig, "site: https://x.example.com\nshare: https://evil.example.com\n"))
	if err == nil || !strings.Contains(err.Error(), "share is configured once in ~/.mason") {
		t.Fatalf("project share error = %v", err)
	}
}

func TestMisplacedAndUnknownKeys(t *testing.T) {
	tests := []struct {
		name, user, project, want string
	}{
		{"site in user file", "server: a.example.com\nsite: https://x.example.com\n", "", "sites belong in a project .mason"},
		{"v1 key renamed", userConfig, "site: https://x.example.com\nbuild_dir: dist\n", `"build_dir" (v1 name — in v2 this is build`},
		{"typo", userConfig, "site: https://x.example.com\nbiuld: dist\n", `did you mean "build"`},
		{"site field typo", userConfig, "site:\n  url: https://x.example.com\n  pth: /srv/x\n", `did you mean "path"`},
		{"both site and sites", userConfig, "site: https://x.example.com\nsites:\n  a: https://y.example.com\n", "either site: or sites:"},
		{"duplicate host", userConfig, "sites:\n  a: https://x.example.com\n  b: x.example.com\n", "both use host x.example.com"},
		{"duplicate key", userConfig, "build: a\nbuild: b\n", `duplicate key "build"`},
		{"url with path", userConfig, "site: https://x.example.com/blog\n", "must be just scheme://host"},
		{"server as url", "server: https://example.com\n", "", "SSH destination"},
		{"bad port", "server: example.com:99999\n", "", "invalid port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(fixture(t, tt.user, tt.project))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), ".mason") {
				t.Fatalf("error should name the file: %v", err)
			}
		})
	}
}

func TestV1FormatIsRejected(t *testing.T) {
	v1 := `default: home
targets:
  home:
    host: example.com
    user: mason
    root: /var/www/docs.example.com
    base_url: https://docs.example.com
`
	_, err := Load(fixture(t, userConfig, v1))
	var v1Err *V1FormatError
	if !errors.As(err, &v1Err) || !strings.Contains(err.Error(), "CONFIG.md") {
		t.Fatalf("error = %v, want V1FormatError mentioning CONFIG.md", err)
	}
}

func TestLazyErrors(t *testing.T) {
	// Nothing configured at all: Load succeeds; asking for things explains what is missing.
	cfg, err := Load(fixture(t, "", ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := cfg.Site(""); err == nil || !strings.Contains(err.Error(), "mason init") {
		t.Fatalf("Site error = %v", err)
	}
	if _, err := cfg.RequireShare(); err == nil || !strings.Contains(err.Error(), "add 'share:") {
		t.Fatalf("RequireShare error = %v", err)
	}

	cfg, err = Load(fixture(t, "", "site: https://x.example.com\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := cfg.Site(""); err == nil || !strings.Contains(err.Error(), "mason setup <host>") {
		t.Fatalf("no-server error = %v", err)
	}
}

func TestHomeDirIsNotAProject(t *testing.T) {
	opts := fixture(t, userConfig, "")
	opts.Dir = filepath.Dir(opts.UserFile)
	cfg, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ProjectFile != "" || cfg.UserFile != opts.UserFile {
		t.Fatalf("project = %q user = %q; ~/.mason must load only as the user file", cfg.ProjectFile, cfg.UserFile)
	}
}

func TestProjectServerAndKeyOverrideUser(t *testing.T) {
	cfg, err := Load(fixture(t, "server: example.com\nkey: ~/.ssh/a\n", "server: ops@other.example.com\nsite: x.example.com\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	site, _ := cfg.Site("")
	if site.Server.String() != "ops@other.example.com" || site.Server.Key != "~/.ssh/a" {
		t.Fatalf("server = %+v", site.Server)
	}

	cfg, err = Load(fixture(t, "server: example.com\nkey: ~/.ssh/a\n", "key: ~/.ssh/b\nsite: x.example.com\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	site, _ = cfg.Site("")
	if site.Server.Host != "example.com" || site.Server.Key != "~/.ssh/b" {
		t.Fatalf("server = %+v", site.Server)
	}
}

func TestParseServer(t *testing.T) {
	tests := map[string]Server{
		"example.com":           {User: "mason", Host: "example.com"},
		"root@example.com":      {User: "root", Host: "example.com"},
		"example.com:2222":      {User: "mason", Host: "example.com", Port: 2222},
		"me@[2001:db8::1]:2222": {User: "me", Host: "2001:db8::1", Port: 2222},
		"2001:db8::1":           {User: "mason", Host: "2001:db8::1"},
		"[2001:db8::1]":         {User: "mason", Host: "2001:db8::1"},
		"my-alias":               {User: "mason", Host: "my-alias"},
	}
	for in, want := range tests {
		got, err := ParseServer(in, "")
		if err != nil || got != want {
			t.Errorf("ParseServer(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	if got := (Server{User: "me", Host: "2001:db8::1", Port: 22}).String(); got != "me@[2001:db8::1]:22" {
		t.Errorf("String = %q", got)
	}
}

func TestEmptyFiles(t *testing.T) {
	cfg, err := Load(fixture(t, "\n", "# nothing yet\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server != nil || len(cfg.Sites()) != 0 {
		t.Fatalf("cfg = %+v", cfg)
	}
}
