package setup

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/norlinga/mason/internal/remote"
)

const authorizedKeysPath = "$HOME/.ssh/authorized_keys"

// AuthorizedKey is one key line in authorized_keys.
type AuthorizedKey struct {
	Options     string
	Type        string
	Blob        string
	Comment     string
	Fingerprint string
}

// Line renders the key as type, blob, and comment (no options).
func (k AuthorizedKey) Line() string {
	return strings.TrimSpace(k.Type + " " + k.Blob + " " + k.Comment)
}

var keyTypes = map[string]bool{
	"ssh-ed25519": true, "ssh-rsa": true, "ssh-dss": true,
	"ecdsa-sha2-nistp256": true, "ecdsa-sha2-nistp384": true, "ecdsa-sha2-nistp521": true,
	"sk-ssh-ed25519@openssh.com": true, "sk-ecdsa-sha2-nistp256@openssh.com": true,
}

// ParsePublicKey parses a public key line ("type base64 [comment]"), optionally preceded
// by authorized_keys options, and validates the blob.
func ParsePublicKey(line string) (AuthorizedKey, error) {
	fields := splitKeyFields(strings.TrimSpace(line))
	for i, f := range fields {
		if !keyTypes[f] {
			continue
		}
		if i+1 >= len(fields) {
			return AuthorizedKey{}, fmt.Errorf("public key has no key data after %s", f)
		}
		k := AuthorizedKey{
			Options: strings.Join(fields[:i], " "),
			Type:    f,
			Blob:    fields[i+1],
			Comment: strings.Join(fields[i+2:], " "),
		}
		raw, err := base64.StdEncoding.DecodeString(k.Blob)
		if err != nil {
			return AuthorizedKey{}, fmt.Errorf("public key data is not valid base64")
		}
		if embedded, ok := sshString(raw); !ok || embedded != k.Type {
			return AuthorizedKey{}, fmt.Errorf("public key data does not match its type %s", k.Type)
		}
		sum := sha256.Sum256(raw)
		k.Fingerprint = "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
		return k, nil
	}
	return AuthorizedKey{}, fmt.Errorf("not an SSH public key (expected e.g. 'ssh-ed25519 AAAA… comment')")
}

// KeyList returns the keys in the mason user's authorized_keys.
func KeyList(ctx context.Context, runner remote.CommandRunner) ([]AuthorizedKey, error) {
	lines, err := readAuthorizedKeys(ctx, runner)
	if err != nil {
		return nil, err
	}
	var keys []AuthorizedKey
	for _, l := range lines {
		if k, err := ParsePublicKey(l); err == nil {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// KeyAdd authorizes pub for the mason user. It reports false when the key was already
// present.
func KeyAdd(ctx context.Context, runner remote.CommandRunner, pub string) (AuthorizedKey, bool, error) {
	k, err := ParsePublicKey(pub)
	if err != nil {
		return AuthorizedKey{}, false, err
	}
	existing, err := KeyList(ctx, runner)
	if err != nil {
		return k, false, err
	}
	for _, e := range existing {
		if e.Blob == k.Blob {
			return e, false, nil
		}
	}
	cmd := fmt.Sprintf("umask 077 && mkdir -p \"$HOME/.ssh\" && printf '%%s %%s\\n' %s %s >> %s",
		shellQuote(AuthorizedKeyOptions), shellQuote(k.Line()), authorizedKeysPath)
	if _, err := runner.Run(ctx, cmd); err != nil {
		return k, false, fmt.Errorf("authorize key: %w", err)
	}
	return k, true, nil
}

// KeyRemove deletes the one key matching match (comment or fingerprint). current is the
// public key line this machine uses ("" if unknown); removing it, or the last key,
// requires force.
func KeyRemove(ctx context.Context, runner remote.CommandRunner, match, current string, force bool) (AuthorizedKey, error) {
	lines, err := readAuthorizedKeys(ctx, runner)
	if err != nil {
		return AuthorizedKey{}, err
	}
	var hits []int
	var keys int
	for i, l := range lines {
		k, err := ParsePublicKey(l)
		if err != nil {
			continue
		}
		keys++
		if k.Comment == match || k.Fingerprint == match || k.Fingerprint == "SHA256:"+match {
			hits = append(hits, i)
		}
	}
	switch {
	case len(hits) == 0:
		return AuthorizedKey{}, fmt.Errorf("no authorized key matches %q (see 'mason key ls')", match)
	case len(hits) > 1:
		return AuthorizedKey{}, fmt.Errorf("%d keys match %q; remove by fingerprint instead (see 'mason key ls')", len(hits), match)
	}
	victim, _ := ParsePublicKey(lines[hits[0]])
	if !force {
		if keys == 1 {
			return victim, errors.New("refusing to remove the last authorized key (pass --force to lock everyone out)")
		}
		if cur, err := ParsePublicKey(current); err == nil && cur.Blob == victim.Blob {
			return victim, errors.New("refusing to remove the key this machine is using (pass --force)")
		}
	}

	kept := append(append([]string(nil), lines[:hits[0]]...), lines[hits[0]+1:]...)
	body := strings.Join(kept, "\n")
	if body != "" {
		body += "\n"
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	cmd := fmt.Sprintf("umask 077 && printf %%s %s | base64 -d > %[2]s.mason-new && mv %[2]s.mason-new %[2]s",
		shellQuote(encoded), authorizedKeysPath)
	if _, err := runner.Run(ctx, cmd); err != nil {
		return victim, fmt.Errorf("rewrite authorized_keys: %w", err)
	}
	return victim, nil
}

func readAuthorizedKeys(ctx context.Context, runner remote.CommandRunner) ([]string, error) {
	res, err := runner.Run(ctx, "cat "+authorizedKeysPath+" 2>/dev/null || true")
	if err != nil {
		return nil, fmt.Errorf("read authorized_keys: %w", err)
	}
	text := strings.TrimRight(res.Stdout, "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

// splitKeyFields splits on whitespace outside double quotes (options like
// command="a b" contain spaces).
func splitKeyFields(s string) []string {
	var fields []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case (r == ' ' || r == '\t') && !inQuote:
			if cur.Len() > 0 {
				fields = append(fields, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		fields = append(fields, cur.String())
	}
	return fields
}

// sshString reads the leading length-prefixed string of an SSH wire-format blob.
func sshString(b []byte) (string, bool) {
	if len(b) < 4 {
		return "", false
	}
	n := binary.BigEndian.Uint32(b[:4])
	if uint64(len(b)-4) < uint64(n) {
		return "", false
	}
	return string(b[4 : 4+n]), true
}
