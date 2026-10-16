package setup

import (
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/config"
)

const testPub = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl mason@laptop"

func TestBootstrapScript(t *testing.T) {
	script, err := BootstrapScript(BootstrapParams{Host: "example.com", PublicKey: testPub, Reload: true})
	if err != nil {
		t.Fatalf("BootstrapScript: %v", err)
	}
	for _, want := range []string{
		"set -eu",
		"MASON_USER='mason'",
		"useradd --system --gid \"$MASON_GROUP\"",
		"PUBKEY='" + testPub + "'",
		"RESTRICT='" + AuthorizedKeyOptions + "'",
		"grep -qF \"$PUBKEY\" \"$AUTHORIZED_KEYS\" ||",
		"usermod -aG \"$MASON_GROUP\" \"$CADDY_USER\"",
		"grep -qF 'import /etc/caddy/mason.d/*.caddy' \"$CADDYFILE\" 2>/dev/null ||",
		"if ! caddy validate --config \"$CADDYFILE\"; then",
		"#   ssh root@example.com 'sh -s' < this-script.sh",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("bootstrap missing %q\n---\n%s", want, script)
		}
	}
	// The bootstrap is per server: no vhosts, no share or site trees.
	for _, banned := range []string{"SNIPPET_FILE='/", "SHARE_ROOT", "SITE_ROOT", "file_server"} {
		if strings.Contains(script, banned) {
			t.Fatalf("bootstrap must not contain %q\n---\n%s", banned, script)
		}
	}
}

func TestBootstrapScriptRequiresSingleLineKey(t *testing.T) {
	for _, pub := range []string{"", "  ", testPub + "\nssh-ed25519 AAAA other"} {
		if _, err := BootstrapScript(BootstrapParams{PublicKey: pub}); err == nil {
			t.Fatalf("BootstrapScript(%q) succeeded, want error", pub)
		}
	}
}

func TestBootstrapScriptQuotesHostileKeyComment(t *testing.T) {
	pub := "ssh-ed25519 AAAA it's'; rm -rf / #"
	script, err := BootstrapScript(BootstrapParams{PublicKey: pub})
	if err != nil {
		t.Fatalf("BootstrapScript: %v", err)
	}
	if !strings.Contains(script, `PUBKEY='ssh-ed25519 AAAA it'\''s'\''; rm -rf / #'`) {
		t.Fatalf("key comment not single-quoted safely\n%s", script)
	}
}

func TestShareStoreScript(t *testing.T) {
	share, err := config.NewShare("https://files.example.com", "/var/www/share", &config.Server{User: "mason", Host: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	script, err := ShareStoreScript(ShareStoreParams{Share: share, Reload: true})
	if err != nil {
		t.Fatalf("ShareStoreScript: %v", err)
	}
	for _, want := range []string{
		"SHARE_ROOT='/var/www/share'",
		"\"$SHARE_ROOT/objects\" \"$SHARE_ROOT/s\"",
		"install -d -m 700 -o \"$MASON_USER\" -g \"$MASON_GROUP\" \"$SHARE_ROOT/.mason\"",
		"SNIPPET_FILE='/etc/caddy/mason.d/files.example.com.caddy'",
		ManagedMarker + "\n",
		"https://files.example.com {\n\troot * /var/www/share\n",
		"\t@mason_private path /objects/* /.mason/*\n\trespond @mason_private 404\n",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("share store script missing %q\n---\n%s", want, script)
		}
	}
	// Share paths may be dotfiles; the general dotfile rule belongs to sites only.
	if strings.Contains(script, "@mason_hidden") {
		t.Fatalf("share vhost must not hide dotfiles\n%s", script)
	}
	if strings.Contains(script, "useradd") || strings.Contains(script, "authorized_keys") {
		t.Fatalf("share store script must not touch users or keys\n%s", script)
	}
}
