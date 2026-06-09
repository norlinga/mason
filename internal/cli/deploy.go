package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/norlinga/mason/internal/deploy"
	"github.com/norlinga/mason/internal/output"
	"github.com/norlinga/mason/internal/remote"
	"github.com/spf13/cobra"
)

// deployRunFunc plans (plan == nil) or executes a precomputed plan (plan != nil) for a deploy.
type deployRunFunc func(ctx context.Context, root rootOptions, delete bool, plan *deploy.Plan, onProgress func(remote.RsyncProgress)) (deploy.Result, error)

type deployPlanJSON struct {
	New         int   `json:"new"`
	Changed     int   `json:"changed"`
	Unchanged   int   `json:"unchanged"`
	Remove      int   `json:"remove"`
	BytesToSend int64 `json:"bytes_to_send"`
}

type deployJSON struct {
	Target          string         `json:"target"`
	Host            string         `json:"host"`
	BuildDir        string         `json:"build_dir"`
	RemotePath      string         `json:"remote_path"`
	Plan            deployPlanJSON `json:"plan"`
	DryRun          bool           `json:"dry_run"`
	UpToDate        bool           `json:"up_to_date"`
	Deployed        bool           `json:"deployed"`
	DurationSeconds float64        `json:"duration_seconds"`
}

func newDeployCommand(stdout, stderr io.Writer, run deployRunFunc) *cobra.Command {
	var delete bool
	cmd := &cobra.Command{
		Use:   "deploy [target]",
		Short: "Deploy a build directory",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			if len(args) == 1 {
				root.target = args[0]
			}
			opts := outputOptions(root)
			return runDeployCommand(cmd.Context(), stdout, stderr, opts, root, delete, run)
		},
	}
	cmd.Flags().BoolVar(&delete, "delete", false, "remove remote files absent from the local build tree")
	return cmd
}

func newStatusCommand(stdout, stderr io.Writer, run deployRunFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "status [target]",
		Short: "Preview a deploy without transferring files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			root.dryRun = true
			if len(args) == 1 {
				root.target = args[0]
			}
			opts := outputOptions(root)
			return runDeployCommand(cmd.Context(), stdout, stderr, opts, root, false, run)
		},
	}
}

func runDeployCommand(ctx context.Context, stdout, stderr io.Writer, opts output.Options, root rootOptions, delete bool, run deployRunFunc) error {
	planRoot := root
	planRoot.dryRun = true
	planRes, err := run(ctx, planRoot, delete, nil, nil)
	if err != nil {
		return emitError(stdout, stderr, opts, err)
	}

	if root.dryRun {
		return renderDeploy(stdout, opts, planRes, false)
	}

	if !planRes.UpToDate && delete && planRes.Plan.Remove > 0 && !root.yes {
		ok, err := confirmDelete(stderr, os.Stdin, planRes.Plan.Remove)
		if err != nil {
			return emitError(stdout, stderr, opts, err)
		}
		if !ok {
			return emitError(stdout, stderr, opts, fmt.Errorf("deploy cancelled"))
		}
	}

	var progress func(remote.RsyncProgress)
	if !opts.Quiet && !root.dryRun && opts.Verbose {
		progress = func(p remote.RsyncProgress) {
			if strings.TrimSpace(p.Line) != "" {
				fmt.Fprintln(stderr, p.Line)
			}
		}
	}

	if planRes.UpToDate {
		planRes.DryRun = false
		return renderDeploy(stdout, opts, planRes, false)
	}

	fmt.Fprintf(stdout, "Target: %s (%s:%s)\n", planRes.TargetName, planRes.Host, planRes.RemotePath)
	fmt.Fprintf(stdout, "Comparing local %s ↔ remote …  done\n", planRes.BuildDir)
	fmt.Fprintln(stdout, formatPlanLine(planRes.Plan))
	if !opts.Quiet && !opts.Verbose {
		fmt.Fprintln(stdout, "Sending …")
	}

	execRoot := root
	execRoot.dryRun = false
	execRes, err := run(ctx, execRoot, delete, &planRes.Plan, progress)
	if err != nil {
		return emitError(stdout, stderr, opts, err)
	}
	execRes.Plan = planRes.Plan

	return renderDeploySummary(stdout, opts, execRes)
}

