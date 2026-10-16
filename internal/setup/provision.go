package setup

import (
	"fmt"
	"path"
	"strings"

	"github.com/norlinga/mason/internal/config"
)

// ProvisionParams configures the generated site provisioning script. The output is a
// pure, deterministic function of these inputs.
type ProvisionParams struct {
	Site config.Site
	// SPA adds a try_files fallback to /index.html even when the site config does not.
	SPA bool
	// DomainMode forces apex/subdomain handling: "apex", "subdomain", or "" to
	// auto-detect via the Public Suffix List.
	DomainMode string
	// NoWWW suppresses the www→apex redirect block for apex domains.
	NoWWW      bool
	CaddyUser  string
	Group      string
	Caddyfile  string
	SnippetDir string
	// Reload appends `caddy validate` + reload/restart to the script.
	Reload bool
}

// Domain modes for ProvisionParams.DomainMode.
const (
	domainModeApex      = "apex"
	domainModeSubdomain = "subdomain"
)

// SnippetPath is where a site's (or the share store's) Caddy snippet lives.
func SnippetPath(snippetDir, host string) string {
	if strings.TrimSpace(snippetDir) == "" {
		snippetDir = defaultSnippetDir
	}
	return path.Join(snippetDir, host+".caddy")
}

// ProvisionScript renders an idempotent POSIX-sh script that stands up a static site:
// the web root (mason-owned, group-readable for Caddy), Caddy group access, and a
// Mason-managed static vhost named by the site's host and imported once.
func ProvisionScript(p ProvisionParams) (string, error) {
	site := p.Site
	if site.Host == "" || site.URL == "" {
		return "", fmt.Errorf("site URL is required")
	}
	if strings.TrimSpace(site.Path) == "" {
		return "", fmt.Errorf("site path is required")
	}
	user := config.DefaultUser
	if site.Server != nil && site.Server.User != "" {
		user = site.Server.User
	}
	group := orDefault(p.Group, user)
	snippetDir := orDefault(p.SnippetDir, defaultSnippetDir)

	var b strings.Builder
	writeScriptPreamble(&b, fmt.Sprintf("mason provision — %s", site.URL), serverHost(site.Server, site.Host))
	fmt.Fprintf(&b, "MASON_USER=%s\n", shellQuote(user))
	fmt.Fprintf(&b, "MASON_GROUP=%s\n", shellQuote(group))
	fmt.Fprintf(&b, "CADDY_USER=%s\n", shellQuote(orDefault(p.CaddyUser, defaultCaddyUser)))
	fmt.Fprintf(&b, "SITE_ROOT=%s\n", shellQuote(site.Path))
	fmt.Fprintf(&b, "SNIPPET_DIR=%s\n", shellQuote(snippetDir))
	fmt.Fprintf(&b, "CADDYFILE=%s\n\n", shellQuote(orDefault(p.Caddyfile, defaultCaddyfile)))

	b.WriteString("# 1. Web root (mason owns it; group-readable so Caddy can serve it)\n")
	b.WriteString("install -d -m 2750 -o \"$MASON_USER\" -g \"$MASON_GROUP\" \"$SITE_ROOT\"\n\n")

	b.WriteString("# 2. Let Caddy read the served tree (least-privilege via group)\n")
	writeCaddyGroupAccessBlock(&b)
	b.WriteString("\n")

	b.WriteString("# 3. Caddy: Mason-managed vhost + single import (never rewrites your Caddyfile)\n")
	writeSnippetInstall(&b, SnippetPath(snippetDir, site.Host), staticSnippetFor(p))
	writeCaddyImportBlock(&b, snippetDir)
	b.WriteString("\n")

	writeCaddyReloadBlock(&b, 4, p.Reload)
	return b.String(), nil
}

// staticSnippet describes the static vhost(s) to render.
type staticSnippet struct {
	siteURL      string
	serveAddress string // canonical address that serves the files (scheme+host)
	root         string
	spa          bool
	// wwwRedirect, when set, appends a second block redirecting wwwAddress to
	// redirectTarget (used for apex domains: www.example.com → https://example.com).
	wwwRedirect    bool
	wwwAddress     string
	redirectTarget string
}

// staticSnippetFor resolves apex-vs-subdomain handling and renders the Caddy snippet.
// Apex domains are served from the bare apex and (unless NoWWW) get a permanent www→apex
// redirect; subdomains are served verbatim.
func staticSnippetFor(p ProvisionParams) string {
	site := p.Site
	prefix, host := splitSiteScheme(site.URL)
	spa := p.SPA || site.SPA

	apex := isApexDomain(host)
	switch strings.ToLower(strings.TrimSpace(p.DomainMode)) {
	case domainModeApex:
		apex = true
	case domainModeSubdomain:
		apex = false
	}

	if !apex {
		return caddyStaticSnippet(staticSnippet{siteURL: site.URL, serveAddress: site.URL, root: site.Path, spa: spa})
	}

	// Normalize to the bare apex so www.example.com still serves example.com and
	// redirects www → apex. The redirect target is always absolute.
	apexHost := strings.TrimPrefix(strings.ToLower(host), "www.")
	return caddyStaticSnippet(staticSnippet{
		siteURL:        site.URL,
		serveAddress:   prefix + apexHost,
		root:           site.Path,
		spa:            spa,
		wwwRedirect:    !p.NoWWW,
		wwwAddress:     prefix + "www." + apexHost,
		redirectTarget: prefix + apexHost,
	})
}

// caddyStaticSnippet renders a static vhost: serve the root with file_server, 404ing
// dotfile paths except /.well-known/. With spa, requests that miss a file fall back to
// /index.html. For an apex domain it also emits a permanent www→apex redirect block.
func caddyStaticSnippet(s staticSnippet) string {
	var b strings.Builder
	b.WriteString(ManagedMarker + "\n")
	fmt.Fprintf(&b, "# %s — regenerated by 'mason provision'; remove the line above to hand-edit.\n", s.siteURL)
	fmt.Fprintf(&b, "%s {\n", s.serveAddress)
	fmt.Fprintf(&b, "\troot * %s\n", s.root)
	if s.spa {
		b.WriteString("\ttry_files {path} /index.html\n")
	}
	// Never serve dotfiles (.git, .env, .mason, …) even if one reaches the web root;
	// /.well-known/ stays public by design.
	b.WriteString("\t@mason_hidden {\n\t\tpath */.*\n\t\tnot path /.well-known/*\n\t}\n")
	b.WriteString("\trespond @mason_hidden 404\n")
	b.WriteString("\tfile_server\n")
	b.WriteString("}\n")
	if s.wwwRedirect {
		b.WriteString("\n")
		fmt.Fprintf(&b, "%s {\n", s.wwwAddress)
		fmt.Fprintf(&b, "\tredir %s{uri} permanent\n", s.redirectTarget)
		b.WriteString("}\n")
	}
	return b.String()
}

func orDefault(v, def string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return def
}

// serverHost is the host to show in a script's "run it like this" hint.
func serverHost(s *config.Server, fallback string) string {
	if s != nil && s.Host != "" {
		return s.Host
	}
	return fallback
}
