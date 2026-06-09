package remote

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/norlinga/mason/internal/config"
)

const defaultRsyncBinary = "rsync"

// Rsync uploads files and directories through the rsync binary.
type Rsync struct {
	binary string
	runner ProcessRunner
}

// RsyncRequest describes one rsync transfer.
type RsyncRequest struct {
	Source      string
	Destination string
	Delete      bool
	DryRun      bool
	SSH         *SSHOptions
	OnProgress  func(RsyncProgress)
}

// CompareDryRunRequest describes a directory diff dry-run for deploy planning.
type CompareDryRunRequest struct {
	Source      string
	Destination string
	Delete      bool
	SSH         *SSHOptions
}

// RsyncProgress is a parsed progress event from rsync output.
type RsyncProgress struct {
	Line    string
	Percent int
}

// ProcessResult captures subprocess output.
type ProcessResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// ProcessRunner runs local subprocesses. It is injectable for tests.
type ProcessRunner interface {
	Run(ctx context.Context, name string, args []string, onOutput func(string)) (ProcessResult, error)
}

// NewRsync creates a real rsync runner.
func NewRsync() *Rsync {
	return NewRsyncWithRunner(defaultRsyncBinary, execProcessRunner{})
}

// NewRsyncWithRunner creates an rsync runner with an injected subprocess runner.
func NewRsyncWithRunner(binary string, runner ProcessRunner) *Rsync {
	if strings.TrimSpace(binary) == "" {
		binary = defaultRsyncBinary
	}
	return &Rsync{binary: binary, runner: runner}
}

// CompareDryRun runs rsync dry-run with itemize-changes and stats for deploy planning.
func (r *Rsync) CompareDryRun(ctx context.Context, request CompareDryRunRequest) (ProcessResult, error) {
	if err := validateCompareDryRunRequest(request); err != nil {
		return ProcessResult{ExitCode: -1}, err
	}

	args := r.compareArgs(request)
	result, err := r.runner.Run(ctx, r.binary, args, nil)
	if err != nil {
		return result, fmt.Errorf("run rsync: %w", err)
	}
	return result, nil
}

// Upload runs rsync for a file or directory transfer.
func (r *Rsync) Upload(ctx context.Context, request RsyncRequest) (ProcessResult, error) {
	if err := validateRsyncRequest(request); err != nil {
		return ProcessResult{ExitCode: -1}, err
	}

	args := r.args(request)
	result, err := r.runner.Run(ctx, r.binary, args, func(line string) {
		if request.OnProgress == nil {
			return
		}
		progress := parseRsyncProgress(line)
		if progress.Line != "" {
			request.OnProgress(progress)
		}
	})
	if err != nil {
		return result, fmt.Errorf("run rsync: %w", err)
	}
	return result, nil
}

func validateRsyncRequest(request RsyncRequest) error {
	if strings.TrimSpace(request.Source) == "" {
		return fmt.Errorf("rsync source is required")
	}
	if strings.TrimSpace(request.Destination) == "" {
		return fmt.Errorf("rsync destination is required")
	}
	if _, err := os.Stat(request.Source); err != nil {
		return fmt.Errorf("stat rsync source %q: %w", request.Source, err)
	}
	return nil
}

func validateCompareDryRunRequest(request CompareDryRunRequest) error {
	if strings.TrimSpace(request.Source) == "" {
		return fmt.Errorf("rsync source is required")
	}
	if strings.TrimSpace(request.Destination) == "" {
		return fmt.Errorf("rsync destination is required")
	}
	info, err := os.Stat(request.Source)
	if err != nil {
		return fmt.Errorf("stat rsync source %q: %w", request.Source, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("rsync source %q is not a directory", request.Source)
	}
	return nil
}

func (r *Rsync) compareArgs(request CompareDryRunRequest) []string {
	args := []string{
		"-a",
		"--dry-run",
		"--itemize-changes",
		"--stats",
		"--human-readable",
	}
	if request.Delete {
		args = append(args, "--delete")
	}
	if request.SSH != nil {
		args = append(args, "-e", rsyncSSHCommand(*request.SSH))
	}
	args = append(args, request.Source, request.Destination)
	return args
}

func (r *Rsync) args(request RsyncRequest) []string {
	args := []string{
		"-a",
		"--human-readable",
		"--info=progress2",
		"--out-format=%n",
	}
	if request.Delete {
		args = append(args, "--delete")
	}
	if request.DryRun {
		args = append(args, "--dry-run")
	}
	if request.SSH != nil {
		args = append(args, "-e", rsyncSSHCommand(*request.SSH))
	}
	args = append(args, request.Source, request.Destination)
	return args
}

func rsyncSSHCommand(options SSHOptions) string {
	port := options.Port
	if port == 0 {
		port = defaultSSHPort
	}
	parts := []string{"ssh", "-p", strconv.Itoa(port)}
	if strings.TrimSpace(options.KeyPath) != "" {
		keyPath, err := expandHome(options.KeyPath)
		if err == nil {
			parts = append(parts, "-i", keyPath, "-o", "IdentitiesOnly=yes")
		}
	}
	return strings.Join(parts, " ")
}

// RsyncDestinationFromTarget formats an rsync destination for a configured target.
func RsyncDestinationFromTarget(target config.Target, remotePath string) string {
	return fmt.Sprintf("%s@%s:%s", target.User, target.Host, remotePath)
}

func parseRsyncProgress(line string) RsyncProgress {
	clean := strings.TrimSpace(line)
	if clean == "" {
		return RsyncProgress{}
	}
	progress := RsyncProgress{Line: clean}
	percentIndex := strings.Index(clean, "%")
	if percentIndex == -1 {
		return progress
	}
	start := percentIndex - 1
	for start >= 0 {
		if clean[start] < '0' || clean[start] > '9' {
			break
		}
		start--
	}
	if value, err := strconv.Atoi(clean[start+1 : percentIndex]); err == nil {
		progress.Percent = value
	}
	return progress
}

type execProcessRunner struct{}

func (r execProcessRunner) Run(ctx context.Context, name string, args []string, onOutput func(string)) (ProcessResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ProcessResult{ExitCode: -1}, fmt.Errorf("open stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return ProcessResult{ExitCode: -1}, fmt.Errorf("open stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return ProcessResult{ExitCode: -1}, fmt.Errorf("start %s: %w", name, err)
	}

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go collectProcessOutput(stdout, &stdoutBuf, onOutput, &wg)
	go collectProcessOutput(stderr, &stderrBuf, onOutput, &wg)

	waitErr := cmd.Wait()
	wg.Wait()

	result := ProcessResult{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: processExitCode(waitErr),
	}
	if waitErr != nil {
		return result, waitErr
	}
	return result, nil
}

func collectProcessOutput(r io.Reader, dst *bytes.Buffer, onOutput func(string), wg *sync.WaitGroup) {
	defer wg.Done()
	var line bytes.Buffer
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			_, _ = dst.Write(chunk)
			for _, b := range chunk {
				if b == '\n' || b == '\r' {
					emitProcessLine(line.String(), onOutput)
					line.Reset()
					continue
				}
				line.WriteByte(b)
			}
		}
		if err != nil {
			break
		}
	}
	emitProcessLine(line.String(), onOutput)
}

func emitProcessLine(line string, onOutput func(string)) {
	if onOutput == nil {
		return
	}
	if strings.TrimSpace(line) == "" {
		return
	}
	onOutput(line)
}

func processExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}
