package setup

import (
	"fmt"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// Defaults shared by the Caddy-touching scripts (bootstrap, share store, provision).
const (
	defaultCaddyUser  = "caddy"
	defaultCaddyfile  = "/etc/caddy/Caddyfile"
	defaultSnippetDir = "/etc/caddy/mason.d"

	// ManagedMarker is the first line of every snippet Mason writes. A snippet without it
	// was written by hand and is never replaced without --force.
	ManagedMarker = "# managed by mason"
)

// writeScriptPreamble opens a generated script: shebang, title, how to run it, strict
// mode, and the rollback hook the reload block calls when `caddy validate` fails.
func writeScriptPreamble(b *strings.Builder, title, host string) {
	b.WriteString("#!/bin/sh\n")
	fmt.Fprintf(b, "# %s\n", title)
	b.WriteString("# Idempotent. Review, then run as an admin, e.g.:\n")
	fmt.Fprintf(b, "#   ssh root@%s 'sh -s' < this-script.sh\n", host)
	b.WriteString("set -eu\n\n")
	b.WriteString("FORCE=\"${FORCE:-0}\"\n")
	b.WriteString("SNIPPET_FILE=''\n")
	b.WriteString("mason_rollback() {\n")
	b.WriteString("\t[ -n \"$SNIPPET_FILE\" ] || return 0\n")
	b.WriteString("\tif [ -f \"$SNIPPET_FILE.mason-prev\" ]; then mv \"$SNIPPET_FILE.mason-prev\" \"$SNIPPET_FILE\"; else rm -f \"$SNIPPET_FILE\"; fi\n")
	b.WriteString("}\n\n")
}

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
// the main Caddyfile. It never rewrites anything else in that file.
func writeCaddyImportBlock(b *strings.Builder, snippetDir string) {
	glob := strings.TrimRight(snippetDir, "/") + "/*.caddy"
	fmt.Fprintf(b, "grep -qF %s \"$CADDYFILE\" 2>/dev/null || printf '\\nimport %%s/*.caddy\\n' \"$SNIPPET_DIR\" >> \"$CADDYFILE\"\n",
		shellQuote("import "+glob))
}

// writeSnippetInstall installs content (which must start with ManagedMarker) at
// snippetFile. A hand-written snippet (no marker) is left alone and the script exits 3
// after printing the diff, unless FORCE=1, in which case it is backed up first. The
// previous version is kept as <file>.mason-prev until the reload block validates, so a
// failed `caddy validate` restores it.
func writeSnippetInstall(b *strings.Builder, snippetFile, content string) {
	fmt.Fprintf(b, "SNIPPET_FILE=%s\n", shellQuote(snippetFile))
	b.WriteString("install -d -m 755 \"$SNIPPET_DIR\"\n")
	b.WriteString("cat > \"$SNIPPET_FILE.mason-new\" <<'CADDY'\n")
	b.WriteString(content)
	b.WriteString("CADDY\n")
	fmt.Fprintf(b, "if [ -f \"$SNIPPET_FILE\" ] && ! grep -qxF %s \"$SNIPPET_FILE\"; then\n", shellQuote(ManagedMarker))
	b.WriteString("\tif [ \"$FORCE\" != 1 ]; then\n")
	b.WriteString("\t\techo \"$SNIPPET_FILE was not written by mason; leaving it alone. Changes mason would make:\" >&2\n")
	b.WriteString("\t\tdiff -u \"$SNIPPET_FILE\" \"$SNIPPET_FILE.mason-new\" >&2 || true\n")
	b.WriteString("\t\trm -f \"$SNIPPET_FILE.mason-new\"\n")
	b.WriteString("\t\techo \"Re-run with --force to replace it (the original is backed up).\" >&2\n")
	b.WriteString("\t\texit 3\n")
	b.WriteString("\tfi\n")
	b.WriteString("\tcp -p \"$SNIPPET_FILE\" \"$SNIPPET_FILE.bak-$(date +%Y%m%d-%H%M%S)\"\n")
	b.WriteString("fi\n")
	b.WriteString("if [ -f \"$SNIPPET_FILE\" ]; then cp -p \"$SNIPPET_FILE\" \"$SNIPPET_FILE.mason-prev\"; fi\n")
	b.WriteString("mv \"$SNIPPET_FILE.mason-new\" \"$SNIPPET_FILE\"\n")
	b.WriteString("chmod 644 \"$SNIPPET_FILE\"\n")
}

// writeCaddyReloadBlock emits the validate-then-apply step as a numbered section. When
// reload is true the script validates — restoring the previous snippet and exiting if
// validation fails — then reloads, or restarts when CADDY_GROUP_CHANGED. When false it
// only prints the manual steps so the operator applies the change after reviewing it.
func writeCaddyReloadBlock(b *strings.Builder, section int, reload bool) {
	if reload {
		fmt.Fprintf(b, "# %d. Validate (restoring the previous snippet on failure), then restart Caddy if\n", section)
		b.WriteString("#    its group changed (a reload does not re-exec the process, so new group\n")
		b.WriteString("#    membership is only picked up on restart); otherwise a reload is enough.\n")
		b.WriteString("if ! caddy validate --config \"$CADDYFILE\"; then\n")
		b.WriteString("\tmason_rollback\n")
		b.WriteString("\techo \"caddy validate failed; the previous configuration was restored\" >&2\n")
		b.WriteString("\texit 1\n")
		b.WriteString("fi\n")
		b.WriteString("[ -z \"$SNIPPET_FILE\" ] || rm -f \"$SNIPPET_FILE.mason-prev\"\n")
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
