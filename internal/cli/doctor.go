package cli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/norlinga/mason/internal/config"
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
type doctorRunFunc func(ctx context.Context, root rootOptions, site string) (setup.DoctorReport, error)

type doctorCheckJSON struct {
	Group  string `json:"group"`
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
		Use:   "doctor [site]",
		Short: "Check your server, share store, and (in a project) site",
		Long: "Read-only checks for everything configured: the server (SSH, rsync, Caddy import),\n" +
			"the share store if ~/.mason has one, and the current project's site (web root,\n" +
			"Mason-managed vhost, Caddy, HTTP). Exits non-zero if any check fails.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			site := ""
			if len(args) == 1 {
				site = args[0]
			}
			report, err := run(cmd.Context(), root, site)
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
			checks = append(checks, doctorCheckJSON{Group: c.Group, Name: c.Name, Status: string(c.Status), Detail: c.Detail})
		}
		return output.WriteJSON(stdout, output.Success(doctorReportJSON{
			Healthy: report.Healthy(),
			Checks:  checks,
		}))
	}

	headers := []string{"WHAT", "CHECK", "STATUS", "DETAIL"}
	grid := make([][]string, 0, len(report.Checks))
	for _, c := range report.Checks {
		grid = append(grid, []string{c.Group, c.Name, string(c.Status), orDash(c.Detail)})
	}
	return renderTable(stdout, headers, grid)
}

// defaultDoctorRun builds the scope from config — the server, the share store if
// configured, and the project's site if run inside a project — and wires real SSH/HTTP.
func defaultDoctorRun(ctx context.Context, root rootOptions, siteName string) (setup.DoctorReport, error) {
	cfg, err := loadConfig(root)
	if err != nil {
		return setup.DoctorReport{}, err
	}
	scope := setup.DoctorScope{Server: cfg.Server}
	deps := setup.DoctorDeps{
		RunnerFor: func(s config.Server) remote.CommandRunner { return remote.NewSSH(remote.SSHOptionsFromServer(s)) },
		HTTPGet:   httpStatus,
	}
	if share, err := cfg.RequireShare(); err == nil {
		scope.Share = &share
		ssh := remote.NewSSH(remote.SSHOptionsFromServer(*share.Server))
		deps.Store = remote.NewRemoteManifestStore(share.Path, remote.NewSSHFS(ssh), ssh)
	}
	if cfg.ProjectFile != "" || siteName != "" {
		site, err := cfg.Site(siteName)
		if err != nil {
			return setup.DoctorReport{}, err
		}
		scope.Site = &site
	}
	if scope.Server == nil && scope.Share == nil && scope.Site == nil {
		return setup.DoctorReport{}, errors.New("nothing to check yet — run 'mason setup <host>' first")
	}
	return setup.Diagnose(ctx, deps, scope), nil
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
