package setup

import (
	"fmt"
	"path"
	"strings"

	"github.com/norlinga/mason/internal/config"
)

// SiteInitParams configures the generated static-host provisioning script. Like
// RemoteInitScript, the output is a pure, deterministic function of these inputs.
// site-init assumes the mason service user, deploy key, and Caddyfile import
// discipline already exist (run remote-init once first); it only provisions a plain
// static vhost: a web root, Caddy group access, and a Mason-owned snippet.
type SiteInitParams struct {
	Target     config.Target
	TargetName string
	// Host overrides the site address derived from the target's base_url. Useful for
	// ad-hoc provisioning before a .mason exists.
	Host string
	// Root overrides the web root derived from the target's deploy.remote_path.
	Root string
	// SPA appends a try_files fallback to index.html for single-page-app builds.
	SPA bool
	// DomainMode forces apex/subdomain handling: "apex", "subdomain", or "" to
	// auto-detect via the Public Suffix List. Apex domains get a www→apex redirect
	// and are served from the bare apex; subdomains are served as-is.
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

// Domain modes for SiteInitParams.DomainMode.
const (
	domainModeApex      = "apex"
	domainModeSubdomain = "subdomain"
)

// SiteInitScript renders an idempotent POSIX-sh script that stands up a plain static
// subdomain: the web root (mason-owned, group-readable for Caddy), Caddy group access,
// and a Mason-owned static Caddy snippet imported once. It creates no users, groups,
// or SSH keys, and emits no share scaffolding or private-prefix 404 matchers.
func SiteInitScript(p SiteInitParams) (string, error) {
	user := strings.TrimSpace(p.Target.User)
	if user == "" {
		user = defaultUser
	}
	group := strings.TrimSpace(p.Group)
	if group == "" {
		group = user
	}
	caddyUser := strings.TrimSpace(p.CaddyUser)
	if caddyUser == "" {
		caddyUser = defaultCaddyUser
	}
	caddyfile := strings.TrimSpace(p.Caddyfile)
	if caddyfile == "" {
		caddyfile = defaultCaddyfile
	}
	snippetDir := strings.TrimSpace(p.SnippetDir)
	if snippetDir == "" {
		snippetDir = defaultSnippetDir
	}
	name := strings.TrimSpace(p.TargetName)
	if name == "" {
		name = defaultTargetName
	}

	siteAddress := strings.TrimSpace(p.Host)
	if siteAddress == "" {
		var err error
		if siteAddress, err = siteAddressFromBaseURL(p.Target.BaseURL); err != nil {
			return "", err
		}
	}

	siteRoot := strings.TrimSpace(p.Root)
	if siteRoot == "" {
		siteRoot = strings.TrimSpace(p.Target.Deploy.RemotePath)
	}
	if siteRoot == "" {
		return "", fmt.Errorf("web root is required: set deploy.remote_path on the target or pass --root")
	}

	snippetGlob := path.Join(snippetDir, "*.caddy")
	snippetFile := path.Join(snippetDir, name+".caddy")

	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	fmt.Fprintf(&b, "# mason site-init — target %q (%s)\n", name, siteAddress)
	b.WriteString("# Idempotent static-host provisioning. Assumes 'mason remote-init' already ran\n")
	b.WriteString("# (mason user, deploy key, Caddyfile import discipline). Review, then run as an\n")
	b.WriteString("# admin, e.g.:\n")
	fmt.Fprintf(&b, "#   ssh %s@%s 'sh -s' < this-script.sh\n", "root", hostOnly(siteAddress))
	b.WriteString("set -eu\n\n")

	fmt.Fprintf(&b, "MASON_USER=%s\n", shellQuote(user))
	fmt.Fprintf(&b, "MASON_GROUP=%s\n", shellQuote(group))
	fmt.Fprintf(&b, "CADDY_USER=%s\n", shellQuote(caddyUser))
	fmt.Fprintf(&b, "SITE_ROOT=%s\n", shellQuote(siteRoot))
	fmt.Fprintf(&b, "SNIPPET_DIR=%s\n", shellQuote(snippetDir))
	fmt.Fprintf(&b, "CADDYFILE=%s\n\n", shellQuote(caddyfile))

	b.WriteString("# 1. Static web root (mason owns it; group-readable so Caddy can serve it)\n")
	b.WriteString("install -d -m 2750 -o \"$MASON_USER\" -g \"$MASON_GROUP\" \"$SITE_ROOT\"\n\n")

	b.WriteString("# 2. Let Caddy read the served tree (least-privilege via group)\n")
	writeCaddyGroupAccessBlock(&b)
	b.WriteString("\n")

	b.WriteString("# 3. Caddy: Mason-owned snippet + single import (never rewrites your Caddyfile)\n")
	b.WriteString("install -d -m 755 \"$SNIPPET_DIR\"\n")
	fmt.Fprintf(&b, "cat > %s <<'CADDY'\n", shellQuote(snippetFile))
	b.WriteString(staticSnippetFor(siteAddress, siteRoot, p))
	b.WriteString("CADDY\n")
	writeCaddyImportBlock(&b, snippetGlob)
	b.WriteString("\n")

	writeCaddyReloadBlock(&b, 4, p.Reload)

	return b.String(), nil
}

// staticSnippet describes the static vhost(s) to render.
type staticSnippet struct {
	serveAddress string // canonical address that serves the files (scheme+host)
	root         string
	spa          bool
	// wwwRedirect, when set, appends a second block redirecting wwwAddress to
	// redirectTarget (used for apex domains: www.example.com → https://example.com).
	wwwRedirect    bool
	wwwAddress     string
	redirectTarget string
}

// staticSnippetFor resolves apex-vs-subdomain handling from the params and renders the
// Caddy snippet. Apex domains are served from the bare apex and (unless NoWWW) get a
// permanent www→apex redirect; subdomains are served verbatim, exactly as before.
func staticSnippetFor(siteAddress, root string, p SiteInitParams) string {
	prefix, host := splitSiteScheme(siteAddress)

	apex := isApexDomain(host)
	switch strings.ToLower(strings.TrimSpace(p.DomainMode)) {
	case domainModeApex:
		apex = true
	case domainModeSubdomain:
		apex = false
	}

	if !apex {
		return caddyStaticSnippet(staticSnippet{serveAddress: siteAddress, root: root, spa: p.SPA})
	}

	// Normalize to the bare apex so a base_url of www.example.com still serves example.com
	// and redirects www → apex. The redirect target is always an absolute https URL
	// (apex domains imply automatic TLS).
	apexHost := strings.TrimPrefix(strings.ToLower(host), "www.")
	redirectScheme := prefix
	if redirectScheme == "" {
		redirectScheme = "https://"
	}
	return caddyStaticSnippet(staticSnippet{
		serveAddress:   prefix + apexHost,
		root:           root,
		spa:            p.SPA,
		wwwRedirect:    !p.NoWWW,
		wwwAddress:     prefix + "www." + apexHost,
		redirectTarget: redirectScheme + apexHost,
	})
}

// caddyStaticSnippet renders a plain static vhost: serve the root with file_server.
// Unlike caddyShareSnippet there is no /objects/* or /.mason/* 404 matcher. With spa,
// requests that miss a file fall back to /index.html for client-side routing. For an
// apex domain it also emits a permanent www→apex redirect block so both names work.
func caddyStaticSnippet(s staticSnippet) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s {\n", s.serveAddress)
	fmt.Fprintf(&b, "\troot * %s\n", s.root)
	if s.spa {
		b.WriteString("\ttry_files {path} /index.html\n")
	}
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
