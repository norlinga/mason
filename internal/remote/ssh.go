package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/norlinga/mason/internal/config"
)

const (
	defaultSSHBinary      = "ssh"
	defaultConnectTimeout = 15 // seconds
	controlPersist        = "60s"
	sshConnectFailure     = 255
)

// SSHOptions are the settings for reaching a server through the system ssh client. Zero
// values are left out of the ssh invocation so ~/.ssh/config can supply them.
type SSHOptions struct {
	Host    string
	User    string
	Port    int
	KeyPath string
	// ControlDir enables connection multiplexing with sockets in this directory.
	ControlDir string
	// AcceptNewHostKey trusts a host key on first contact (accept-new); only setup sets it.
	AcceptNewHostKey bool
}

// SSHOptionsFromServer builds options for a configured server, multiplexing connections
// through a socket directory under the user cache dir.
func SSHOptionsFromServer(server config.Server) SSHOptions {
	opts := SSHOptions{Host: server.Host, User: server.User, Port: server.Port, KeyPath: server.Key}
	opts.ControlDir = controlDir()
	return opts
}

// maxControlDirLen keeps ControlPath (<dir>/<40-hex %C>) plus the 17-byte temporary
// suffix OpenSSH adds while binding under the smallest unix socket limit (104 bytes).
const maxControlDirLen = 104 - 1 - 40 - 17 - 1

// controlDir picks a socket directory short enough for a unix socket path: the user
// cache dir, else a per-user directory in /tmp, else "" (no multiplexing).
func controlDir() string {
	if cache, err := os.UserCacheDir(); err == nil {
		if dir := filepath.Join(cache, "mason", "ssh"); len(dir) <= maxControlDirLen {
			return dir
		}
	}
	if dir := filepath.Join("/tmp", fmt.Sprintf("mason-%d", os.Getuid())); len(dir) <= maxControlDirLen {
		return dir
	}
	return ""
}

// Args returns the ssh options (everything before the destination). rsync's -e transport
// uses the same list so both paths authenticate and verify hosts identically.
func (o SSHOptions) Args() []string {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=" + strconv.Itoa(defaultConnectTimeout),
	}
	if o.AcceptNewHostKey {
		args = append(args, "-o", "StrictHostKeyChecking=accept-new")
	}
	if o.Port != 0 {
		args = append(args, "-p", strconv.Itoa(o.Port))
	}
	if key := strings.TrimSpace(o.KeyPath); key != "" {
		if expanded, err := expandHome(key); err == nil {
			key = expanded
		}
		args = append(args, "-i", key, "-o", "IdentitiesOnly=yes")
	}
	if o.ControlDir != "" {
		args = append(args,
			"-o", "ControlMaster=auto",
			"-o", "ControlPath="+filepath.Join(o.ControlDir, "%C"),
			"-o", "ControlPersist="+controlPersist,
		)
	}
	return args
}

// Destination renders user@host for ssh.
func (o SSHOptions) Destination() string {
	if o.User == "" {
		return o.Host
	}
	return o.User + "@" + o.Host
}

// rsyncHost renders the host part of an rsync remote spec, bracketing IPv6 literals.
func (o SSHOptions) rsyncHost() string {
	host := o.Host
	if ip := net.ParseIP(host); ip != nil && strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if o.User == "" {
		return host
	}
	return o.User + "@" + host
}

func (o SSHOptions) prepare() error {
	if o.ControlDir == "" {
		return nil
	}
	if err := os.MkdirAll(o.ControlDir, 0o700); err != nil {
		return fmt.Errorf("create ssh control dir: %w", err)
	}
	return nil
}

// CommandResult captures remote command output.
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// CommandRunner runs shell commands on a remote server.
type CommandRunner interface {
	Run(ctx context.Context, command string) (CommandResult, error)
}

// ConnectError reports that ssh could not reach or authenticate to the server.
type ConnectError struct {
	Destination string
	Reason      string
	Detail      string
}

func (e *ConnectError) Error() string {
	return fmt.Sprintf("ssh %s: %s", e.Destination, e.Reason)
}

// SSH runs commands through the system ssh binary.
type SSH struct {
	options SSHOptions
	binary  string
	runner  ProcessRunner
}

// NewSSH creates a runner that shells out to ssh.
func NewSSH(options SSHOptions) *SSH {
	return NewSSHWithRunner(options, defaultSSHBinary, execProcessRunner{})
}

// NewSSHWithRunner creates a runner with an injected process runner (tests).
func NewSSHWithRunner(options SSHOptions, binary string, runner ProcessRunner) *SSH {
	if strings.TrimSpace(binary) == "" {
		binary = defaultSSHBinary
	}
	return &SSH{options: options, binary: binary, runner: runner}
}

// Run executes command on the server and returns its output and exit code.
func (s *SSH) Run(ctx context.Context, command string) (CommandResult, error) {
	if strings.TrimSpace(command) == "" {
		return CommandResult{ExitCode: -1}, errors.New("remote command is required")
	}
	if strings.TrimSpace(s.options.Host) == "" {
		return CommandResult{ExitCode: -1}, errors.New("ssh host is required")
	}
	if err := s.options.prepare(); err != nil {
		return CommandResult{ExitCode: -1}, err
	}

	args := append(s.options.Args(), "--", s.options.Destination(), command)
	res, err := s.runner.Run(ctx, s.binary, args, nil)
	result := CommandResult{Stdout: res.Stdout, Stderr: res.Stderr, ExitCode: res.ExitCode}
	if err == nil {
		return result, nil
	}
	if res.ExitCode == sshConnectFailure || res.ExitCode < 0 {
		result.ExitCode = -1
		return result, connectError(s.options.Destination(), res.Stderr, err)
	}
	return result, fmt.Errorf("remote command exited %d", res.ExitCode)
}

// connectError turns ssh's stderr into one actionable line.
func connectError(dest, stderr string, err error) *ConnectError {
	detail := strings.TrimSpace(stderr)
	reasons := []struct{ match, reason string }{
		{"Host key verification failed", "host key is unknown or changed — run 'mason setup " + hostOf(dest) + "' to trust it, or check known_hosts"},
		{"REMOTE HOST IDENTIFICATION HAS CHANGED", "host key changed — verify the server, then fix ~/.ssh/known_hosts"},
		{"Permission denied", "permission denied — this machine's key is not authorized for " + dest + " (run 'mason setup " + hostOf(dest) + "')"},
		{"Could not resolve hostname", "cannot resolve host " + hostOf(dest)},
		{"Connection refused", "connection refused by " + hostOf(dest)},
		{"timed out", "connection timed out"},
		{"No route to host", "no route to host " + hostOf(dest)},
	}
	for _, r := range reasons {
		if strings.Contains(detail, r.match) {
			return &ConnectError{Destination: dest, Reason: r.reason, Detail: detail}
		}
	}
	if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "executable file not found") {
		return &ConnectError{Destination: dest, Reason: "ssh is not installed or not on PATH", Detail: err.Error()}
	}
	reason := lastLine(detail)
	if reason == "" {
		reason = err.Error()
	}
	return &ConnectError{Destination: dest, Reason: reason, Detail: detail}
}

func hostOf(dest string) string {
	if i := strings.LastIndex(dest, "@"); i >= 0 {
		return dest[i+1:]
	}
	return dest
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/")), nil
}