func renderDeploySummary(stdout io.Writer, opts output.Options, res deploy.Result) error {
	if opts.JSON {
		return output.WriteJSON(stdout, output.Success(deployJSON{
			Target:     res.TargetName,
			Host:       res.Host,
			BuildDir:   res.BuildDir,
			RemotePath: res.RemotePath,
			Plan: deployPlanJSON{
				New:         res.Plan.New,
				Changed:     res.Plan.Changed,
				Unchanged:   res.Plan.Unchanged,
				Remove:      res.Plan.Remove,
				BytesToSend: res.Plan.BytesToSend,
			},
			DryRun:          false,
			UpToDate:        res.UpToDate,
			Deployed:        res.Deployed,
			DurationSeconds: res.Duration.Seconds(),
		}))
	}
	if opts.Quiet {
		return nil
	}

	updated := deploy.FilesUpdated(res.Plan)
	line := fmt.Sprintf("✓ Done in %.1fs — %d files updated, %s sent",
		res.Duration.Seconds(), updated, output.HumanSize(res.Plan.BytesToSend))
	if res.Plan.Remove > 0 {
		line += fmt.Sprintf(", %d removed", res.Plan.Remove)
	}
	fmt.Fprintln(stdout, line)
	return nil
}

func renderDeploy(stdout io.Writer, opts output.Options, res deploy.Result, showSending bool) error {
	if opts.JSON {
		return output.WriteJSON(stdout, output.Success(deployJSON{
			Target:     res.TargetName,
			Host:       res.Host,
			BuildDir:   res.BuildDir,
			RemotePath: res.RemotePath,
			Plan: deployPlanJSON{
				New:         res.Plan.New,
				Changed:     res.Plan.Changed,
				Unchanged:   res.Plan.Unchanged,
				Remove:      res.Plan.Remove,
				BytesToSend: res.Plan.BytesToSend,
			},
			DryRun:          res.DryRun,
			UpToDate:        res.UpToDate,
			Deployed:        res.Deployed,
			DurationSeconds: res.Duration.Seconds(),
		}))
	}

	if opts.Quiet {
		return nil
	}

	fmt.Fprintf(stdout, "Target: %s (%s:%s)\n", res.TargetName, res.Host, res.RemotePath)
	fmt.Fprintf(stdout, "Comparing local %s ↔ remote …  done\n", res.BuildDir)
	fmt.Fprintln(stdout, formatPlanLine(res.Plan))

	if res.DryRun {
		return nil
	}

	if res.UpToDate {
		fmt.Fprintf(stdout, "✓ Already up to date (%d files, 0 B sent)\n", deploy.TotalFiles(res.Plan))
		return nil
	}

	if showSending && res.Deployed {
		fmt.Fprintln(stdout, "Sending …")
	}

	if res.Deployed {
		updated := deploy.FilesUpdated(res.Plan)
		line := fmt.Sprintf("✓ Done in %.1fs — %d files updated, %s sent",
			res.Duration.Seconds(), updated, output.HumanSize(res.Plan.BytesToSend))
		if res.Plan.Remove > 0 {
			line += fmt.Sprintf(", %d removed", res.Plan.Remove)
		}
		fmt.Fprintln(stdout, line)
	}

	return nil
}

func formatPlanLine(plan deploy.Plan) string {
	line := fmt.Sprintf("Plan: %d new · %d changed · %d unchanged", plan.New, plan.Changed, plan.Unchanged)
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

func defaultDeployRun(ctx context.Context, root rootOptions, delete bool, plan *deploy.Plan, onProgress func(remote.RsyncProgress)) (deploy.Result, error) {
	resolved, err := resolveTarget(root)
	if err != nil {
		return deploy.Result{}, err
	}
	stack, err := newRemoteStack(resolved)
	if err != nil {
		return deploy.Result{}, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return deploy.Result{}, err
	}
	svc := deploy.NewService(resolved.Name, stack.target, stack.rsync)
	opts := deploy.DeployOptions{
		BaseDir:    cwd,
		Delete:     delete,
		DryRun:     root.dryRun,
		OnProgress: onProgress,
	}
	// Reuse the plan computed by the dry-run pass instead of scanning the remote again.
	if plan != nil && !root.dryRun {
		return svc.Execute(ctx, opts, *plan)
	}
	return svc.Run(ctx, opts)
}
