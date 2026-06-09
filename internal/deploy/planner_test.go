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
	target := config.Target{
		Host:    "share.example.com",
		User:    "mason",
		Root:    "/srv/share",
		BaseURL: "https://share.example.com",
		Deploy: config.DeployConfig{
			BuildDir:   buildDir,
			RemotePath: "/srv/site",
		},
	}

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
	planner := NewPlanner(config.Target{}, &fakeRunner{})

	tests := []struct {
		name   string
		target config.Target
	}{
		{
			name:   "missing build dir",
			target: config.Target{Deploy: config.DeployConfig{RemotePath: "/srv/site"}},
		},
		{
			name:   "missing remote path",
			target: config.Target{Deploy: config.DeployConfig{BuildDir: "./public"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			planner.target = tt.target
			_, err := planner.Plan(context.Background(), PlanOptions{})
			if err == nil {
				t.Fatalf("Plan succeeded, want error")
			}
		})
	}
}

func TestPlannerRejectsMissingBuildDirectory(t *testing.T) {
	planner := NewPlanner(config.Target{
		Deploy: config.DeployConfig{
			BuildDir:   filepath.Join(t.TempDir(), "missing"),
			RemotePath: "/srv/site",
		},
	}, &fakeRunner{})

	_, err := planner.Plan(context.Background(), PlanOptions{})
	if err == nil {
		t.Fatalf("Plan succeeded, want error")
	}
}

func TestPlannerWrapsRunnerError(t *testing.T) {
	buildDir := writeBuildDir(t)
	wantErr := errors.New("rsync failed")
	planner := NewPlanner(config.Target{
		Host: "share.example.com",
		User: "mason",
		Deploy: config.DeployConfig{
			BuildDir:   buildDir,
			RemotePath: "/srv/site",
		},
	}, &fakeRunner{err: wantErr})

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
	planner := NewPlanner(config.Target{
		Host: "share.example.com",
		User: "mason",
		Deploy: config.DeployConfig{
			BuildDir:   relDir,
			RemotePath: "/srv/site",
		},
	}, runner)

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

	planner := NewPlanner(config.Target{
		Deploy: config.DeployConfig{
			BuildDir:   local,
			RemotePath: remoteDir,
		},
	}, &localCompareRunner{dest: remoteDir})

	plan, err := planner.Plan(context.Background(), PlanOptions{Delete: true})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.New != 1 || plan.Remove != 1 || plan.Unchanged != 1 {
		t.Fatalf("counts = new:%d unchanged:%d remove:%d, want 1/1/1",
			plan.New, plan.Unchanged, plan.Remove)
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
