package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/norlinga/mason/internal/config"
	"github.com/norlinga/mason/internal/deploy"
	"github.com/norlinga/mason/internal/output"
	"github.com/norlinga/mason/internal/remote"
	"github.com/spf13/cobra"
)

// deployRequest is one pass of a deploy: a plan (Plan == nil) or the transfer of a
// precomputed plan.
type deployRequest struct {
	Site       string
	Delete     bool
	DryRun     bool
	Plan       *deploy.Plan
	OnProgress func(remote.RsyncProgress)
	// Warn receives advisory messages (e.g. deploying the whole project folder).
	Warn func(string)
}

// deployRunFunc plans or executes a deploy. It is the seam that lets output be tested
// without SSH or rsync.
type deployRunFunc func(ctx context.Context, root rootOptions, req deployRequest) (deploy.Result, error)

type deployPlanJSON struct {
	New         int   `json:"new"`
	Changed     int   `json:"changed"`
	Unchanged   int   `json:"unchanged"`
	Remove      int   `json:"remove"`
	BytesToSend int64 `json:"bytes_to_send"`
}

type deployJSON struct {
	Site            string         `json:"site"`
	URL             string         `json:"url"`
	Server          string         `json:"server"`
	BuildDir        string         `json:"build_dir"`
	RemotePath      string         `json:"remote_path"`
	Plan            deployPlanJSON `json:"plan"`
	DryRun          bool           `json:"dry_run"`
	UpToDate        bool           `json:"up_to_date"`
	Deployed        bool           `json:"deployed"`
	DurationSeconds float64        `json:"duration_seconds"`
}

func newDeployCommand(stdout, stderr io.Writer, run deployRunFunc) *cobra.Command {
	var del, dryRun bool
	cmd := &cobra.Command{
		Use:   "deploy [site]",
		Short: "Deploy the project's build folder to its site",
		Long: "Sync the project's build folder to the site's path on its server: plan, transfer,\n" +
			"summary. [site] picks one of the project's sites (by name or host); the first one\n" +
			"listed is the default. Dotfiles are never deployed, except /.well-known/.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			req := deployRequest{Delete: del, DryRun: dryRun}
			if len(args) == 1 {
				req.Site = args[0]
			}
			return runDeployCommand(cmd.Context(), stdout, stderr, cmd.InOrStdin(), outputOptions(root), root, req, run)
		},
	}
	cmd.Flags().BoolVar(&del, "delete", false, "remove remote files absent from the local build folder (asks first)")
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "show the plan without transferring (same as status)")
	return cmd
}

func newStatusCommand(stdout, stderr io.Writer, run deployRunFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "status [site]",
		Short: "Show what a deploy would change, without transferring",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			req := deployRequest{DryRun: true}
			if len(args) == 1 {
				req.Site = args[0]
			}
			return runDeployCommand(cmd.Context(), stdout, stderr, cmd.InOrStdin(), outputOptions(root), root, req, run)
		},
	}
}

func runDeployCommand(ctx context.Context, stdout, stderr io.Writer, stdin io.Reader, opts output.Options, root rootOptions, req deployRequest, run deployRunFunc) error {
	if !opts.Quiet && !opts.JSON {
		req.Warn = func(msg string) { fmt.Fprintln(stderr, "warning: "+msg) }
	}

	planReq := req
	planReq.DryRun = true
	planReq.Plan = nil
	planRes, err := run(ctx, root, planReq)
	if err != nil {
		return emitError(stdout, stderr, opts, err)
	}

	if req.DryRun {
		return renderDeploy(stdout, opts, planRes)
	}

	if !planRes.UpToDate && req.Delete && planRes.Plan.Remove > 0 && !root.yes {
		ok, err := confirmDelete(stderr, stdin, planRes.Plan.Remove)
		if err != nil {
			return emitError(stdout, stderr, opts, err)
		}
		if !ok {
			return emitError(stdout, stderr, opts, fmt.Errorf("deploy cancelled"))
		}
	}

	if planRes.UpToDate {
		planRes.DryRun = false
		return renderDeploy(stdout, opts, planRes)
	}

	if !opts.JSON && !opts.Quiet {
		writeDeployHeader(stdout, planRes)
	}

	execReq := req
	execReq.DryRun = false
	execReq.Plan = &planRes.Plan
	var bar *progressLine
	switch {
	case opts.JSON || opts.Quiet:
	case opts.Verbose:
		execReq.OnProgress = func(p remote.RsyncProgress) { fmt.Fprintln(stderr, p.Line) }
	case isTerminal(stderr):
		bar = &progressLine{w: stderr}
		execReq.OnProgress = bar.update
	default:
		fmt.Fprintln(stdout, "Sending …")
	}
	execRes, err := run(ctx, root, execReq)
	bar.finish()
	if err != nil {
		return emitError(stdout, stderr, opts, err)
	}
	execRes.Plan = planRes.Plan
	return renderDeploySummary(stdout, opts, execRes)
}

func writeDeployHeader(w io.Writer, res deploy.Result) {
	fmt.Fprintf(w, "Site:  %s → %s:%s\n", res.URL, res.Server, res.RemotePath)
	fmt.Fprintf(w, "Build: %s\n", res.BuildDir)
	fmt.Fprintln(w, formatPlanLine(res.Plan))
}

func deployResultJSON(res deploy.Result, dryRun bool) deployJSON {
	return deployJSON{
		Site:       res.Site,
		URL:        res.URL,
		Server:     res.Server,
		BuildDir:   res.BuildDir,
		RemotePath: res.RemotePath,
		Plan: deployPlanJSON{
			New:         res.Plan.New,
			Changed:     res.Plan.Changed,
			Unchanged:   res.Plan.Unchanged,
			Remove:      res.Plan.Remove,
			BytesToSend: res.Plan.BytesToSend,
		},
		DryRun:          dryRun,
		UpToDate:        res.UpToDate,
		Deployed:        res.Deployed,
		DurationSeconds: res.Duration.Seconds(),
	}
}

