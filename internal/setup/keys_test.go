package setup

import (
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/remote"
)

// fileRunner emulates the mason user's authorized_keys for the commands keys.go sends.
type fileRunner struct {
	content string
	calls   int
}

func (f *fileRunner) Run(_ context.Context, cmd string) (remote.CommandResult, error) {
	f.calls++
	switch {
	case strings.HasPrefix(cmd, "cat "):
		return remote.CommandResult{Stdout: f.content}, nil
	case strings.Contains(cmd, ">> $HOME/.ssh/authorized_keys"):
		args := shellWords(cmd[strings.Index(cmd, "printf"):strings.Index(cmd, ">>")])
		f.content += fmt.Sprintf("%s %s\n", args[2], args[3])
		return remote.CommandResult{}, nil
	case strings.Contains(cmd, "base64 -d"):
		enc := shellWords(cmd[strings.Index(cmd, "printf"):strings.Index(cmd, "|")])[2]
		data, _ := base64.StdEncoding.DecodeString(enc)
		f.content = string(data)
		return remote.CommandResult{}, nil
	}
	return remote.CommandResult{}, fmt.Errorf("unexpected command %q", cmd)
}

// shellWords splits POSIX words, honoring single quotes and the '\” idiom.
func shellWords(s string) []string {
	var out []string
	var cur strings.Builder
	inWord, inQuote := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			inQuote, inWord = !inQuote, true
		case c == '\\' && !inQuote && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inWord = true
		case (c == ' ' || c == '\t') && !inQuote:
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out
}

func genKey(t *testing.T, comment string) string {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not installed")
	}
	path := t.TempDir() + "/k"
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", comment, "-f", path).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v %s", err, out)
	}
	pub := strings.TrimSpace(readFile(t, path+".pub"))
	// Fingerprints must match OpenSSH's own.
	out, err := exec.Command("ssh-keygen", "-lf", path+".pub").Output()
	if err != nil {
		t.Fatal(err)
	}
	k, err := ParsePublicKey(pub)
	if err != nil {
		t.Fatalf("ParsePublicKey: %v", err)
	}
	if want := strings.Fields(string(out))[1]; k.Fingerprint != want {
		t.Fatalf("fingerprint = %s, ssh-keygen says %s", k.Fingerprint, want)
	}
	return pub
}

func TestKeyAddListRemove(t *testing.T) {
	laptop := genKey(t, "mason@laptop")
	desktop := genKey(t, "mason@desktop")
	f := &fileRunner{content: "# managed by hand\n" + AuthorizedKeyOptions + " " + laptop + "\n"}
	ctx := context.Background()

	k, added, err := KeyAdd(ctx, f, desktop)
	if err != nil || !added || k.Comment != "mason@desktop" {
		t.Fatalf("KeyAdd = %+v, %v, %v", k, added, err)
	}
	if _, added, _ := KeyAdd(ctx, f, desktop); added {
		t.Fatal("KeyAdd added a duplicate")
	}
	if !strings.Contains(f.content, AuthorizedKeyOptions+" "+desktop) {
		t.Fatalf("restrict options missing:\n%s", f.content)
	}

	keys, err := KeyList(ctx, f)
	if err != nil || len(keys) != 2 || keys[0].Comment != "mason@laptop" || keys[0].Options != AuthorizedKeyOptions {
		t.Fatalf("KeyList = %+v, %v", keys, err)
	}

	// The key this machine uses is protected.
	if _, err := KeyRemove(ctx, f, "mason@laptop", laptop, false); err == nil || !strings.Contains(err.Error(), "this machine") {
		t.Fatalf("removing own key: %v", err)
	}
	victim, err := KeyRemove(ctx, f, keys[1].Fingerprint, laptop, false)
	if err != nil || victim.Comment != "mason@desktop" {
		t.Fatalf("KeyRemove = %+v, %v", victim, err)
	}
	if strings.Contains(f.content, "mason@desktop") || !strings.HasPrefix(f.content, "# managed by hand\n") {
		t.Fatalf("after remove:\n%s", f.content)
	}
	// The last key is protected too.
	if _, err := KeyRemove(ctx, f, "mason@laptop", "", false); err == nil || !strings.Contains(err.Error(), "last") {
		t.Fatalf("removing last key: %v", err)
	}
}

func TestKeyAddRejectsMalformedWithoutRemoteCalls(t *testing.T) {
	f := &fileRunner{}
	for _, bad := range []string{"", "ssh-ed25519", "ssh-ed25519 notbase64!", "ssh-rsa " + base64.StdEncoding.EncodeToString([]byte("\x00\x00\x00\x0bssh-ed25519xx")), "hello world"} {
		if _, _, err := KeyAdd(context.Background(), f, bad); err == nil {
			t.Fatalf("KeyAdd(%q) succeeded", bad)
		}
	}
	if f.calls != 0 {
		t.Fatalf("malformed keys reached the server (%d calls)", f.calls)
	}
}

func TestKeyRemoveAmbiguous(t *testing.T) {
	a, b := genKey(t, "laptop"), genKey(t, "laptop")
	f := &fileRunner{content: a + "\n" + b + "\n"}
	if _, err := KeyRemove(context.Background(), f, "laptop", "", false); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("err = %v", err)
	}
}
