// Package deploy orchestrates build-directory rsync deploys. The planner diffs local
// build_dir against the remote deploy path without transferring bytes.
package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/norlinga/mason/internal/config"
	"github.com/norlinga/mason/internal/remote"
)

// Runner compares local and remote trees through rsync dry-run.
type Runner interface {
	CompareDryRun(ctx context.Context, request remote.CompareDryRunRequest) (remote.ProcessResult, error)
}

// Planner builds deploy plans from configured targets.
type Planner struct {
	target config.Target
	runner Runner
}

// PlanOptions configures one planning run.
type PlanOptions struct {
	// BaseDir resolves relative deploy.build_dir paths; empty means the current directory.
	BaseDir string
	Delete  bool
}

// Plan summarizes what a deploy would change.
type Plan struct {
	BuildDir    string
	RemotePath  string
	New         int
	Changed     int
	Unchanged   int
	Remove      int
	BytesToSend int64
}

// NewPlanner builds a deploy planner for a target.
func NewPlanner(target config.Target, runner Runner) *Planner {
	return &Planner{target: target, runner: runner}
}

// Plan diffs local build_dir against the remote deploy path using rsync dry-run.
func (p *Planner) Plan(ctx context.Context, opts PlanOptions) (Plan, error) {
	buildDir := strings.TrimSpace(p.target.Deploy.BuildDir)
	remotePath := strings.TrimSpace(p.target.Deploy.RemotePath)
	if buildDir == "" {
		return Plan{}, fmt.Errorf("deploy.build_dir is required")
	}
	if remotePath == "" {
		return Plan{}, fmt.Errorf("deploy.remote_path is required")
	}

	source, err := resolveBuildDir(buildDir, opts.BaseDir)
	if err != nil {
		return Plan{}, err
	}

	result, err := p.runner.CompareDryRun(ctx, remote.CompareDryRunRequest{
		Source:      source,
		Destination: deployDestination(p.target, remotePath),
		Delete:      opts.Delete,
		SSH:         sshOptions(p.target),
	})
	if err != nil {
		return Plan{}, fmt.Errorf("compare deploy tree: %w", err)
	}

	stats, err := ParseCompareOutput(result.Stdout + "\n" + result.Stderr)
	if err != nil {
		return Plan{}, fmt.Errorf("parse rsync plan: %w", err)
	}

	return Plan{
		BuildDir:    buildDir,
		RemotePath:  remotePath,
		New:         stats.New,
		Changed:     stats.Changed,
		Unchanged:   stats.Unchanged,
		Remove:      stats.Remove,
		BytesToSend: stats.BytesToSend,
	}, nil
}

type compareStats struct {
	New         int
	Changed     int
	Unchanged   int
	Remove      int
	BytesToSend int64
}

var (
	createdFilesRE     = regexp.MustCompile(`^Number of created files:\s*(\d+)(?:\s*\(reg:\s*(\d+)\))?`)
	deletedFilesRE     = regexp.MustCompile(`^Number of deleted files:\s*(\d+)(?:\s*\(reg:\s*(\d+)\))?`)
	transferredFilesRE = regexp.MustCompile(`^Number of regular files transferred:\s*(\d+)`)
	fileListRE         = regexp.MustCompile(`^Number of files:\s*(\d+)(?:\s*\(reg:\s*(\d+)(?:,\s*dir:\s*(\d+))?\))?`)
	transferredSizeRE  = regexp.MustCompile(`^Total transferred file size:\s*([0-9,]+)\s*bytes`)
)

// ParseCompareOutput derives deploy plan counts from rsync dry-run stats output.
func ParseCompareOutput(output string) (compareStats, error) {
	var stats compareStats
	var regularFiles int
	var transferred int
	var sawStats bool

	for line := range strings.Lines(output) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		switch {
		case createdFilesRE.MatchString(line):
			sawStats = true
			stats.New = countFromStatLine(line, createdFilesRE)
		case deletedFilesRE.MatchString(line):
			sawStats = true
			stats.Remove = countFromStatLine(line, deletedFilesRE)
		case transferredFilesRE.MatchString(line):
			sawStats = true
			transferred = atoiFirst(transferredFilesRE.FindStringSubmatch(line))
		case fileListRE.MatchString(line):
			sawStats = true
			regularFiles = regCountFromFileList(line)
		case transferredSizeRE.MatchString(line):
			sawStats = true
			stats.BytesToSend = bytesFromStatLine(line)
		}
	}

	if !sawStats {
		return compareStats{}, fmt.Errorf("rsync stats output missing")
	}

	stats.Changed = transferred - stats.New
	if stats.Changed < 0 {
		stats.Changed = 0
	}
	if regularFiles > transferred {
		stats.Unchanged = regularFiles - transferred
	}

	return stats, nil
}

func resolveBuildDir(buildDir, baseDir string) (string, error) {
	if strings.TrimSpace(baseDir) == "" {
		baseDir = "."
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolve base directory: %w", err)
	}

	path := buildDir
	if !filepath.IsAbs(path) {
		path = filepath.Join(absBase, path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve build directory: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("stat build directory %q: %w", buildDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("deploy.build_dir %q is not a directory", buildDir)
	}

	return ensureTrailingSlash(absPath), nil
}

func deployDestination(target config.Target, remotePath string) string {
	clean := strings.TrimSuffix(strings.TrimSpace(remotePath), "/") + "/"
	return remote.RsyncDestinationFromTarget(target, clean)
}

func sshOptions(target config.Target) *remote.SSHOptions {
	opts := remote.SSHOptionsFromTarget(target)
	return &opts
}

func ensureTrailingSlash(path string) string {
	if strings.HasSuffix(path, string(os.PathSeparator)) {
		return path
	}
	return path + string(os.PathSeparator)
}

func countFromStatLine(line string, re *regexp.Regexp) int {
	matches := re.FindStringSubmatch(line)
	if len(matches) == 0 {
		return 0
	}
	if len(matches) > 2 && matches[2] != "" {
		return atoiFirst(matches[2:])
	}
	return atoiFirst(matches[1:])
}

func regCountFromFileList(line string) int {
	matches := fileListRE.FindStringSubmatch(line)
	if len(matches) > 2 && matches[2] != "" {
		return atoiFirst(matches[2:])
	}
	return atoiFirst(matches[1:])
}

func bytesFromStatLine(line string) int64 {
	matches := transferredSizeRE.FindStringSubmatch(line)
	if len(matches) < 2 {
		return 0
	}
	clean := strings.ReplaceAll(matches[1], ",", "")
	value, err := strconv.ParseInt(clean, 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func atoiFirst(values []string) int {
	for _, value := range values {
		if value == "" {
			continue
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			continue
		}
		return n
	}
	return 0
}
