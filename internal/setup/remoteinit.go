package setup

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/norlinga/mason/internal/config"
)

// Defaults for remote-init server bootstrap.
const (
	defaultCaddyUser   = "caddy"
	defaultCaddyfile   = "/etc/caddy/Caddyfile"
	defaultSnippetDir  = "/etc/caddy/mason.d"
	authorizedKeyOpts  = "no-port-forwarding,no-agent-forwarding,no-X11-forwarding,no-user-rc"
	defaultRemoteShell = "/bin/bash"
)

// RemoteInitParams configures the generated bootstrap script. The output is a pure,
// deterministic function of these inputs so the same target reproduces the same
// server state on every run.
type RemoteInitParams struct {
	Target     config.Target
	TargetName string
	// PublicKey is the contents of the deploy key's .pub file. When empty, the key
	// install step is replaced with a clearly marked TODO comment.
	PublicKey  string
	CaddyUser  string
	Group      string
	Caddyfile  string
	SnippetDir string
	// Reload appends `caddy validate` + `systemctl reload caddy` to the script.
	Reload bool
}

// RemoteInitScript renders an idempotent POSIX-sh bootstrap script for a target:
// service user + group, hardened deploy key, share tree (objects/ s/ .mason/),
// deploy tree(s), Caddy read access, and a Mason-owned Caddy snippet imported once.
func RemoteInitScript(p RemoteInitParams) (string, error) {
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
	shareRoot := strings.TrimSpace(p.Target.Root)
	if shareRoot == "" {
		return "", fmt.Errorf("target root is required")
	}
	siteAddress, err := siteAddressFromBaseURL(p.Target.BaseURL)
	if err != nil {
		return "", err
	}

	snippetGlob := path.Join(snippetDir, "*.caddy")
	snippetFile := path.Join(snippetDir, name+".caddy")
	deployPath := strings.TrimSpace(p.Target.Deploy.RemotePath)

	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	fmt.Fprintf(&b, "# mason remote-init — target %q (%s)\n", name, siteAddress)
	b.WriteString("# Idempotent server bootstrap. Review, then run as root, e.g.:\n")
	fmt.Fprintf(&b, "#   ssh %s@%s 'sh -s' < this-script.sh\n", "root", hostOnly(siteAddress))
	b.WriteString("set -eu\n\n")

	fmt.Fprintf(&b, "MASON_USER=%s\n", shellQuote(user))
	fmt.Fprintf(&b, "MASON_GROUP=%s\n", shellQuote(group))
	fmt.Fprintf(&b, "CADDY_USER=%s\n", shellQuote(caddyUser))
	fmt.Fprintf(&b, "SHARE_ROOT=%s\n", shellQuote(shareRoot))
	fmt.Fprintf(&b, "SNIPPET_DIR=%s\n", shellQuote(snippetDir))
	fmt.Fprintf(&b, "CADDYFILE=%s\n\n", shellQuote(caddyfile))

	b.WriteString("# 1. Dedicated unprivileged service user + group\n")
	b.WriteString("getent group \"$MASON_GROUP\" >/dev/null 2>&1 || groupadd --system \"$MASON_GROUP\"\n")
	b.WriteString("id \"$MASON_USER\" >/dev/null 2>&1 || useradd --system --gid \"$MASON_GROUP\" --create-home --shell " + defaultRemoteShell + " \"$MASON_USER\"\n")
	b.WriteString("MASON_HOME=\"$(getent passwd \"$MASON_USER\" | cut -d: -f6)\"\n\n")

	b.WriteString("# 2. Hardened deploy key\n")
	b.WriteString("install -d -m 700 -o \"$MASON_USER\" -g \"$MASON_GROUP\" \"$MASON_HOME/.ssh\"\n")
	b.WriteString("AUTHORIZED_KEYS=\"$MASON_HOME/.ssh/authorized_keys\"\n")
	b.WriteString("[ -f \"$AUTHORIZED_KEYS\" ] || : > \"$AUTHORIZED_KEYS\"\n")
	b.WriteString("chown \"$MASON_USER\":\"$MASON_GROUP\" \"$AUTHORIZED_KEYS\"\n")
	b.WriteString("chmod 600 \"$AUTHORIZED_KEYS\"\n")
	if pub := strings.TrimSpace(p.PublicKey); pub != "" {
		fmt.Fprintf(&b, "PUBKEY=%s\n", shellQuote(pub))
		fmt.Fprintf(&b, "RESTRICT=%s\n", shellQuote(authorizedKeyOpts))
		b.WriteString("grep -qF \"$PUBKEY\" \"$AUTHORIZED_KEYS\" || printf '%s %s\\n' \"$RESTRICT\" \"$PUBKEY\" >> \"$AUTHORIZED_KEYS\"\n\n")
	} else {
		b.WriteString("# TODO: no public key supplied — add your deploy key to \"$AUTHORIZED_KEYS\"\n")
		b.WriteString("#   (run 'mason setup' to generate one, then re-run 'mason remote-init')\n\n")
	}

	b.WriteString("# 3. Share tree (mason owns; objects/s group-readable for Caddy; manifest private)\n")
	b.WriteString("install -d -m 2750 -o \"$MASON_USER\" -g \"$MASON_GROUP\" \"$SHARE_ROOT\" \"$SHARE_ROOT/objects\" \"$SHARE_ROOT/s\"\n")
	b.WriteString("install -d -m 700 -o \"$MASON_USER\" -g \"$MASON_GROUP\" \"$SHARE_ROOT/.mason\"\n\n")

	if deployPath != "" && deployPath != shareRoot {
		b.WriteString("# 4. Deploy tree (mason can rsync the static site too)\n")
		fmt.Fprintf(&b, "install -d -m 2750 -o \"$MASON_USER\" -g \"$MASON_GROUP\" %s\n\n", shellQuote(deployPath))
	}

	b.WriteString("# 5. Let Caddy read the served trees (least-privilege via group)\n")
	b.WriteString("CADDY_GROUP_CHANGED=0\n")
	b.WriteString("if id \"$CADDY_USER\" >/dev/null 2>&1; then\n")
	b.WriteString("\tif ! id -nG \"$CADDY_USER\" | grep -qw \"$MASON_GROUP\"; then\n")
	b.WriteString("\t\tusermod -aG \"$MASON_GROUP\" \"$CADDY_USER\"\n")
	b.WriteString("\t\tCADDY_GROUP_CHANGED=1\n")
	b.WriteString("\tfi\n")
	b.WriteString("fi\n\n")

	b.WriteString("# 6. Caddy: Mason-owned snippet + single import (never rewrites your Caddyfile)\n")
	b.WriteString("install -d -m 755 \"$SNIPPET_DIR\"\n")
	fmt.Fprintf(&b, "cat > %s <<'CADDY'\n", shellQuote(snippetFile))
	b.WriteString(caddySnippet(siteAddress, shareRoot))
	b.WriteString("CADDY\n")
	fmt.Fprintf(&b, "grep -qF %s \"$CADDYFILE\" 2>/dev/null || printf '\\nimport %%s/*.caddy\\n' \"$SNIPPET_DIR\" >> \"$CADDYFILE\"\n\n",
		shellQuote("import "+snippetGlob))

	if p.Reload {
		b.WriteString("# 7. Validate, then restart Caddy if its group changed (a reload does not\n")
		b.WriteString("#    re-exec the process, so new group membership is only picked up on restart);\n")
		b.WriteString("#    otherwise a reload is enough.\n")
		b.WriteString("caddy validate --config \"$CADDYFILE\"\n")
		b.WriteString("if [ \"$CADDY_GROUP_CHANGED\" -eq 1 ]; then\n")
		b.WriteString("\tsystemctl restart caddy\n")
		b.WriteString("else\n")
		b.WriteString("\tsystemctl reload caddy\n")
		b.WriteString("fi\n")
	} else {
		b.WriteString("# 7. Apply the change yourself once you've reviewed it. Use restart (not reload)\n")
		b.WriteString("#    on first run so Caddy picks up its new group membership:\n")
		b.WriteString("#   caddy validate --config \"$CADDYFILE\" && systemctl restart caddy\n")
	}

	return b.String(), nil
}

// caddySnippet renders the share vhost: serve the root, but hard-404 the private
// prefixes so blobs are reachable only via their symlinks and the manifest is never
// public.
func caddySnippet(siteAddress, root string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s {\n", siteAddress)
	fmt.Fprintf(&b, "\troot * %s\n", root)
	b.WriteString("\tfile_server\n")
	b.WriteString("\t@mason_private path /objects/* /.mason/*\n")
	b.WriteString("\trespond @mason_private 404\n")
	b.WriteString("}\n")
	return b.String()
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

// hostOnly strips a scheme from a site address for display in the run hint.
func hostOnly(siteAddress string) string {
	if _, host, ok := strings.Cut(siteAddress, "://"); ok {
		return host
	}
	return siteAddress
}

// shellQuote single-quotes a value for safe embedding in the generated script.
func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
