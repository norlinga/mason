package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/remote"
)

// fakeNet simulates servers: which destinations (user@host, keyed by key path) accept
// connections, and what each ran. A bootstrap run as root authorizes the key it carries.
type fakeNet struct {
	authorized map[string]bool // "user@host|key" → reachable
	adminOK    bool
	ran        []string // "user@host: first line of command"
	trusted    []bool   // AcceptNewHostKey per connection
}

func (n *fakeNet) connect(o remote.SSHOptions) remote.CommandRunner {
	return scriptedRunner{fn: func(cmd string) (remote.CommandResult, error) {
		n.trusted = append(n.trusted, o.AcceptNewHostKey)
		dest := o.Destination()
		if o.User == "root" {
			if !n.adminOK {
				return remote.CommandResult{ExitCode: -1}, &remote.ConnectError{Destination: dest, Reason: "permission denied"}
			}
			n.ran = append(n.ran, dest+": "+firstComment(cmd))
			if strings.Contains(cmd, "mason server bootstrap") {
				n.authorized["mason@"+o.Host+"|"+keyFromScript(cmd)] = true
			}
			return remote.CommandResult{}, nil
		}
		if !n.authorized[dest+"|"+o.KeyPath] {
			return remote.CommandResult{ExitCode: -1}, &remote.ConnectError{Destination: dest, Reason: "permission denied"}
		}
		n.ran = append(n.ran, dest+": "+cmd)
		return remote.CommandResult{}, nil
	}}
}

func firstComment(script string) string {
	for _, l := range strings.Split(script, "\n") {
		if strings.HasPrefix(l, "# mason") {
			return l
		}
	}
	return ""
}

// keyFromScript recovers which local key a bootstrap authorizes (tests embed the path
// as the key comment).
func keyFromScript(script string) string {
	for _, l := range strings.Split(script, "\n") {
		if strings.HasPrefix(l, "PUBKEY=") {
			f := strings.Fields(strings.Trim(strings.TrimPrefix(l, "PUBKEY="), "'"))
			return f[len(f)-1]
		}
	}
	return ""
}

type fakeKeygen struct{ calls int }

func (k *fakeKeygen) Generate(_ context.Context, keyPath, comment string) error {
	k.calls++
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, []byte("private"), 0o600); err != nil {
		return err
	}
	// The comment is the key path so fakeNet can tell keys apart.
	return os.WriteFile(keyPath+".pub", []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl "+keyPath+"\n"), 0o644)
}

func setupFixture(t *testing.T) (string, *fakeNet, *fakeKeygen, Deps) {
	t.Helper()
	home := t.TempDir()
	net := &fakeNet{authorized: map[string]bool{}}
	kg := &fakeKeygen{}
	deps := Deps{
		Connect:  net.connect,
		Keygen:   kg,
		HomeDir:  func() (string, error) { return home, nil },
		Hostname: func() (string, error) { return "laptop", nil },
	}
	return home, net, kg, deps
}