func renderDeploySummary(stdout io.Writer, opts output.Options, res deploy.Result) error {
	if opts.JSON {
		return output.WriteJSON(stdout, output.Success(deployResultJSON(res, false)))
	}
	if opts.Quiet {
		return nil
	}
	fmt.Fprintln(stdout, doneLine(res))
	return nil
}

func renderDeploy(stdout io.Writer, opts output.Options, res deploy.Result) error {
	if opts.JSON {
		return output.WriteJSON(stdout, output.Success(deployResultJSON(res, res.DryRun)))
	}
	if opts.Quiet {
		return nil
	}
	writeDeployHeader(stdout, res)
	if res.DryRun {
		return nil
	}
	if res.UpToDate {
		fmt.Fprintf(stdout, "✓ Already up to date (%d files, 0 B sent)\n", deploy.TotalFiles(res.Plan))
		return nil
	}
	if res.Deployed {
		fmt.Fprintln(stdout, doneLine(res))
	}
	return nil
}

func doneLine(res deploy.Result) string {
	line := fmt.Sprintf("✓ Done in %.1fs — %d files updated, %s sent",
		res.Duration.Seconds(), deploy.FilesUpdated(res.Plan), output.HumanSize(res.Plan.BytesToSend))
	if res.Plan.Remove > 0 {
		line += fmt.Sprintf(", %d removed", res.Plan.Remove)
	}
	return line
}

func formatPlanLine(plan deploy.Plan) string {
	line := fmt.Sprintf("Plan:  %d new · %d changed · %d unchanged", plan.New, plan.Changed, plan.Unchanged)
	if plan.Remove > 0 {
		line += fmt.Sprintf(" · %d to remove", plan.Remove)
	}
	if plan.BytesToSend > 0 {
		line += fmt.Sprintf("   (%s to send)", output.HumanSize(plan.BytesToSend))
	}
	return line
}

func confirmDelete(stderr io.Writer, stdin io.Reader, removeCount int) (bool, error) {
	fmt.Fprintf(stderr, "Remove %d remote file(s) not present locally? [y/N] ", removeCount)
	scanner := bufio.NewScanner(stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, err
		}
		return false, nil
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}

func defaultDeployRun(ctx context.Context, root rootOptions, req deployRequest) (deploy.Result, error) {
	cfg, err := loadConfig(root)
	if err != nil {
		return deploy.Result{}, err
	}
	site, err := cfg.Site(req.Site)
	if err != nil {
		return deploy.Result{}, err
	}
	// The build folder is relative to the project file, not to wherever mason runs.
	baseDir := filepath.Dir(cfg.ProjectFile)

	if req.Plan == nil {
		if strings.TrimSuffix(strings.TrimSpace(site.Build), "/") == "." && req.Warn != nil {
			req.Warn("build: " + site.Build + " deploys the whole project folder — set build: to your generator's output folder (dotfiles are skipped either way)")
		}
		if err := checkProvisioned(ctx, site); err != nil {
			return deploy.Result{}, err
		}
	}

	svc := deploy.NewService(site, remote.NewRsync())
	opts := deploy.DeployOptions{
		BaseDir:    baseDir,
		Delete:     req.Delete,
		DryRun:     req.DryRun,
		OnProgress: req.OnProgress,
	}
	// Reuse the plan computed by the dry-run pass instead of scanning the remote again.
	if req.Plan != nil && !req.DryRun {
		return svc.Execute(ctx, opts, *req.Plan)
	}
	return svc.Run(ctx, opts)
}

// checkProvisioned fails early, with the fix, when the site's path is missing or not
// writable on its server.
func checkProvisioned(ctx context.Context, site config.Site) error {
	ssh := remote.NewSSH(remote.SSHOptionsFromServer(*site.Server))
	q := shellQuote(site.Path)
	res, err := ssh.Run(ctx, fmt.Sprintf("if [ ! -d %s ]; then echo missing; elif [ ! -w %s ]; then echo readonly; fi", q, q))
	if err != nil {
		return err
	}
	switch strings.TrimSpace(res.Stdout) {
	case "missing":
		return fmt.Errorf("%s is not provisioned on %s (%s does not exist) — run: mason provision", site.URL, site.Server.Host, site.Path)
	case "readonly":
		return fmt.Errorf("%s:%s is not writable by %s — run: mason provision (as an admin) to fix ownership", site.Server.Host, site.Path, site.Server.User)
	}
	return nil
}

// progressLine renders rsync --info=progress2 updates as one in-place terminal line.
type progressLine struct {
	w     io.Writer
	drawn bool
}

func (p *progressLine) update(ev remote.RsyncProgress) {
	if ev.Percent == 0 && !strings.Contains(ev.Line, "%") {
		return // per-file names from --out-format; the bar carries the totals
	}
	fields := strings.Fields(ev.Line)
	sent, rate := "", ""
	if len(fields) >= 3 {
		sent, rate = fields[0], fields[2]
	}
	const width = 24
	filled := ev.Percent * width / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	fmt.Fprintf(p.w, "\r\033[KSending %s %3d%%  %s  %s", bar, ev.Percent, sent, rate)
	p.drawn = true
}

func (p *progressLine) finish() {
	if p != nil && p.drawn {
		fmt.Fprint(p.w, "\r\033[K")
	}
}

// isTerminal reports whether w is an interactive terminal.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
