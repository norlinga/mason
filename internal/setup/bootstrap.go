package setup

import (
	"fmt"
	"strings"

	"github.com/norlinga/mason/internal/config"
)

// Defaults specific to the server bootstrap.
const (
	// AuthorizedKeyOptions restrict every key Mason installs to plain command execution.
	AuthorizedKeyOptions = "no-port-forwarding,no-agent-forwarding,no-X11-forwarding,no-user-rc"
	defaultRemoteShell   = "/bin/bash"
)

// BootstrapParams configures the once-per-server bootstrap script.
type BootstrapParams struct {
	// Host is shown in the script's run hint.
	Host string
	// User is the service account to create (default "mason").
	User string
	// PublicKey is this machine's public key line; it is required.
	PublicKey  string
	CaddyUser  string
	Group      string
	Caddyfile  string
	SnippetDir string
	// Reload appends `caddy validate` + reload/restart to the script.
	Reload bool
}

// BootstrapScript renders the idempotent once-per-server script: the mason service user
// and group, this machine's restricted key, Caddy group access, and the single
// `import mason.d/*.caddy` line. It writes no vhosts and no share or site trees — those
// come from ShareStoreScript and ProvisionScript.
func BootstrapScript(p BootstrapParams) (string, error) {
	pub := strings.TrimSpace(p.PublicKey)
	if pub == "" {
		return "", fmt.Errorf("a public key is required to bootstrap the server")
	}
	if strings.ContainsAny(pub, "\n\r") {
		return "", fmt.Errorf("public key must be a single line")
	}
	user := orDefault(p.User, config.DefaultUser)
	group := orDefault(p.Group, user)
	snippetDir := orDefault(p.SnippetDir, defaultSnippetDir)

	var b strings.Builder
	writeScriptPreamble(&b, "mason server bootstrap — "+orDefault(p.Host, "server"), orDefault(p.Host, "<server>"))
	fmt.Fprintf(&b, "MASON_USER=%s\n", shellQuote(user))
	fmt.Fprintf(&b, "MASON_GROUP=%s\n", shellQuote(group))
	fmt.Fprintf(&b, "CADDY_USER=%s\n", shellQuote(orDefault(p.CaddyUser, defaultCaddyUser)))
	fmt.Fprintf(&b, "SNIPPET_DIR=%s\n", shellQuote(snippetDir))
	fmt.Fprintf(&b, "CADDYFILE=%s\n\n", shellQuote(orDefault(p.Caddyfile, defaultCaddyfile)))

	b.WriteString("# 1. Dedicated unprivileged service user + group\n")
	b.WriteString("getent group \"$MASON_GROUP\" >/dev/null 2>&1 || groupadd --system \"$MASON_GROUP\"\n")
	b.WriteString("id \"$MASON_USER\" >/dev/null 2>&1 || useradd --system --gid \"$MASON_GROUP\" --create-home --shell " + defaultRemoteShell + " \"$MASON_USER\"\n")
	b.WriteString("MASON_HOME=\"$(getent passwd \"$MASON_USER\" | cut -d: -f6)\"\n\n")

	b.WriteString("# 2. This machine's key, restricted to command execution\n")
	writeAuthorizeKeyBlock(&b, pub)
	b.WriteString("\n")

	b.WriteString("# 3. Let Caddy read mason-owned trees (least-privilege via group)\n")
	writeCaddyGroupAccessBlock(&b)
	b.WriteString("\n")

	b.WriteString("# 4. Caddy: import Mason's snippet directory once (never rewrites your Caddyfile)\n")
	b.WriteString("install -d -m 755 \"$SNIPPET_DIR\"\n")
	writeCaddyImportBlock(&b, snippetDir)
	b.WriteString("\n")

	writeCaddyReloadBlock(&b, 5, p.Reload)
	return b.String(), nil
}

// writeAuthorizeKeyBlock appends pub to the mason user's authorized_keys (with the
// restrict options) unless that key is already present. Expects MASON_USER, MASON_GROUP,
// and MASON_HOME to be set.
func writeAuthorizeKeyBlock(b *strings.Builder, pub string) {
	b.WriteString("install -d -m 700 -o \"$MASON_USER\" -g \"$MASON_GROUP\" \"$MASON_HOME/.ssh\"\n")
	b.WriteString("AUTHORIZED_KEYS=\"$MASON_HOME/.ssh/authorized_keys\"\n")
	b.WriteString("[ -f \"$AUTHORIZED_KEYS\" ] || : > \"$AUTHORIZED_KEYS\"\n")
	b.WriteString("chown \"$MASON_USER\":\"$MASON_GROUP\" \"$AUTHORIZED_KEYS\"\n")
	b.WriteString("chmod 600 \"$AUTHORIZED_KEYS\"\n")
	fmt.Fprintf(b, "PUBKEY=%s\n", shellQuote(pub))
	fmt.Fprintf(b, "RESTRICT=%s\n", shellQuote(AuthorizedKeyOptions))
	b.WriteString("grep -qF \"$PUBKEY\" \"$AUTHORIZED_KEYS\" || printf '%s %s\\n' \"$RESTRICT\" \"$PUBKEY\" >> \"$AUTHORIZED_KEYS\"\n")
}
