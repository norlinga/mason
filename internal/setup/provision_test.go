package setup

import (
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/config"
)

func staticSite() config.Site {
	return config.Site{
		Name:   "blog.example.com",
		URL:    "https://blog.example.com",
		Host:   "blog.example.com",
		Path:   "/var/www/blog.example.com",
		Server: &config.Server{User: "mason", Host: "vps.example.net"},
	}
}

func staticParams() ProvisionParams {
	return ProvisionParams{Site: staticSite(), Reload: true}
}

// apexParams returns params for an apex-domain site (example.com), the generic
// counterpart to the subdomain staticParams (blog.example.com).
func apexParams() ProvisionParams {
	p := staticParams()
	p.Site.URL, p.Site.Host, p.Site.Name = "https://example.com", "example.com", "example.com"
	p.Site.Path = "/var/www/example.com"
	return p
}

func TestProvisionScriptInvariants(t *testing.T) {
	script, err := ProvisionScript(staticParams())
	if err != nil {
		t.Fatalf("ProvisionScript: %v", err)
	}

	wantContains := []string{
		"set -eu",
		// static web root, group-readable for Caddy
		"install -d -m 2750 -o \"$MASON_USER\" -g \"$MASON_GROUP\" \"$SITE_ROOT\"",
		"SITE_ROOT='/var/www/blog.example.com'",
		// caddy reads via group
		"usermod -aG \"$MASON_GROUP\" \"$CADDY_USER\"",
		// plain static vhost
		"https://blog.example.com {",
		"file_server",
		// dotfiles are never served, but /.well-known/ stays public
		"\t@mason_hidden {\n\t\tpath */.*\n\t\tnot path /.well-known/*\n\t}\n\trespond @mason_hidden 404\n\tfile_server\n",
		// single import line, idempotent
		"grep -qF 'import /etc/caddy/mason.d/*.caddy' \"$CADDYFILE\" 2>/dev/null ||",
		// snippet named by the site host, managed marker first
		"SNIPPET_FILE='/etc/caddy/mason.d/blog.example.com.caddy'",
		"cat > \"$SNIPPET_FILE.mason-new\" <<'CADDY'\n# managed by mason\n",
		// reload guarded by validate
		"caddy validate --config \"$CADDYFILE\"",
		"systemctl reload caddy",
	}
	for _, want := range wantContains {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q\n---\n%s", want, script)
		}
	}

	// site-init must NOT carry any share scaffolding or private-prefix matchers.
	for _, banned := range []string{
		"@mason_private",
		"/objects/*",
		"/.mason/*",
		"useradd",
		"groupadd",
		"authorized_keys",
		"SHARE_ROOT",
	} {
		if strings.Contains(script, banned) {
			t.Fatalf("static script must not contain %q\n---\n%s", banned, script)
		}
	}
}

func TestProvisionScriptDeterministic(t *testing.T) {
	a, err := ProvisionScript(staticParams())
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := ProvisionScript(staticParams())
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a != b {
		t.Fatal("ProvisionScript is not deterministic for identical input")
	}
}

func TestProvisionScriptSPA(t *testing.T) {
	p := staticParams()
	p.SPA = true
	script, err := ProvisionScript(p)
	if err != nil {
		t.Fatalf("ProvisionScript: %v", err)
	}
	if !strings.Contains(script, "try_files {path} /index.html") {
		t.Fatalf("--spa missing try_files fallback\n%s", script)
	}

	plain, _ := ProvisionScript(staticParams())
	if strings.Contains(plain, "try_files") {
		t.Fatalf("non-spa script must not emit try_files\n%s", plain)
	}
}

func TestProvisionScriptNoReload(t *testing.T) {
	p := staticParams()
	p.Reload = false
	script, err := ProvisionScript(p)
	if err != nil {
		t.Fatalf("ProvisionScript: %v", err)
	}
	if strings.Contains(script, "if [ \"$CADDY_GROUP_CHANGED\" -eq 1 ]; then") {
		t.Fatal("no-reload still emits an active apply block")
	}
	if !strings.Contains(script, "Apply the change yourself") {
		t.Fatalf("missing manual-apply hint\n%s", script)
	}
}

func TestProvisionScriptRestartsWhenGroupChanges(t *testing.T) {
	script, err := ProvisionScript(staticParams()) // Reload: true
	if err != nil {
		t.Fatalf("ProvisionScript: %v", err)
	}
	for _, want := range []string{
		"CADDY_GROUP_CHANGED=1",
		"if [ \"$CADDY_GROUP_CHANGED\" -eq 1 ]; then",
		"systemctl restart caddy",
		"systemctl reload caddy",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q\n---\n%s", want, script)
		}
	}
}

