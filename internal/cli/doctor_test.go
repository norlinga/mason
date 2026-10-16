package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/setup"
)

func runDoctorCommand(t *testing.T, args []string, run doctorRunFunc) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "doctor" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newDoctorCommand(&stdout, &stderr, run))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func healthyReport() setup.DoctorReport {
	return setup.DoctorReport{Checks: []setup.DoctorCheck{
		{Name: "ssh connection", Status: setup.CheckOK, Detail: "reachable"},
		{Name: "rsync available", Status: setup.CheckOK, Detail: "/usr/bin/rsync"},
		{Name: ".mason private", Status: setup.CheckOK, Detail: "mode 700"},
	}}
}

func TestDoctorCommandPlainHealthy(t *testing.T) {
	stdout, _, err := runDoctorCommand(t, []string{"doctor"},
		func(context.Context, rootOptions, string) (setup.DoctorReport, error) {
			return healthyReport(), nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Columns are width-aligned; assert on content and the header rather than spacing.
	for _, line := range []string{"WHAT", "CHECK", "STATUS", "DETAIL", "ssh connection", "rsync available", ".mason private"} {
		if !strings.Contains(stdout, line) {
			t.Fatalf("plain output missing %q:\n%s", line, stdout)
		}
	}
}

func TestDoctorCommandUnhealthyExitsNonZero(t *testing.T) {
	stdout, _, err := runDoctorCommand(t, []string{"doctor"},
		func(context.Context, rootOptions, string) (setup.DoctorReport, error) {
			return setup.DoctorReport{Checks: []setup.DoctorCheck{
				{Name: "rsync available", Status: setup.CheckFail, Detail: "not found"},
			}}, nil
		})
	if !errors.Is(err, errDoctorUnhealthy) {
		t.Fatalf("err = %v, want errDoctorUnhealthy for a failing check", err)
	}
	if !strings.Contains(stdout, "rsync available") {
		t.Fatalf("report should still render before exit: %q", stdout)
	}
}

func TestDoctorCommandWarnStaysHealthy(t *testing.T) {
	_, _, err := runDoctorCommand(t, []string{"doctor"},
		func(context.Context, rootOptions, string) (setup.DoctorReport, error) {
			return setup.DoctorReport{Checks: []setup.DoctorCheck{
				{Name: ".mason private", Status: setup.CheckWarn, Detail: "mode 755"},
			}}, nil
		})
	if err != nil {
		t.Fatalf("warnings should exit zero, got err = %v", err)
	}
}

func TestDoctorCommandJSON(t *testing.T) {
	stdout, _, err := runDoctorCommand(t, []string{"--json", "doctor"},
		func(context.Context, rootOptions, string) (setup.DoctorReport, error) {
			return setup.DoctorReport{Checks: []setup.DoctorCheck{
				{Group: "server example.com", Name: "ssh connection", Status: setup.CheckOK, Detail: "reachable"},
			}}, nil
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": {
    "healthy": true,
    "checks": [
      {
        "group": "server example.com",
        "name": "ssh connection",
        "status": "ok",
        "detail": "reachable"
      }
    ]
  }
}
`
	if stdout != want {
		t.Fatalf("json output:\n got: %q\nwant: %q", stdout, want)
	}
}

func TestDoctorCommandJSONUnhealthy(t *testing.T) {
	stdout, _, err := runDoctorCommand(t, []string{"--json", "doctor"},
		func(context.Context, rootOptions, string) (setup.DoctorReport, error) {
			return setup.DoctorReport{Checks: []setup.DoctorCheck{
				{Name: "rsync available", Status: setup.CheckFail, Detail: "not found"},
			}}, nil
		})
	if !errors.Is(err, errDoctorUnhealthy) {
		t.Fatalf("err = %v, want errDoctorUnhealthy", err)
	}
	if !strings.Contains(stdout, `"healthy": false`) {
		t.Fatalf("json output missing healthy false: %q", stdout)
	}
	if !strings.Contains(stdout, `"status": "fail"`) {
		t.Fatalf("json output missing fail status: %q", stdout)
	}
}

func TestDoctorCommandRunError(t *testing.T) {
	_, stderr, err := runDoctorCommand(t, []string{"doctor"},
		func(context.Context, rootOptions, string) (setup.DoctorReport, error) {
			return setup.DoctorReport{}, errors.New("no .mason config found")
		})
	if err == nil {
		t.Fatal("execute err = nil, want config error")
	}
	if !strings.Contains(stderr, "no .mason config found") {
		t.Fatalf("stderr = %q", stderr)
	}
}
