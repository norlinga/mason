package setup

import (
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/config"
)

func staticTarget() config.Target {
	return config.Target{
		Host:    "vps.example.net",
		User:    "mason",
		Root:    "/var/www/blog.example.com",
		BaseURL: "https://blog.example.com",
		Deploy:  config.DeployConfig{RemotePath: "/var/www/blog.example.com"},
	}
}

func staticParams() SiteInitParams {
	return SiteInitParams{
		Target:     staticTarget(),
		TargetName: "home",
		Reload:     true,
	}
}

// apexParams returns params for an apex-domain target (example.com), the
// generic counterpart to the subdomain staticParams (blog.example.com).
func apexParams() SiteInitParams {
	p := staticParams()
	p.Target.Root = "/var/www/example.com"
	p.Target.BaseURL = "https://example.com"
	p.Target.Deploy.RemotePath = "/var/www/example.com"
	return p
}

func TestSiteInitScriptInvariants(t *testing.T) {
	script, err := SiteInitScript(staticParams())
	if err != nil {
		t.Fatalf("SiteInitScript: %v", err)
	}

	wantContains := []string{
		"set -eu",
		// static web root, group-readable for Caddy
		"install -d -m 2750 -o \"$MASON_USER\" -g \"$MASON_GROUP\" \"$SITE_ROOT\"",
		"SITE_ROOT='/var/www/blog.example.com'",
		// caddy reads via group
		"usermod -aG \"$MASON_GROUP\" \"$CADDY_USER\"",
		// plain static vhost
		"blog.example.com {",
		"file_server",
		// single import line, idempotent
		"grep -qF 'import /etc/caddy/mason.d/*.caddy' \"$CADDYFILE\" 2>/dev/null ||",
		// snippet written to per-target file
		"cat > '/etc/caddy/mason.d/home.caddy' <<'CADDY'",
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

func TestSiteInitScriptDeterministic(t *testing.T) {
	a, err := SiteInitScript(staticParams())
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := SiteInitScript(staticParams())
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a != b {
		t.Fatal("SiteInitScript is not deterministic for identical input")
	}
}

func TestSiteInitScriptSPA(t *testing.T) {
	p := staticParams()
	p.SPA = true
	script, err := SiteInitScript(p)
	if err != nil {
		t.Fatalf("SiteInitScript: %v", err)
	}
	if !strings.Contains(script, "try_files {path} /index.html") {
		t.Fatalf("--spa missing try_files fallback\n%s", script)
	}

	plain, _ := SiteInitScript(staticParams())
	if strings.Contains(plain, "try_files") {
		t.Fatalf("non-spa script must not emit try_files\n%s", plain)
	}
}

func TestSiteInitScriptNoReload(t *testing.T) {
	p := staticParams()
	p.Reload = false
	script, err := SiteInitScript(p)
	if err != nil {
		t.Fatalf("SiteInitScript: %v", err)
	}
	if strings.Contains(script, "if [ \"$CADDY_GROUP_CHANGED\" -eq 1 ]; then") {
		t.Fatal("no-reload still emits an active apply block")
	}
	if !strings.Contains(script, "Apply the change yourself") {
		t.Fatalf("missing manual-apply hint\n%s", script)
	}
}

func TestSiteInitScriptRestartsWhenGroupChanges(t *testing.T) {
	script, err := SiteInitScript(staticParams()) // Reload: true
	if err != nil {
		t.Fatalf("SiteInitScript: %v", err)
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

func TestSiteInitScriptHostOverride(t *testing.T) {
	p := staticParams()
	p.Target.BaseURL = "" // force reliance on the override
	p.Host = "staging.example.com"
	script, err := SiteInitScript(p)
	if err != nil {
		t.Fatalf("SiteInitScript: %v", err)
	}
	if !strings.Contains(script, "staging.example.com {") {
		t.Fatalf("--host override not used as site address\n%s", script)
	}
}

func TestSiteInitScriptRootOverride(t *testing.T) {
	p := staticParams()
	p.Target.Deploy.RemotePath = "" // force reliance on the override
	p.Root = "/srv/custom"
	script, err := SiteInitScript(p)
	if err != nil {
		t.Fatalf("SiteInitScript: %v", err)
	}
	if !strings.Contains(script, "SITE_ROOT='/srv/custom'") {
		t.Fatalf("--root override not used as web root\n%s", script)
	}
}

func TestSiteInitScriptRequiresRoot(t *testing.T) {
	p := staticParams()
	p.Target.Deploy.RemotePath = ""
	if _, err := SiteInitScript(p); err == nil {
		t.Fatal("missing web root err = nil, want error")
	}
}

func TestSiteInitScriptRequiresHost(t *testing.T) {
	p := staticParams()
	p.Target.BaseURL = ""
	if _, err := SiteInitScript(p); err == nil {
		t.Fatal("missing site address err = nil, want error")
	}
}

func TestSiteInitScriptSubdomainNoRedirect(t *testing.T) {
	// A subdomain must stay a single plain block with no www handling.
	script, err := SiteInitScript(staticParams())
	if err != nil {
		t.Fatalf("SiteInitScript: %v", err)
	}
	if strings.Contains(script, "redir") || strings.Contains(script, "www.") {
		t.Fatalf("subdomain script must not emit a www redirect\n%s", script)
	}
}

func TestSiteInitScriptApexDomain(t *testing.T) {
	script, err := SiteInitScript(apexParams())
	if err != nil {
		t.Fatalf("SiteInitScript: %v", err)
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

func TestSiteInitScriptApexNormalizesWWW(t *testing.T) {
	// A base_url that already carries www should still serve the bare apex and
	// redirect www → apex (not serve www and redirect apex → www).
	p := apexParams()
	p.Target.BaseURL = "https://www.example.com"
	script, err := SiteInitScript(p)
	if err != nil {
		t.Fatalf("SiteInitScript: %v", err)
	}
	if !strings.Contains(script, "https://example.com {\n\troot * /var/www/example.com") {
		t.Fatalf("www base_url should serve the bare apex\n%s", script)
	}
	if !strings.Contains(script, "https://www.example.com {\n\tredir https://example.com{uri} permanent") {
		t.Fatalf("www base_url should redirect www → apex\n%s", script)
	}
}

func TestSiteInitScriptApexNoWWW(t *testing.T) {
	p := apexParams()
	p.NoWWW = true
	script, err := SiteInitScript(p)
	if err != nil {
		t.Fatalf("SiteInitScript: %v", err)
	}
	if strings.Contains(script, "redir") || strings.Contains(script, "www.example.com") {
		t.Fatalf("--no-www must omit the redirect block\n%s", script)
	}
	if !strings.Contains(script, "https://example.com {") {
		t.Fatalf("apex still served with --no-www\n%s", script)
	}
}

func TestSiteInitScriptDomainModeOverride(t *testing.T) {
	// Force apex on a subdomain host: redirect appears.
	p := staticParams()
	p.DomainMode = "apex"
	script, err := SiteInitScript(p)
	if err != nil {
		t.Fatalf("apex override: %v", err)
	}
	if !strings.Contains(script, "redir") {
		t.Fatalf("--apex override should add a www redirect\n%s", script)
	}

	// Force subdomain on an apex host: no redirect.
	p = apexParams()
	p.DomainMode = "subdomain"
	script, err = SiteInitScript(p)
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

func TestSiteInitGroupDefault(t *testing.T) {
	p := staticParams()
	p.Target.User = "publisher"
	script, err := SiteInitScript(p)
	if err != nil {
		t.Fatalf("SiteInitScript: %v", err)
	}
	if !strings.Contains(script, "MASON_USER='publisher'") || !strings.Contains(script, "MASON_GROUP='publisher'") {
		t.Fatalf("user/group not defaulted together\n%s", script)
	}
}
