package setup

import (
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/config"
)

func fullTarget() config.Target {
	return config.Target{
		Host:    "share.example.com",
		User:    "mason",
		Root:    "/srv/share",
		BaseURL: "https://share.example.com",
		Deploy:  config.DeployConfig{RemotePath: "/srv/site"},
	}
}

func fullParams() RemoteInitParams {
	return RemoteInitParams{
		Target:     fullTarget(),
		TargetName: "home",
		PublicKey:  "ssh-ed25519 AAAATESTKEY mason@laptop",
		Reload:     true,
	}
}

func TestRemoteInitScriptInvariants(t *testing.T) {
	script, err := RemoteInitScript(fullParams())
	if err != nil {
		t.Fatalf("RemoteInitScript: %v", err)
	}

	wantContains := []string{
		"set -eu",
		// idempotent user/group creation
		"getent group \"$MASON_GROUP\" >/dev/null 2>&1 || groupadd",
		"id \"$MASON_USER\" >/dev/null 2>&1 || useradd",
		// hardened key install, idempotent append
		"ssh-ed25519 AAAATESTKEY mason@laptop",
		"no-port-forwarding,no-agent-forwarding,no-X11-forwarding,no-user-rc",
		"grep -qF \"$PUBKEY\" \"$AUTHORIZED_KEYS\" ||",
		// share tree perms: objects/s group-readable, manifest private
		"install -d -m 2750 -o \"$MASON_USER\" -g \"$MASON_GROUP\" \"$SHARE_ROOT\" \"$SHARE_ROOT/objects\" \"$SHARE_ROOT/s\"",
		"install -d -m 700 -o \"$MASON_USER\" -g \"$MASON_GROUP\" \"$SHARE_ROOT/.mason\"",
		// deploy tree prepared so mason can rsync the static site
		"install -d -m 2750 -o \"$MASON_USER\" -g \"$MASON_GROUP\" '/srv/site'",
		// caddy reads via group
		"usermod -aG \"$MASON_GROUP\" \"$CADDY_USER\"",
		// caddy snippet with private-prefix 404
		"share.example.com {",
		"@mason_private path /objects/* /.mason/*",
		"respond @mason_private 404",
		// single import line, idempotent
		"grep -qF 'import /etc/caddy/mason.d/*.caddy' \"$CADDYFILE\" 2>/dev/null ||",
		// reload guarded by validate
		"caddy validate --config \"$CADDYFILE\"",
		"systemctl reload caddy",
	}
	for _, want := range wantContains {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q\n---\n%s", want, script)
		}
	}
}

func TestRemoteInitScriptDeterministic(t *testing.T) {
	a, err := RemoteInitScript(fullParams())
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := RemoteInitScript(fullParams())
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a != b {
		t.Fatal("RemoteInitScript is not deterministic for identical input")
	}
}

func TestRemoteInitScriptNoKey(t *testing.T) {
	p := fullParams()
	p.PublicKey = ""
	script, err := RemoteInitScript(p)
	if err != nil {
		t.Fatalf("RemoteInitScript: %v", err)
	}
	if strings.Contains(script, "AAAATESTKEY") {
		t.Fatal("script embedded a key when none was provided")
	}
	if !strings.Contains(script, "TODO: no public key supplied") {
		t.Fatalf("missing no-key TODO\n%s", script)
	}
}

func TestRemoteInitScriptNoReload(t *testing.T) {
	p := fullParams()
	p.Reload = false
	script, err := RemoteInitScript(p)
	if err != nil {
		t.Fatalf("RemoteInitScript: %v", err)
	}
	if strings.Contains(script, "systemctl reload caddy\n") && !strings.Contains(script, "#   caddy validate") {
		t.Fatal("no-reload still issues an unguarded reload")
	}
	if !strings.Contains(script, "Reload Caddy yourself") {
		t.Fatalf("missing manual-reload hint\n%s", script)
	}
}

func TestRemoteInitScriptOmitsDeployWhenSameAsRoot(t *testing.T) {
	p := fullParams()
	p.Target.Deploy.RemotePath = "/srv/share" // same as share root
	script, err := RemoteInitScript(p)
	if err != nil {
		t.Fatalf("RemoteInitScript: %v", err)
	}
	if strings.Contains(script, "# 4. Deploy tree") {
		t.Fatal("deploy section emitted when remote_path equals share root")
	}
}

func TestRemoteInitScriptOmitsDeployWhenUnset(t *testing.T) {
	p := fullParams()
	p.Target.Deploy.RemotePath = ""
	script, err := RemoteInitScript(p)
	if err != nil {
		t.Fatalf("RemoteInitScript: %v", err)
	}
	if strings.Contains(script, "# 4. Deploy tree") {
		t.Fatal("deploy section emitted when remote_path unset")
	}
}

func TestRemoteInitScriptRequiresRootAndBaseURL(t *testing.T) {
	p := fullParams()
	p.Target.Root = ""
	if _, err := RemoteInitScript(p); err == nil {
		t.Fatal("missing root err = nil, want error")
	}
	p = fullParams()
	p.Target.BaseURL = ""
	if _, err := RemoteInitScript(p); err == nil {
		t.Fatal("missing base_url err = nil, want error")
	}
}

func TestSiteAddressFromBaseURL(t *testing.T) {
	tests := map[string]string{
		"https://share.example.com":          "https://share.example.com",
		"https://share.example.com/":         "https://share.example.com",
		"https://share.example.com/sub/path": "https://share.example.com",
		"http://h:8080":                      "http://h:8080",
	}
	for in, want := range tests {
		got, err := siteAddressFromBaseURL(in)
		if err != nil {
			t.Fatalf("siteAddressFromBaseURL(%q): %v", in, err)
		}
		if got != want {
			t.Fatalf("siteAddressFromBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRemoteInitGroupDefault verifies the service group defaults to the user.
func TestRemoteInitGroupDefault(t *testing.T) {
	p := fullParams()
	p.Target.User = "publisher"
	script, err := RemoteInitScript(p)
	if err != nil {
		t.Fatalf("RemoteInitScript: %v", err)
	}
	if !strings.Contains(script, "MASON_USER='publisher'") || !strings.Contains(script, "MASON_GROUP='publisher'") {
		t.Fatalf("user/group not defaulted together\n%s", script)
	}
}