func TestSetupAlreadyWorkingChangesNothingRemote(t *testing.T) {
	home, net, kg, deps := setupFixture(t)
	net.authorized["mason@example.com|"] = true // agent / ~/.ssh/config already works

	res, err := Run(context.Background(), deps, Params{Server: "example.com"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if kg.calls != 0 || res.Bootstrapped || res.KeyPath != "" {
		t.Fatalf("res = %+v, keygen calls %d; want no key and no bootstrap", res, kg.calls)
	}
	if got := readFile(t, filepath.Join(home, ".mason")); got != "server: example.com\n" {
		t.Fatalf("~/.mason = %q", got)
	}
	if !net.trusted[0] {
		t.Fatal("first contact must accept a new host key")
	}

	// Second run: idempotent, config untouched.
	res, err = Run(context.Background(), deps, Params{Server: "example.com"})
	if err != nil || res.ConfigChanged {
		t.Fatalf("second run: %+v, %v", res, err)
	}
}

func TestSetupFreshServerBootstrapsAsAdmin(t *testing.T) {
	home, net, kg, deps := setupFixture(t)
	net.adminOK = true
	var asked []string
	deps.Confirm = func(q string) (bool, error) { asked = append(asked, q); return true, nil }

	res, err := Run(context.Background(), deps, Params{Server: "example.com"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	keyPath := filepath.Join(home, ".ssh", "mason")
	if kg.calls != 1 || res.KeyPath != keyPath || !res.KeyGenerated || !res.Bootstrapped {
		t.Fatalf("res = %+v", res)
	}
	if len(asked) != 1 || !strings.Contains(asked[0], "Bootstrap example.com as root") {
		t.Fatalf("confirmation = %q", asked)
	}
	if !strings.HasPrefix(net.ran[0], "root@example.com: # mason server bootstrap") {
		t.Fatalf("ran = %q", net.ran)
	}
	if last := net.trusted[len(net.trusted)-1]; last {
		t.Fatal("verification after bootstrap must not accept new host keys")
	}
	want := "server: example.com\nkey: ~/.ssh/mason\n"
	if got := readFile(t, filepath.Join(home, ".mason")); got != want {
		t.Fatalf("~/.mason = %q, want %q", got, want)
	}
}

func TestSetupNoAdminAccessPrintsKeyAdd(t *testing.T) {
	home, _, _, deps := setupFixture(t)
	_, err := Run(context.Background(), deps, Params{Server: "example.com", Yes: true})
	if err == nil || !strings.Contains(err.Error(), "mason key add 'ssh-ed25519 ") {
		t.Fatalf("err = %v, want a 'mason key add' line", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".mason")); !os.IsNotExist(statErr) {
		t.Fatal("config written despite failure")
	}
}

func TestSetupDeclinedChangesNothing(t *testing.T) {
	home, net, _, deps := setupFixture(t)
	net.adminOK = true
	deps.Confirm = func(string) (bool, error) { return false, nil }

	_, err := Run(context.Background(), deps, Params{Server: "example.com"})
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("err = %v", err)
	}
	if len(net.ran) != 0 {
		t.Fatalf("remote ran %q after decline", net.ran)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".mason")); !os.IsNotExist(statErr) {
		t.Fatal("config written after decline")
	}
}

func TestSetupNonInteractiveWithoutYesRefuses(t *testing.T) {
	_, net, _, deps := setupFixture(t)
	net.adminOK = true
	_, err := Run(context.Background(), deps, Params{Server: "example.com"})
	if err == nil || !strings.Contains(err.Error(), "-y") {
		t.Fatalf("err = %v, want a hint to use -y", err)
	}
	if len(net.ran) != 0 {
		t.Fatalf("remote ran %q without confirmation", net.ran)
	}
}

func TestSetupShareStore(t *testing.T) {
	home, net, _, deps := setupFixture(t)
	net.adminOK = true
	net.authorized["mason@example.com|"] = true

	res, err := Run(context.Background(), deps, Params{
		Server: "example.com", ShareURL: "files.example.com", SharePath: "/var/www/share", Yes: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Bootstrapped || !res.ShareProvisioned {
		t.Fatalf("res = %+v", res)
	}
	if len(net.ran) != 2 || !strings.Contains(net.ran[1], "root@example.com: # mason share store — https://files.example.com") {
		t.Fatalf("ran = %q", net.ran)
	}
	want := "server: example.com\nshare:\n  url: https://files.example.com\n  path: /var/www/share\n"
	if got := readFile(t, filepath.Join(home, ".mason")); got != want {
		t.Fatalf("~/.mason =\n%s\nwant\n%s", got, want)
	}
}

func TestSetupPrintOnlyChangesNothing(t *testing.T) {
	home, net, kg, deps := setupFixture(t)
	res, err := Run(context.Background(), deps, Params{Server: "example.com", ShareURL: "files.example.com", PrintOnly: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Scripts) != 2 || !strings.Contains(res.Scripts[0], "mason server bootstrap") || !strings.Contains(res.Scripts[1], "share store") {
		t.Fatalf("scripts = %d", len(res.Scripts))
	}
	if kg.calls != 0 || len(net.trusted) != 0 {
		t.Fatalf("print mode generated keys (%d) or connected (%d)", kg.calls, len(net.trusted))
	}
	if _, err := os.Stat(filepath.Join(home, ".mason")); !os.IsNotExist(err) {
		t.Fatal("print mode wrote config")
	}
}

func TestSetupRejectsURLAsServer(t *testing.T) {
	_, _, _, deps := setupFixture(t)
	if _, err := Run(context.Background(), deps, Params{Server: "https://example.com"}); err == nil {
		t.Fatal("URL accepted as a server")
	}
}

func TestSetupRerunUsesConfiguredKey(t *testing.T) {
	home, net, kg, deps := setupFixture(t)
	key := filepath.Join(home, ".ssh", "id_ed25519")
	writeExec(t, filepath.Join(home, ".mason"), "server: example.com\nkey: "+key+"\n")
	net.authorized["mason@example.com|"+key] = true // works with that key; not in the agent

	res, err := Run(context.Background(), deps, Params{Server: "example.com"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if kg.calls != 0 || res.Bootstrapped || res.KeyPath != key || res.ConfigChanged {
		t.Fatalf("res = %+v, keygen %d; want the configured key reused and nothing changed", res, kg.calls)
	}
}
