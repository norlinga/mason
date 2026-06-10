package setup

import (
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// Defaults shared by the Caddy-touching bootstrap scripts (remote-init, site-init).
const (
	defaultCaddyUser  = "caddy"
	defaultCaddyfile  = "/etc/caddy/Caddyfile"
	defaultSnippetDir = "/etc/caddy/mason.d"
)

// writeCaddyGroupAccessBlock emits the least-privilege step that adds Caddy's user
// to the mason group so it can read the served tree. It sets CADDY_GROUP_CHANGED=1
// when membership actually changed; the reload block uses that to choose restart vs
// reload (a reload does not re-exec Caddy, so a new supplementary group is only
// picked up on restart). Callers write their own numbered section comment.
func writeCaddyGroupAccessBlock(b *strings.Builder) {
	b.WriteString("CADDY_GROUP_CHANGED=0\n")
	b.WriteString("if id \"$CADDY_USER\" >/dev/null 2>&1; then\n")
	b.WriteString("\tif ! id -nG \"$CADDY_USER\" | grep -qw \"$MASON_GROUP\"; then\n")
	b.WriteString("\t\tusermod -aG \"$MASON_GROUP\" \"$CADDY_USER\"\n")
	b.WriteString("\t\tCADDY_GROUP_CHANGED=1\n")
	b.WriteString("\tfi\n")
	b.WriteString("fi\n")
}

// writeCaddyImportBlock appends the single, idempotent `import <dir>/*.caddy` line to
// the main Caddyfile. It never rewrites anything else in that file. snippetGlob is the
// fully-joined glob (e.g. /etc/caddy/mason.d/*.caddy) used for the grep -qF guard.
func writeCaddyImportBlock(b *strings.Builder, snippetGlob string) {
	fmt.Fprintf(b, "grep -qF %s \"$CADDYFILE\" 2>/dev/null || printf '\\nimport %%s/*.caddy\\n' \"$SNIPPET_DIR\" >> \"$CADDYFILE\"\n",
		shellQuote("import "+snippetGlob))
}

// writeCaddyReloadBlock emits the validate-then-apply step as numbered section. When
// reload is true the script validates and reloads (or restarts when CADDY_GROUP_CHANGED),
// gating any apply on `caddy validate`. When false it only prints the manual steps so
// the operator applies the change after reviewing it. section is the step number used
// in the leading comment so each script keeps its own contiguous numbering.
func writeCaddyReloadBlock(b *strings.Builder, section int, reload bool) {
	if reload {
		fmt.Fprintf(b, "# %d. Validate, then restart Caddy if its group changed (a reload does not\n", section)
		b.WriteString("#    re-exec the process, so new group membership is only picked up on restart);\n")
		b.WriteString("#    otherwise a reload is enough.\n")
		b.WriteString("caddy validate --config \"$CADDYFILE\"\n")
		b.WriteString("if [ \"$CADDY_GROUP_CHANGED\" -eq 1 ]; then\n")
		b.WriteString("\tsystemctl restart caddy\n")
		b.WriteString("else\n")
		b.WriteString("\tsystemctl reload caddy\n")
		b.WriteString("fi\n")
		return
	}
	fmt.Fprintf(b, "# %d. Apply the change yourself once you've reviewed it. Use restart (not reload)\n", section)
	b.WriteString("#    on first run so Caddy picks up its new group membership:\n")
	b.WriteString("#   caddy validate --config \"$CADDYFILE\" && systemctl restart caddy\n")
}

// siteAddressFromBaseURL reduces a base_url to a Caddy site address (scheme://host),
// dropping any path so the snippet block is stable.
func siteAddressFromBaseURL(baseURL string) (string, error) {
	raw := strings.TrimSpace(baseURL)
	if raw == "" {
		return "", fmt.Errorf("target base_url is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid base_url %q", baseURL)
	}
	if u.Scheme == "" {
		return u.Host, nil
	}
	return u.Scheme + "://" + u.Host, nil
}

// hostOnly strips a scheme from a site address for display in run hints.
func hostOnly(siteAddress string) string {
	if _, host, ok := strings.Cut(siteAddress, "://"); ok {
		return host
	}
	return siteAddress
}

// splitSiteScheme splits a site address into its scheme prefix ("https://" or "")
// and the bare host, so callers can rebuild related addresses (e.g. the www variant)
// while preserving the original scheme.
func splitSiteScheme(siteAddress string) (prefix, host string) {
	if s, h, ok := strings.Cut(siteAddress, "://"); ok {
		return s + "://", h
	}
	return "", siteAddress
}

// isApexDomain reports whether host is a registrable apex domain (example.com,
// example.co.uk) rather than a subdomain (blog.example.com). A leading
// "www." is treated as the apex it fronts, so www.example.com classifies as apex. It
// uses the Public Suffix List, so multi-label TLDs (.co.uk, .com.au) are handled
// correctly — naive label counting would misread example.co.uk as a subdomain.
func isApexDomain(host string) bool {
	h := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(host)), "www.")
	if h == "" || !strings.Contains(h, ".") {
		return false
	}
	etld1, err := publicsuffix.EffectiveTLDPlusOne(h)
	if err != nil {
		return false
	}
	return etld1 == h
}

// shellQuote single-quotes a value for safe embedding in a generated script.
func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
