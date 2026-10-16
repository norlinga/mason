package setup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runSnippetScript executes the snippet-install and reload fragments for real, with fake
// caddy/systemctl binaries on PATH. validateOK controls whether `caddy validate` passes.
// It returns the exit code and combined output.
func runSnippetScript(t *testing.T, dir, content string, force, validateOK bool) (int, string) {
	t.Helper()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	validate := "exit 0"
	if !validateOK {
		validate = "echo 'Error: adapting config' >&2; exit 1"
	}
	writeExec(t, filepath.Join(bin, "caddy"), "#!/bin/sh\n"+validate+"\n")
	writeExec(t, filepath.Join(bin, "systemctl"), "#!/bin/sh\necho \"systemctl $*\" >> \""+dir+"/systemctl.log\"\n")

	var b strings.Builder
	writeScriptPreamble(&b, "test", "host")
	b.WriteString("SNIPPET_DIR=" + shellQuote(filepath.Join(dir, "mason.d")) + "\n")
	b.WriteString("CADDYFILE=" + shellQuote(filepath.Join(dir, "Caddyfile")) + "\n")
	b.WriteString("CADDY_GROUP_CHANGED=0\n")
	writeSnippetInstall(&b, filepath.Join(dir, "mason.d", "x.example.com.caddy"), content)
	writeCaddyReloadBlock(&b, 2, true)

	cmd := exec.Command("sh", "-c", b.String())
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	if force {
		cmd.Env = append(cmd.Env, "FORCE=1")
	}
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run script: %v", err)
	}
	return code, string(out)
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

const managedV1 = ManagedMarker + "\nhttps://x.example.com {\n\tfile_server\n}\n"
const managedV2 = ManagedMarker + "\nhttps://x.example.com {\n\troot * /srv/x\n\tfile_server\n}\n"

func TestSnippetInstallFreshAndUpdate(t *testing.T) {
	dir := t.TempDir()
	snippet := filepath.Join(dir, "mason.d", "x.example.com.caddy")

	if code, out := runSnippetScript(t, dir, managedV1, false, true); code != 0 {
		t.Fatalf("fresh install exit %d: %s", code, out)
	}
	if got := readFile(t, snippet); got != managedV1 {
		t.Fatalf("snippet = %q", got)
	}
	if code, out := runSnippetScript(t, dir, managedV2, false, true); code != 0 {
		t.Fatalf("update exit %d: %s", code, out)
	}
	if got := readFile(t, snippet); got != managedV2 {
		t.Fatalf("managed snippet not updated: %q", got)
	}
	if _, err := os.Stat(snippet + ".mason-prev"); !os.IsNotExist(err) {
		t.Fatal(".mason-prev left behind after a successful validate")
	}
	if log := readFile(t, filepath.Join(dir, "systemctl.log")); !strings.Contains(log, "systemctl reload caddy") {
		t.Fatalf("caddy not reloaded: %q", log)
	}
}

func TestSnippetInstallRefusesHandEdited(t *testing.T) {
	dir := t.TempDir()
	snippet := filepath.Join(dir, "mason.d", "x.example.com.caddy")
	hand := "https://x.example.com {\n\theader /a b\n\tfile_server\n}\n"
	if err := os.MkdirAll(filepath.Dir(snippet), 0o755); err != nil {
		t.Fatal(err)
	}
	writeExec(t, snippet, hand)

	code, out := runSnippetScript(t, dir, managedV2, false, true)
	if code != 3 {
		t.Fatalf("exit = %d, want 3; output:\n%s", code, out)
	}
	if !strings.Contains(out, "not written by mason") || !strings.Contains(out, "+\troot * /srv/x") {
		t.Fatalf("missing explanation or diff:\n%s", out)
	}
	if got := readFile(t, snippet); got != hand {
		t.Fatalf("hand-edited snippet modified: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "systemctl.log")); !os.IsNotExist(err) {
		t.Fatal("caddy reloaded after a refusal")
	}

	code, out = runSnippetScript(t, dir, managedV2, true, true)
	if code != 0 {
		t.Fatalf("FORCE exit %d: %s", code, out)
	}
	if got := readFile(t, snippet); got != managedV2 {
		t.Fatalf("FORCE did not replace: %q", got)
	}
	backups, _ := filepath.Glob(snippet + ".bak-*")
	if len(backups) != 1 || readFile(t, backups[0]) != hand {
		t.Fatalf("FORCE backup missing or wrong: %v", backups)
	}
}

func TestSnippetInstallRollsBackOnInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	snippet := filepath.Join(dir, "mason.d", "x.example.com.caddy")
	if code, out := runSnippetScript(t, dir, managedV1, false, true); code != 0 {
		t.Fatalf("fresh install exit %d: %s", code, out)
	}
	_ = os.Remove(filepath.Join(dir, "systemctl.log"))

	code, out := runSnippetScript(t, dir, managedV2, false, false)
	if code != 1 || !strings.Contains(out, "previous configuration was restored") {
		t.Fatalf("exit = %d, output:\n%s", code, out)
	}
	if got := readFile(t, snippet); got != managedV1 {
		t.Fatalf("snippet not rolled back: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "systemctl.log")); !os.IsNotExist(err) {
		t.Fatal("caddy reloaded despite failed validation")
	}

	// A brand-new snippet that fails validation is removed entirely.
	dir2 := t.TempDir()
	if code, _ := runSnippetScript(t, dir2, managedV2, false, false); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(dir2, "mason.d", "x.example.com.caddy")); !os.IsNotExist(err) {
		t.Fatal("invalid new snippet left in place")
	}
}
