package remote

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestRemotePathJoinsRoot(t *testing.T) {
	tests := []struct {
		root string
		rel  string
		want string
	}{
		{root: "/srv/share", rel: "inbox/foo.mp3", want: "/srv/share/inbox/foo.mp3"},
		{root: "/srv/share/", rel: "/s/f76.mp3", want: "/srv/share/s/f76.mp3"},
		{root: "/srv/share", rel: "", want: "/srv/share"},
		{root: "", rel: "objects/ab/cd/hash", want: "objects/ab/cd/hash"},
		{root: "/", rel: ".mason/manifest.json", want: "/.mason/manifest.json"},
	}

	for _, tt := range tests {
		if got := RemotePath(tt.root, tt.rel); got != tt.want {
			t.Fatalf("RemotePath(%q, %q) = %q, want %q", tt.root, tt.rel, got, tt.want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: "''"},
		{in: "plain", want: "'plain'"},
		{in: "it's", want: "'it'\\''s'"},
	}

	for _, tt := range tests {
		if got := shellQuote(tt.in); got != tt.want {
			t.Fatalf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSSHFSMkdirAll(t *testing.T) {
	runner := &recordingCommandRunner{}
	fs := NewSSHFS(runner)

	if err := fs.MkdirAll(context.Background(), "/srv/share/.mason"); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands length = %d, want 1", len(runner.commands))
	}
	if got, want := runner.commands[0], "mkdir -p '/srv/share/.mason'"; got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestSSHFSMkdirAllRejectsEmptyPath(t *testing.T) {
	fs := NewSSHFS(&recordingCommandRunner{})
	if err := fs.MkdirAll(context.Background(), "  "); err == nil {
		t.Fatalf("MkdirAll succeeded, want error")
	}
}

func TestSSHFSSymlink(t *testing.T) {
	runner := &recordingCommandRunner{}
	fs := NewSSHFS(runner)

	err := fs.Symlink(context.Background(), "/srv/share/s/f76.mp3", "../objects/ab/cd/hash")
	if err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if got, want := runner.commands[0], "ln -sfn '../objects/ab/cd/hash' '/srv/share/s/f76.mp3'"; got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestSSHFSSymlinkRejectsMissingPaths(t *testing.T) {
	fs := NewSSHFS(&recordingCommandRunner{})

	if err := fs.Symlink(context.Background(), "", "target"); err == nil {
		t.Fatalf("Symlink with empty link path succeeded, want error")
	}
	if err := fs.Symlink(context.Background(), "link", ""); err == nil {
		t.Fatalf("Symlink with empty target succeeded, want error")
	}
}

func TestSSHFSRemove(t *testing.T) {
	runner := &recordingCommandRunner{}
	fs := NewSSHFS(runner)

	if err := fs.Remove(context.Background(), "/srv/share/inbox/foo.mp3"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got, want := runner.commands[0], "rm -f '/srv/share/inbox/foo.mp3'"; got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestSSHFSReadFile(t *testing.T) {
	runner := &recordingCommandRunner{
		results: map[string]CommandResult{
			"cat '/srv/share/.mason/manifest.json'": {Stdout: `{"version":1}` + "\n"},
		},
	}
	fs := NewSSHFS(runner)

	data, err := fs.ReadFile(context.Background(), "/srv/share/.mason/manifest.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got, want := string(data), `{"version":1}`+"\n"; got != want {
		t.Fatalf("data = %q, want %q", got, want)
	}
}

func TestSSHFSReadFilePropagatesError(t *testing.T) {
	wantErr := errors.New("missing file")
	runner := &recordingCommandRunner{err: wantErr}
	fs := NewSSHFS(runner)

	_, err := fs.ReadFile(context.Background(), "/srv/share/.mason/manifest.json")
	if err == nil {
		t.Fatalf("ReadFile succeeded, want error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped runner error", err)
	}
}

func TestSSHFSWriteFile(t *testing.T) {
	runner := &recordingCommandRunner{}
	fs := NewSSHFS(runner)

	payload := []byte(`{"version":1}`)
	if err := fs.WriteFile(context.Background(), "/srv/share/.mason/manifest.json", payload, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if len(runner.commands) != 2 {
		t.Fatalf("commands length = %d, want 2", len(runner.commands))
	}
	if got, want := runner.commands[0], "mkdir -p '/srv/share/.mason'"; got != want {
		t.Fatalf("mkdir command = %q, want %q", got, want)
	}

	encoded := base64.StdEncoding.EncodeToString(payload)
	wantWrite := "printf %s '" + encoded + "' | base64 -d > '/srv/share/.mason/manifest.json' && chmod 600 '/srv/share/.mason/manifest.json'"
	if got := runner.commands[1]; got != wantWrite {
		t.Fatalf("write command = %q, want %q", got, wantWrite)
	}
}

func TestSSHFSWriteFileSkipsMkdirForRootFile(t *testing.T) {
	runner := &recordingCommandRunner{}
	fs := NewSSHFS(runner)

	if err := fs.WriteFile(context.Background(), "/manifest.json", []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands length = %d, want 1", len(runner.commands))
	}
	if !strings.HasPrefix(runner.commands[0], "printf %s ") {
		t.Fatalf("command = %q, want write command", runner.commands[0])
	}
}

type recordingCommandRunner struct {
	commands []string
	results  map[string]CommandResult
	err      error
}

func (r *recordingCommandRunner) Run(ctx context.Context, command string) (CommandResult, error) {
	r.commands = append(r.commands, command)
	if r.err != nil {
		return CommandResult{ExitCode: -1}, r.err
	}
	if r.results != nil {
		if result, ok := r.results[command]; ok {
			return result, nil
		}
	}
	return CommandResult{}, nil
}
