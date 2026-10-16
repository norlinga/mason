package deploy

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/norlinga/mason/internal/remote"
)

func TestServiceExecuteDeploys(t *testing.T) {
	buildDir := writeBuildDir(t)
	runner := &recordingRsync{}
	svc := NewService(testSite(buildDir, "/srv/site"), runner)

	plan := Plan{BuildDir: buildDir, RemotePath: "/srv/site", New: 1, BytesToSend: 4}
	res, err := svc.Execute(context.Background(), DeployOptions{}, plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Deployed {
		t.Fatal("Deployed = false, want true")
	}
	if runner.uploadCalls != 1 {
		t.Fatalf("upload calls = %d, want 1", runner.uploadCalls)
	}
}

func TestServiceExecuteAppliesDefaultFilters(t *testing.T) {
	buildDir := writeBuildDir(t)
	runner := &recordingRsync{}
	svc := NewService(testSite(buildDir, "/srv/site"), runner)

	plan := Plan{BuildDir: buildDir, RemotePath: "/srv/site", New: 1}
	if _, err := svc.Execute(context.Background(), DeployOptions{}, plan); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !slices.Equal(runner.uploadRequest.Filters, DefaultFilters) {
		t.Fatalf("upload filters = %#v, want %#v", runner.uploadRequest.Filters, DefaultFilters)
	}
}

func TestServiceRunDryRun(t *testing.T) {
	buildDir := writeBuildDir(t)
	runner := &recordingRsync{
		compareResult: remote.ProcessResult{
			Stdout: `Number of files: 2 (reg: 1, dir: 1)
Number of created files: 1 (reg: 1)
Number of deleted files: 0
Number of regular files transferred: 1
Total transferred file size: 4 bytes
`,
		},
	}
	svc := NewService(testSite(buildDir, "/srv/site"), runner)

	res, err := svc.Run(context.Background(), DeployOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.DryRun || res.Deployed {
		t.Fatalf("DryRun=%v Deployed=%v, want dry-run without deploy", res.DryRun, res.Deployed)
	}
	if res.Plan.New != 1 {
		t.Fatalf("Plan.New = %d, want 1", res.Plan.New)
	}
	if runner.uploadCalls != 0 {
		t.Fatalf("upload calls = %d, want 0", runner.uploadCalls)
	}
}

func TestServiceRunDeploys(t *testing.T) {
	buildDir := writeBuildDir(t)
	runner := &recordingRsync{
		compareResult: remote.ProcessResult{
			Stdout: `Number of files: 2 (reg: 1, dir: 1)
Number of created files: 1 (reg: 1)
Number of deleted files: 0
Number of regular files transferred: 1
Total transferred file size: 4 bytes
`,
		},
	}
	svc := NewService(testSite(buildDir, "/srv/site"), runner)

	res, err := svc.Run(context.Background(), DeployOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Deployed || res.UpToDate {
		t.Fatalf("Deployed=%v UpToDate=%v, want deployed", res.Deployed, res.UpToDate)
	}
	if runner.uploadCalls != 1 {
		t.Fatalf("upload calls = %d, want 1", runner.uploadCalls)
	}
}

func TestServiceRunUpToDateSkipsUpload(t *testing.T) {
	buildDir := writeBuildDir(t)
	runner := &recordingRsync{
		compareResult: remote.ProcessResult{
			Stdout: `Number of files: 2 (reg: 1, dir: 1)
Number of created files: 0
Number of deleted files: 0
Number of regular files transferred: 0
Total transferred file size: 0 bytes
`,
		},
	}
	svc := NewService(testSite(buildDir, "/srv/site"), runner)

	res, err := svc.Run(context.Background(), DeployOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.UpToDate || res.Deployed {
		t.Fatalf("UpToDate=%v Deployed=%v, want up to date without deploy", res.UpToDate, res.Deployed)
	}
	if runner.uploadCalls != 0 {
		t.Fatalf("upload calls = %d, want 0", runner.uploadCalls)
	}
}

func TestServiceRunWrapsUploadError(t *testing.T) {
	buildDir := writeBuildDir(t)
	wantErr := errors.New("rsync failed")
	runner := &recordingRsync{
		compareResult: remote.ProcessResult{
			Stdout: `Number of files: 2 (reg: 1, dir: 1)
Number of created files: 1 (reg: 1)
Number of deleted files: 0
Number of regular files transferred: 1
Total transferred file size: 4 bytes
`,
		},
		uploadErr: wantErr,
	}
	svc := NewService(testSite(buildDir, "/srv/site"), runner)

	_, err := svc.Run(context.Background(), DeployOptions{})
	if err == nil {
		t.Fatalf("Run succeeded, want error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped upload error", err)
	}
}

func TestUpToDate(t *testing.T) {
	if !UpToDate(Plan{}) {
		t.Fatal("empty plan should be up to date")
	}
	if UpToDate(Plan{New: 1}) {
		t.Fatal("plan with new files should not be up to date")
	}
}

func TestFilesUpdatedAndTotalFiles(t *testing.T) {
	plan := Plan{New: 2, Changed: 3, Unchanged: 10}
	if got := FilesUpdated(plan); got != 5 {
		t.Fatalf("FilesUpdated = %d, want 5", got)
	}
	if got := TotalFiles(plan); got != 15 {
		t.Fatalf("TotalFiles = %d, want 15", got)
	}
}

type recordingRsync struct {
	compareResult remote.ProcessResult
	compareErr    error
	uploadResult  remote.ProcessResult
	uploadErr     error
	uploadCalls   int
	uploadRequest remote.RsyncRequest
}

func (r *recordingRsync) CompareDryRun(_ context.Context, _ remote.CompareDryRunRequest) (remote.ProcessResult, error) {
	if r.compareErr != nil {
		return r.compareResult, r.compareErr
	}
	return r.compareResult, nil
}

func (r *recordingRsync) Upload(_ context.Context, request remote.RsyncRequest) (remote.ProcessResult, error) {
	r.uploadCalls++
	r.uploadRequest = request
	time.Sleep(time.Millisecond)
	if r.uploadErr != nil {
		return r.uploadResult, r.uploadErr
	}
	return r.uploadResult, nil
}
