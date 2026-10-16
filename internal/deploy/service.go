package deploy

import (
	"context"
	"fmt"
	"time"

	"github.com/norlinga/mason/internal/config"
	"github.com/norlinga/mason/internal/remote"
)

// Rsync compares and transfers deploy trees.
type Rsync interface {
	CompareDryRun(ctx context.Context, request remote.CompareDryRunRequest) (remote.ProcessResult, error)
	Upload(ctx context.Context, request remote.RsyncRequest) (remote.ProcessResult, error)
}

// Service orchestrates deploy planning and execution.
type Service struct {
	site  config.Site
	rsync Rsync
}

// DeployOptions configures one deploy run.
type DeployOptions struct {
	BaseDir    string
	Delete     bool
	DryRun     bool
	OnProgress func(remote.RsyncProgress)
}

// Result reports the outcome of a deploy run.
type Result struct {
	// Site is the site's name; URL its public address; Server its SSH destination.
	Site       string
	URL        string
	Server     string
	BuildDir   string
	RemotePath string
	Plan       Plan
	DryRun     bool
	UpToDate   bool
	Deployed   bool
	Duration   time.Duration
}

// NewService builds a deploy service for a resolved site.
func NewService(site config.Site, rsync Rsync) *Service {
	return &Service{site: site, rsync: rsync}
}

// Execute transfers files for a plan that is not already up to date.
func (s *Service) Execute(ctx context.Context, opts DeployOptions, plan Plan) (Result, error) {
	start := time.Now()
	result := Result{
		Site:       s.site.Name,
		URL:        s.site.URL,
		Server:     s.serverName(),
		BuildDir:   plan.BuildDir,
		RemotePath: plan.RemotePath,
		Plan:       plan,
		UpToDate:   UpToDate(plan),
	}

	if result.UpToDate {
		result.Duration = time.Since(start)
		return result, nil
	}

	source, err := resolveBuildDir(plan.BuildDir, opts.BaseDir)
	if err != nil {
		return Result{}, err
	}

	ssh := sshOptions(s.site)
	_, err = s.rsync.Upload(ctx, remote.RsyncRequest{
		Source:      source,
		Destination: deployDestination(s.site, plan.RemotePath),
		Delete:      opts.Delete,
		Filters:     DefaultFilters,
		SSH:         ssh,
		OnProgress:  opts.OnProgress,
	})
	if err != nil {
		return Result{}, fmt.Errorf("deploy: %w", err)
	}

	result.Deployed = true
	result.Duration = time.Since(start)
	return result, nil
}

// Run plans the deploy and optionally transfers files.
func (s *Service) Run(ctx context.Context, opts DeployOptions) (Result, error) {
	start := time.Now()
	plan, err := NewPlanner(s.site, s.rsync).Plan(ctx, PlanOptions{
		BaseDir: opts.BaseDir,
		Delete:  opts.Delete,
	})
	if err != nil {
		return Result{}, err
	}

	result := Result{
		Site:       s.site.Name,
		URL:        s.site.URL,
		Server:     s.serverName(),
		BuildDir:   plan.BuildDir,
		RemotePath: plan.RemotePath,
		Plan:       plan,
		DryRun:     opts.DryRun,
		UpToDate:   UpToDate(plan),
	}

	if opts.DryRun || result.UpToDate {
		result.Duration = time.Since(start)
		return result, nil
	}

	executed, err := s.Execute(ctx, opts, plan)
	if err != nil {
		return Result{}, err
	}
	executed.DryRun = false
	return executed, nil
}

// UpToDate reports whether a plan has nothing to transfer or remove.
func UpToDate(plan Plan) bool {
	return plan.New == 0 && plan.Changed == 0 && plan.Remove == 0
}

// FilesUpdated returns the number of files that would be created or changed.
func FilesUpdated(plan Plan) int {
	return plan.New + plan.Changed
}

// TotalFiles returns the number of regular files in the deploy tree per the plan.
func TotalFiles(plan Plan) int {
	return plan.Unchanged + plan.New + plan.Changed
}

func (s *Service) serverName() string {
	if s.site.Server == nil {
		return ""
	}
	return s.site.Server.Host
}