func TestProvisionScriptRequiresRoot(t *testing.T) {
	p := staticParams()
	p.Site.Path = ""
	if _, err := ProvisionScript(p); err == nil {
		t.Fatal("missing web root err = nil, want error")
	}
}

func TestProvisionScriptRequiresHost(t *testing.T) {
	p := staticParams()
	p.Site.URL, p.Site.Host = "", ""
	if _, err := ProvisionScript(p); err == nil {
		t.Fatal("missing site address err = nil, want error")
	}
}

func TestProvisionScriptSubdomainNoRedirect(t *testing.T) {
	// A subdomain must stay a single plain block with no www handling.
	script, err := ProvisionScript(staticParams())
	if err != nil {
		t.Fatalf("ProvisionScript: %v", err)
	}
	if strings.Contains(script, "redir") || strings.Contains(script, "www.") {
		t.Fatalf("subdomain script must not emit a www redirect\n%s", script)
	}
}

func TestProvisionScriptApexDomain(t *testing.T) {
	script, err := ProvisionScript(apexParams())
	if err != nil {
		t.Fatalf("ProvisionScript: %v", err)
	}
	for _, want := range []string{
		"https://example.com {",
		"root * /var/www/example.com",
		"https://www.example.com {",
		"redir https://example.com{uri} permanent",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("apex script missing %q\n---\n%s", want, script)
		}
	}
}

func TestProvisionScriptApexNormalizesWWW(t *testing.T) {
	// A base_url that already carries www should still serve the bare apex and
	// redirect www → apex (not serve www and redirect apex → www).
	p := apexParams()
	p.Site.URL, p.Site.Host = "https://www.example.com", "www.example.com"
	script, err := ProvisionScript(p)
	if err != nil {
		t.Fatalf("ProvisionScript: %v", err)
	}
	if !strings.Contains(script, "https://example.com {\n\troot * /var/www/example.com") {
		t.Fatalf("www base_url should serve the bare apex\n%s", script)
	}
	if !strings.Contains(script, "https://www.example.com {\n\tredir https://example.com{uri} permanent") {
		t.Fatalf("www base_url should redirect www → apex\n%s", script)
	}
}

func TestProvisionScriptApexNoWWW(t *testing.T) {
	p := apexParams()
	p.NoWWW = true
	script, err := ProvisionScript(p)
	if err != nil {
		t.Fatalf("ProvisionScript: %v", err)
	}
	if strings.Contains(script, "redir") || strings.Contains(script, "www.example.com") {
		t.Fatalf("--no-www must omit the redirect block\n%s", script)
	}
	if !strings.Contains(script, "https://example.com {") {
		t.Fatalf("apex still served with --no-www\n%s", script)
	}
}

func TestProvisionScriptDomainModeOverride(t *testing.T) {
	// Force apex on a subdomain host: redirect appears.
	p := staticParams()
	p.DomainMode = "apex"
	script, err := ProvisionScript(p)
	if err != nil {
		t.Fatalf("apex override: %v", err)
	}
	if !strings.Contains(script, "redir") {
		t.Fatalf("--apex override should add a www redirect\n%s", script)
	}

	// Force subdomain on an apex host: no redirect.
	p = apexParams()
	p.DomainMode = "subdomain"
	script, err = ProvisionScript(p)
	if err != nil {
		t.Fatalf("subdomain override: %v", err)
	}
	if strings.Contains(script, "redir") {
		t.Fatalf("--subdomain override should suppress the www redirect\n%s", script)
	}
}

func TestIsApexDomain(t *testing.T) {
	tests := map[string]bool{
		"example.com":          true,
		"EXAMPLE.com":          true,
		"www.example.com":      true,
		"example.co.uk":        true,  // multi-label TLD apex
		"blog.example.com":     false, // subdomain
		"blog.example.co.uk":   false, // subdomain under a multi-label TLD
		"www.blog.example.com": false, // www in front of a subdomain
		"localhost":            false,
		"":                     false,
	}
	for host, want := range tests {
		if got := isApexDomain(host); got != want {
			t.Errorf("isApexDomain(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestProvisionGroupDefault(t *testing.T) {
	p := staticParams()
	p.Site.Server.User = "publisher"
	script, err := ProvisionScript(p)
	if err != nil {
		t.Fatalf("ProvisionScript: %v", err)
	}
	if !strings.Contains(script, "MASON_USER='publisher'") || !strings.Contains(script, "MASON_GROUP='publisher'") {
		t.Fatalf("user/group not defaulted together\n%s", script)
	}
}
