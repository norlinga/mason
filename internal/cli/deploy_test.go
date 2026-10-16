package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/norlinga/mason/internal/deploy"
	"github.com/norlinga/mason/internal/remote"
)

func sampleDeployPlan() deploy.Plan {
	return deploy.Plan{
		New:         12,
		Changed:     3,
		Unchanged:   184,
		Remove:      1,
		BytesToSend: 28400000,
	}
}

func sampleDeployResult(dryRun, upToDate, deployed bool) deploy.Result {
	return deploy.Result{
		Site:       "blog.example.com",
		URL:        "https://blog.example.com",
		Server:     "example.com",
		BuildDir:   "./public",
		RemotePath: "/srv/site",
		Plan:       sampleDeployPlan(),
		DryRun:     dryRun,
		UpToDate:   upToDate,
		Deployed:   deployed,
		Duration:   5100 * time.Millisecond,
	}
}

func execDeployCommand(t *testing.T, args []string, run deployRunFunc) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "deploy" || c.Name() == "status" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newDeployCommand(&stdout, &stderr, run))
	root.AddCommand(newStatusCommand(&stdout, &stderr, run))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestStatusCommandPlain(t *testing.T) {
	stdout, _, err := execDeployCommand(t, []string{"status"},
		func(_ context.Context, _ rootOptions, req deployRequest) (deploy.Result, error) {
			if !req.DryRun {
				t.Fatal("status should always plan with dry-run")
			}
			return sampleDeployResult(true, false, false), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "Site:  https://blog.example.com → example.com:/srv/site\n" +
		"Build: ./public\n" +
		"Plan:  12 new · 3 changed · 184 unchanged · 1 to remove   (27.1 MB to send)\n"
	if stdout != want {
		t.Fatalf("status output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestDeployCommandDryRun(t *testing.T) {
	stdout, _, err := execDeployCommand(t, []string{"deploy", "--dry-run"},
		func(_ context.Context, _ rootOptions, req deployRequest) (deploy.Result, error) {
			if !req.DryRun {
				t.Fatal("deploy -n should plan with dry-run")
			}
			return sampleDeployResult(true, false, false), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "Site:  https://blog.example.com → example.com:/srv/site\n" +
		"Build: ./public\n" +
		"Plan:  12 new · 3 changed · 184 unchanged · 1 to remove   (27.1 MB to send)\n"
	if stdout != want {
		t.Fatalf("dry-run output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestDeployCommandUpToDate(t *testing.T) {
	stdout, _, err := execDeployCommand(t, []string{"deploy"},
		func(_ context.Context, _ rootOptions, req deployRequest) (deploy.Result, error) {
			if !req.DryRun {
				t.Fatal("first deploy pass should dry-run")
			}
			res := sampleDeployResult(true, true, false)
			res.Plan = deploy.Plan{Unchanged: 199}
			return res, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "Site:  https://blog.example.com → example.com:/srv/site\n" +
		"Build: ./public\n" +
		"Plan:  0 new · 0 changed · 199 unchanged\n" +
		"✓ Already up to date (199 files, 0 B sent)\n"
	if stdout != want {
		t.Fatalf("up-to-date output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestDeployCommandDeploySummary(t *testing.T) {
	calls := 0
	var execPlan *deploy.Plan
	stdout, _, err := execDeployCommand(t, []string{"--yes", "deploy"},
		func(_ context.Context, _ rootOptions, req deployRequest) (deploy.Result, error) {
			calls++
			if req.DryRun {
				if req.Plan != nil {
					t.Fatal("plan pass should not receive a precomputed plan")
				}
				return sampleDeployResult(true, false, false), nil
			}
			execPlan = req.Plan
			return sampleDeployResult(false, false, true), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if calls != 2 {
		t.Fatalf("run calls = %d, want 2 (plan then deploy)", calls)
	}
	if execPlan == nil {
		t.Fatal("execute pass should reuse the precomputed plan instead of re-planning, got nil")
	}
	want := "Site:  https://blog.example.com → example.com:/srv/site\n" +
		"Build: ./public\n" +
		"Plan:  12 new · 3 changed · 184 unchanged · 1 to remove   (27.1 MB to send)\n" +
		"Sending …\n" +
		"✓ Done in 5.1s — 15 files updated, 27.1 MB sent, 1 removed\n"
	if stdout != want {
		t.Fatalf("deploy output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestDeployCommandDeleteConfirmationDeclined(t *testing.T) {
	var stdin bytes.Buffer
	stdin.WriteString("n\n")

	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "deploy" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newDeployCommand(&stdout, &stderr, func(_ context.Context, _ rootOptions, req deployRequest) (deploy.Result, error) {
		if !req.Delete {
			t.Fatal("expected delete flag")
		}
		if !req.DryRun {
			t.Fatal("should not deploy after declined confirmation")
		}
		return sampleDeployResult(true, false, false), nil
	}))
	root.SetArgs([]string{"deploy", "--delete"})
	root.SetIn(&stdin)

	err := root.Execute()
	if err == nil {
		t.Fatal("execute err = nil, want cancellation error")
	}
	if !strings.Contains(stderr.String(), "Remove 1 remote file(s)") {
		t.Fatalf("stderr = %q, want delete confirmation prompt", stderr.String())
	}
}

func TestDeployCommandJSON(t *testing.T) {
	stdout, _, err := execDeployCommand(t, []string{"--json", "status"},
		func(_ context.Context, _ rootOptions, req deployRequest) (deploy.Result, error) {
			if !req.DryRun {
				t.Fatal("status should dry-run")
			}
			return sampleDeployResult(true, false, false), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": {
    "site": "blog.example.com",
    "url": "https://blog.example.com",
    "server": "example.com",
    "build_dir": "./public",
    "remote_path": "/srv/site",
    "plan": {
      "new": 12,
      "changed": 3,
      "unchanged": 184,
      "remove": 1,
      "bytes_to_send": 28400000
    },
    "dry_run": true,
    "up_to_date": false,
    "deployed": false,
    "duration_seconds": 5.1
  }
}
`
	if stdout != want {
		t.Fatalf("json output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestDeployCommandErrorPlain(t *testing.T) {
	stdout, stderr, err := execDeployCommand(t, []string{"deploy"},
		func(context.Context, rootOptions, deployRequest) (deploy.Result, error) {
			return deploy.Result{}, errors.New("ssh unreachable")
		})
	if err == nil {
		t.Fatal("execute err = nil, want error")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty on error", stdout)
	}
	if !strings.Contains(stderr, "ssh unreachable") {
		t.Fatalf("stderr = %q, want the error message", stderr)
	}
}

func TestDeployCommandPassesSiteName(t *testing.T) {
	var sites []string
	_, _, err := execDeployCommand(t, []string{"status", "live"},
		func(_ context.Context, _ rootOptions, req deployRequest) (deploy.Result, error) {
			sites = append(sites, req.Site)
			return sampleDeployResult(true, false, false), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(sites) != 1 || sites[0] != "live" {
		t.Fatalf("site passed = %v, want [live]", sites)
	}
}

func TestDeployCommandWarnsOnStderr(t *testing.T) {
	stdout, stderr, err := execDeployCommand(t, []string{"status"},
		func(_ context.Context, _ rootOptions, req deployRequest) (deploy.Result, error) {
			req.Warn("build: . deploys the whole project folder")
			return sampleDeployResult(true, false, false), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(stderr, "warning: build: . deploys") || strings.Contains(stdout, "warning") {
		t.Fatalf("warning should go to stderr only:\nstdout %q\nstderr %q", stdout, stderr)
	}
}

func TestProgressLineRendersBar(t *testing.T) {
	var buf bytes.Buffer
	p := &progressLine{w: &buf}
	p.update(remote.RsyncProgress{Line: "img/cover.png", Percent: 0})
	if buf.Len() != 0 {
		t.Fatalf("file-name lines should not draw: %q", buf.String())
	}
	p.update(remote.RsyncProgress{Line: "18.10M  50%    6.20MB/s    0:00:02 (xfr#3, to-chk=10/200)", Percent: 50})
	p.finish()
	out := buf.String()
	if !strings.Contains(out, "Sending ████████████░░░░░░░░░░░░  50%  18.10M  6.20MB/s") || !strings.HasSuffix(out, "\r\033[K") {
		t.Fatalf("progress = %q", out)
	}
}
