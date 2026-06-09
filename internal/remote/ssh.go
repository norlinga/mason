package remote

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/norlinga/mason/internal/config"
	"golang.org/x/crypto/ssh"
)

const (
	defaultSSHPort    = 22
	defaultSSHTimeout = 30 * time.Second
)

// SSHOptions contains the connection settings needed to run remote commands.
type SSHOptions struct {
	Host    string
	User    string
	Port    int
	KeyPath string
	Timeout time.Duration
}

// CommandResult captures remote command output.
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// CommandRunner runs shell commands on a remote target.
type CommandRunner interface {
	Run(ctx context.Context, command string) (CommandResult, error)
}

// SSH runs commands over SSH.
type SSH struct {
	options SSHOptions
	dialer  SSHDialer
}

// SSHDialer connects to an SSH server. It is injectable for tests.
type SSHDialer interface {
	Dial(ctx context.Context, network, address string, config *ssh.ClientConfig) (SSHClient, error)
}

// SSHClient is the subset of ssh.Client used by this package.
type SSHClient interface {
	NewSession() (SSHSession, error)
	Close() error
}

// SSHSession is the subset of ssh.Session used by this package.
type SSHSession interface {
	Run(command string) error
	Close() error
	SetStdout(io.Writer)
	SetStderr(io.Writer)
}

// SSHOptionsFromTarget converts a configured target into SSH options.
func SSHOptionsFromTarget(target config.Target) SSHOptions {
	return SSHOptions{
		Host:    target.Host,
		User:    target.User,
		Port:    target.Port,
		KeyPath: target.KeyPath,
		Timeout: defaultSSHTimeout,
	}
}

// NewSSH creates a real SSH command runner.
func NewSSH(options SSHOptions) *SSH {
	return NewSSHWithDialer(options, netSSHDialer{})
}

// NewSSHWithDialer creates an SSH command runner with an injected dialer.
func NewSSHWithDialer(options SSHOptions, dialer SSHDialer) *SSH {
	return &SSH{
		options: options,
		dialer:  dialer,
	}
}

// Run executes command on the remote host and returns captured output.
func (s *SSH) Run(ctx context.Context, command string) (CommandResult, error) {
	if strings.TrimSpace(command) == "" {
		return CommandResult{ExitCode: -1}, fmt.Errorf("remote command is required")
	}

	clientConfig, err := s.clientConfig()
	if err != nil {
		return CommandResult{ExitCode: -1}, err
	}

	client, err := s.dialer.Dial(ctx, "tcp", s.address(), clientConfig)
	if err != nil {
		return CommandResult{ExitCode: -1}, fmt.Errorf("connect ssh %s: %w", s.address(), err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return CommandResult{ExitCode: -1}, fmt.Errorf("open ssh session: %w", err)
	}
	defer session.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.SetStdout(&stdout)
	session.SetStderr(&stderr)

	err = session.Run(command)
	result := CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode(err),
	}
	if err != nil {
		return result, fmt.Errorf("run remote command %q: %w", command, err)
	}
	return result, nil
}

func (s *SSH) clientConfig() (*ssh.ClientConfig, error) {
	if strings.TrimSpace(s.options.User) == "" {
		return nil, fmt.Errorf("ssh user is required")
	}
	signer, err := signerFromKeyPath(s.options.KeyPath)
	if err != nil {
		return nil, err
	}
	timeout := s.options.Timeout
	if timeout == 0 {
		timeout = defaultSSHTimeout
	}
	return &ssh.ClientConfig{
		User:            s.options.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}, nil
}

func (s *SSH) address() string {
	port := s.options.Port
	if port == 0 {
		port = defaultSSHPort
	}
	return net.JoinHostPort(s.options.Host, strconv.Itoa(port))
}

func signerFromKeyPath(path string) (ssh.Signer, error) {
	if strings.TrimSpace(path) == "" {
		path = "~/.ssh/id_ed25519"
	}
	expanded, err := expandHome(path)
	if err != nil {
		return nil, err
	}
	key, err := os.ReadFile(expanded)
	if err != nil {
		return nil, fmt.Errorf("read ssh key %q: %w", expanded, err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse ssh key %q: %w", expanded, err)
	}
	return signer, nil
}

func expandHome(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*ssh.ExitError); ok {
		return exitErr.ExitStatus()
	}
	return -1
}

type netSSHDialer struct{}

func (d netSSHDialer) Dial(ctx context.Context, network, address string, config *ssh.ClientConfig) (SSHClient, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	clientConn, chans, reqs, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return sshClient{Client: ssh.NewClient(clientConn, chans, reqs)}, nil
}

type sshClient struct {
	*ssh.Client
}

func (c sshClient) NewSession() (SSHSession, error) {
	session, err := c.Client.NewSession()
	if err != nil {
		return nil, err
	}
	return sshSession{Session: session}, nil
}

type sshSession struct {
	*ssh.Session
}

func (s sshSession) SetStdout(w io.Writer) {
	s.Stdout = w
}

func (s sshSession) SetStderr(w io.Writer) {
	s.Stderr = w
}
