package deploy

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/config"
	"github.com/norlinga/mason/internal/remote"
)

func TestParseCompareOutput(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    compareStats
		wantErr bool
	}{
		{
			name: "mixed changes",
			output: `*deleting   stale.txt
>f.s....... c1.txt
>f.s....... c2.txt
>f+++++++++ new.txt

Number of files: 7 (reg: 6, dir: 1)
Number of created files: 1 (reg: 1)
Number of deleted files: 1 (reg: 1)
Number of regular files transferred: 3
Total file size: 32 bytes
Total transferred file size: 23 bytes
`,
			want: compareStats{New: 1, Changed: 2, Unchanged: 3, Remove: 1, BytesToSend: 23},
		},
		{
			name: "already up to date",
			output: `Number of files: 4 (reg: 3, dir: 1)
Number of created files: 0
Number of deleted files: 0
Number of regular files transferred: 0
Total transferred file size: 0 bytes
`,
			want: compareStats{Unchanged: 3},
		},
		{
			name: "comma separated bytes",
			output: `Number of files: 200 (reg: 199, dir: 1)
Number of created files: 12 (reg: 12)
Number of deleted files: 1 (reg: 1)
Number of regular files transferred: 15
Total transferred file size: 28,400,000 bytes
`,
			want: compareStats{New: 12, Changed: 3, Unchanged: 184, Remove: 1, BytesToSend: 28400000},
		},
		{
			// Verbatim rsync 3.x output: thousands separators and a dir breakdown.
			name: "large tree with directories",
			output: `Number of files: 1,207 (reg: 1,205, dir: 2)
Number of created files: 1,206 (reg: 1,205, dir: 1)
Number of deleted files: 3 (reg: 2, dir: 1)
Number of regular files transferred: 1,205
Total transferred file size: 4,918 bytes
`,
			want: compareStats{New: 1205, Remove: 2, BytesToSend: 4918},
		},
		{
			name:    "missing stats",
			output:  `>f+++++++++ new.txt`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCompareOutput(tt.output)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseCompareOutput succeeded, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCompareOutput: %v", err)
			}
			if got != tt.want {
				t.Fatalf("stats = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPlannerPlan(t *testing.T) {
	buildDir := writeBuildDir(t)
	target := testSite(buildDir, "/srv/site")

	runner := &fakeRunner{
		result: remote.ProcessResult{
			Stdout: `Number of files: 4 (reg: 3, dir: 1)
Number of created files: 1 (reg: 1)
Number of deleted files: 1 (reg: 1)
Number of regular files transferred: 2
Total transferred file size: 24 bytes
`,
		},
	}

	planner := NewPlanner(target, runner)
	plan, err := planner.Plan(context.Background(), PlanOptions{Delete: true})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if plan.BuildDir != buildDir {
		t.Fatalf("BuildDir = %q, want %q", plan.BuildDir, buildDir)
	}
	if plan.RemotePath != "/srv/site" {
		t.Fatalf("RemotePath = %q, want /srv/site", plan.RemotePath)
	}
	if plan.New != 1 || plan.Changed != 1 || plan.Unchanged != 1 || plan.Remove != 1 {
		t.Fatalf("counts = new:%d changed:%d unchanged:%d remove:%d, want 1/1/1/1",
			plan.New, plan.Changed, plan.Unchanged, plan.Remove)
	}
	if plan.BytesToSend != 24 {
		t.Fatalf("BytesToSend = %d, want 24", plan.BytesToSend)
	}

	if runner.request.Source == "" || !strings.HasSuffix(runner.request.Source, string(os.PathSeparator)) {
		t.Fatalf("source = %q, want trailing slash", runner.request.Source)
	}
	if runner.request.Destination != "mason@share.example.com:/srv/site/" {
		t.Fatalf("destination = %q, want mason@share.example.com:/srv/site/", runner.request.Destination)
	}
	if !runner.request.Delete {
		t.Fatalf("Delete = false, want true")
	}
	if runner.request.SSH == nil || runner.request.SSH.Host != "share.example.com" {
		t.Fatalf("SSH options = %#v, want host share.example.com", runner.request.SSH)
	}
}

func TestPlannerRequiresDeployConfig(t *testing.T) {
	planner := NewPlanner(testSite("", ""), &fakeRunner{})

	tests := []struct {
		name   string
		target config.Site
	}{
		{
			name:   "missing build dir",
			target: testSite("", "/srv/site"),
		},
		{
			name:   "missing remote path",
			target: testSite("./public", ""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			planner.site = tt.target
			_, err := planner.Plan(context.Background(), PlanOptions{})
			if err == nil {
				t.Fatalf("Plan succeeded, want error")
			}
		})
	}
}

func TestPlannerRejectsMissingBuildDirectory(t *testing.T) {
	planner := NewPlanner(testSite(filepath.Join(t.TempDir(), "missing"), "/srv/site"), &fakeRunner{})

	_, err := planner.Plan(context.Background(), PlanOptions{})
	if err == nil {
		t.Fatalf("Plan succeeded, want error")
	}
}

func TestPlannerWrapsRunnerError(t *testing.T) {
	buildDir := writeBuildDir(t)
	wantErr := errors.New("rsync failed")
	planner := NewPlanner(testSite(buildDir, "/srv/site"), &fakeRunner{err: wantErr})

	_, err := planner.Plan(context.Background(), PlanOptions{})
	if err == nil {
		t.Fatalf("Plan succeeded, want error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped runner error", err)
	}
}

func TestPlannerResolvesRelativeBuildDir(t *testing.T) {
	baseDir := t.TempDir()
	relDir := "public"
	if err := os.Mkdir(filepath.Join(baseDir, relDir), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	runner := &fakeRunner{
		result: remote.ProcessResult{
			Stdout: `Number of files: 1 (reg: 0, dir: 1)
Number of created files: 0
Number of deleted files: 0
Number of regular files transferred: 0
Total transferred file size: 0 bytes
`,
		},
	}
	planner := NewPlanner(testSite(relDir, "/srv/site"), runner)

	if _, err := planner.Plan(context.Background(), PlanOptions{BaseDir: baseDir}); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	wantSource := filepath.Join(baseDir, relDir) + string(os.PathSeparator)
	if runner.request.Source != wantSource {
		t.Fatalf("source = %q, want %q", runner.request.Source, wantSource)
	}
}

func TestPlannerCompareDryRunLocally(t *testing.T) {
	requireRsync(t)

	local := t.TempDir()
	remoteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "same.txt"), []byte("same"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(remoteDir, "same.txt"), []byte("same"), 0o644); err != nil {
		t.Fatalf("write remote file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(local, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(remoteDir, "stale.txt"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	planner := NewPlanner(testSite(local, remoteDir), &localCompareRunner{dest: remoteDir})

	plan, err := planner.Plan(context.Background(), PlanOptions{Delete: true})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.New != 1 || plan.Remove != 1 || plan.Unchanged != 1 {
		t.Fatalf("counts = new:%d unchanged:%d remove:%d, want 1/1/1",
			plan.New, plan.Unchanged, plan.Remove)
	}
}

func TestPlannerExcludesDotfilesLocally(t *testing.T) {
	requireRsync(t)

	local := t.TempDir()
	remoteDir := t.TempDir()
	for name, body := range map[string]string{
		"index.html":                 "home",
		".well-known/security.txt":   "contact",
		".git/config":                "[core]",
		".env":                       "SECRET=1",
		".mason":                     "default: home",
		"assets/.DS_Store":           "junk",
		"nested/.well-known/ignored": "nested",
	} {
		writeFile(t, filepath.Join(local, name), body)
	}
	// A dotfile already on the server must survive --delete (excluded paths are protected).
	writeFile(t, filepath.Join(remoteDir, ".git", "HEAD"), "ref")

	planner := NewPlanner(testSite(local, remoteDir), &localCompareRunner{dest: remoteDir})

	plan, err := planner.Plan(context.Background(), PlanOptions{Delete: true})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	// Only index.html ("home") and .well-known/security.txt ("contact") are sent.
	if want := int64(len("home") + len("contact")); plan.New != 2 || plan.BytesToSend != want || plan.Remove != 0 {
		t.Fatalf("new = %d bytes_to_send = %d remove = %d, want 2, %d, 0",
			plan.New, plan.BytesToSend, plan.Remove, want)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeBuildDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func requireRsync(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skipf("rsync not installed: %v", err)
	}
}

type fakeRunner struct {
	request remote.CompareDryRunRequest
	result  remote.ProcessResult
	err     error
}

func (r *fakeRunner) CompareDryRun(_ context.Context, request remote.CompareDryRunRequest) (remote.ProcessResult, error) {
	r.request = request
	if r.err != nil {
		return r.result, r.err
	}
	return r.result, nil
}

type localCompareRunner struct {
	dest string
}

func (r *localCompareRunner) CompareDryRun(ctx context.Context, request remote.CompareDryRunRequest) (remote.ProcessResult, error) {
	request.Destination = ensureTrailingSlash(r.dest)
	request.SSH = nil
	return remote.NewRsync().CompareDryRun(ctx, request)
}

func testSite(build, path string) config.Site {
	return config.Site{
		Name:   "home",
		URL:    "https://share.example.com",
		Host:   "share.example.com",
		Build:  build,
		Path:   path,
		Server: &config.Server{User: "mason", Host: "share.example.com"},
	}
}
