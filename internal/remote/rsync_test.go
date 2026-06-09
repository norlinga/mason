package remote

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/norlinga/mason/internal/config"
)

func TestRsyncBuildsArgsAndReportsProgress(t *testing.T) {
	source := writeTempFile(t, "demo.txt", "hello")
	runner := &fakeProcessRunner{
		result: ProcessResult{Stdout: "demo.txt\n42 100%\n"},
		output: []string{
			"demo.txt",
			"42 100%",
		},
	}
	rsync := NewRsyncWithRunner("rsync-test", runner)

	var progress []RsyncProgress
	result, err := rsync.Upload(context.Background(), RsyncRequest{
		Source:      source,
		Destination: "mason@example.com:/srv/share/demo.txt",
		Delete:      true,
		DryRun:      true,
		SSH:         &SSHOptions{Port: 2222, KeyPath: source},
		OnProgress: func(next RsyncProgress) {
			progress = append(progress, next)
		},
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if result.Stdout != runner.result.Stdout {
		t.Fatalf("Stdout = %q, want %q", result.Stdout, runner.result.Stdout)
	}
	if runner.name != "rsync-test" {
		t.Fatalf("runner name = %q, want rsync-test", runner.name)
	}
	for _, want := range []string{"-a", "--human-readable", "--info=progress2", "--out-format=%n", "--delete", "--dry-run", "-e"} {
		if !slices.Contains(runner.args, want) {
			t.Fatalf("args missing %q: %#v", want, runner.args)
		}
	}
	if got, want := runner.args[len(runner.args)-2], source; got != want {
		t.Fatalf("source arg = %q, want %q", got, want)
	}
	if got, want := runner.args[len(runner.args)-1], "mason@example.com:/srv/share/demo.txt"; got != want {
		t.Fatalf("destination arg = %q, want %q", got, want)
	}
	if len(progress) != 2 {
		t.Fatalf("progress length = %d, want 2", len(progress))
	}
	if progress[1].Percent != 100 {
		t.Fatalf("progress percent = %d, want 100", progress[1].Percent)
	}
}

func TestRsyncRejectsInvalidRequest(t *testing.T) {
	rsync := NewRsyncWithRunner("rsync", &fakeProcessRunner{})

	tests := []struct {
		name    string
		request RsyncRequest
	}{
		{name: "missing source", request: RsyncRequest{Destination: "dest"}},
		{name: "missing destination", request: RsyncRequest{Source: writeTempFile(t, "demo.txt", "hello")}},
		{name: "missing source file", request: RsyncRequest{Source: filepath.Join(t.TempDir(), "missing"), Destination: "dest"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := rsync.Upload(context.Background(), tt.request)
			if err == nil {
				t.Fatalf("Upload succeeded, want error")
			}
		})
	}
}

func TestRsyncWrapsRunnerError(t *testing.T) {
	source := writeTempFile(t, "demo.txt", "hello")
	wantErr := errors.New("rsync failed")
	rsync := NewRsyncWithRunner("rsync", &fakeProcessRunner{
		result: ProcessResult{Stderr: "bad\n", ExitCode: 12},
		err:    wantErr,
	})

	result, err := rsync.Upload(context.Background(), RsyncRequest{
		Source:      source,
		Destination: "dest",
	})
	if err == nil {
		t.Fatalf("Upload succeeded, want error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped runner error", err)
	}
	if result.ExitCode != 12 {
		t.Fatalf("ExitCode = %d, want 12", result.ExitCode)
	}
}

func TestRsyncCopiesFileLocally(t *testing.T) {
	requireRsync(t)

	source := writeTempFile(t, "demo.txt", "hello")
	destDir := t.TempDir()

	_, err := NewRsync().Upload(context.Background(), RsyncRequest{
		Source:      source,
		Destination: destDir + string(os.PathSeparator),
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(destDir, "demo.txt"))
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("copied file = %q, want hello", string(got))
	}
}

func TestRsyncCopiesDirectoryLocally(t *testing.T) {
	requireRsync(t)

	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "nested.txt"), []byte("contents"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	destDir := t.TempDir()

	_, err := NewRsync().Upload(context.Background(), RsyncRequest{
		Source:      sourceDir + string(os.PathSeparator),
		Destination: destDir + string(os.PathSeparator),
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(destDir, "nested.txt"))
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if string(got) != "contents" {
		t.Fatalf("copied file = %q, want contents", string(got))
	}
}

func TestRsyncDestinationFromTarget(t *testing.T) {
	target := config.Target{
		Host: "share.example.com",
		User: "mason",
		Port: 2222,
	}
	got := RsyncDestinationFromTarget(target, "/srv/share/demo.txt")
	want := "mason@share.example.com:/srv/share/demo.txt"
	if got != want {
		t.Fatalf("destination = %q, want %q", got, want)
	}
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func requireRsync(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(defaultRsyncBinary); err != nil {
		t.Skipf("rsync not installed: %v", err)
	}
}

type fakeProcessRunner struct {
	name   string
	args   []string
	result ProcessResult
	output []string
	err    error
}

func (r *fakeProcessRunner) Run(ctx context.Context, name string, args []string, onOutput func(string)) (ProcessResult, error) {
	r.name = name
	r.args = append([]string(nil), args...)
	for _, line := range r.output {
		onOutput(line)
	}
	return r.result, r.err
}
