package cli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/norlinga/mason/internal/output"
	"github.com/norlinga/mason/internal/remote"
	"github.com/norlinga/mason/internal/setup"
	"github.com/spf13/cobra"
)

// errDoctorUnhealthy is returned (after the report is rendered) so the process exits
// non-zero when a check failed. The root command silences errors, so it is not printed.
var errDoctorUnhealthy = errors.New("doctor: one or more checks failed")

// doctorRunFunc produces a diagnostic report. It is the seam that lets output be
// tested without real SSH or HTTP.
type doctorRunFunc func(ctx context.Context, root rootOptions) (setup.DoctorReport, error)

type doctorCheckJSON struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type doctorReportJSON struct {
	Healthy bool              `json:"healthy"`
	Checks  []doctorCheckJSON `json:"checks"`
}

func newDoctorCommand(stdout, stderr io.Writer, run doctorRunFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose a target: SSH, perms, manifest, private prefixes, Caddy",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			report, err := run(cmd.Context(), root)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			if rerr := renderDoctor(stdout, opts, report); rerr != nil {
				return emitError(stdout, stderr, opts, rerr)
			}
			if !report.Healthy() {
				return errDoctorUnhealthy
			}
			return nil
		},
	}
}

func renderDoctor(stdout io.Writer, opts output.Options, report setup.DoctorReport) error {
	if opts.JSON {
		checks := make([]doctorCheckJSON, 0, len(report.Checks))
		for _, c := range report.Checks {
			checks = append(checks, doctorCheckJSON{Name: c.Name, Status: string(c.Status), Detail: c.Detail})
		}
		return output.WriteJSON(stdout, output.Success(doctorReportJSON{
			Healthy: report.Healthy(),
			Checks:  checks,
		}))
	}

	headers := []string{"CHECK", "STATUS", "DETAIL"}
	grid := make([][]string, 0, len(report.Checks))
	for _, c := range report.Checks {
		grid = append(grid, []string{c.Name, string(c.Status), orDash(c.Detail)})
	}
	return renderTable(stdout, headers, grid)
}

// defaultDoctorRun wires the real SSH runner, manifest store, and HTTP probe.
func defaultDoctorRun(ctx context.Context, root rootOptions) (setup.DoctorReport, error) {
	resolved, err := resolveTarget(root)
	if err != nil {
		return setup.DoctorReport{}, err
	}
	target := resolved.Target
	ssh := remote.NewSSH(remote.SSHOptionsFromTarget(target))
	fs := remote.NewSSHFS(ssh)
	store := remote.NewRemoteManifestStore(target.Root, fs, ssh)
	deps := setup.DoctorDeps{Runner: ssh, Store: store, HTTPGet: httpStatus}
	return setup.Diagnose(ctx, deps, target), nil
}

// httpStatus performs a GET and returns only the status code.
func httpStatus(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}
