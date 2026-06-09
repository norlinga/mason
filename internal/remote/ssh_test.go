package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/norlinga/mason/internal/config"
	"golang.org/x/crypto/ssh"
)

func TestSSHOptionsFromTarget(t *testing.T) {
	target := config.Target{
		Host:    "share.example.com",
		User:    "mason",
		Port:    2222,
		KeyPath: "~/.ssh/mason",
	}

	got := SSHOptionsFromTarget(target)
	if got.Host != target.Host {
		t.Fatalf("Host = %q, want %q", got.Host, target.Host)
	}
	if got.User != target.User {
		t.Fatalf("User = %q, want %q", got.User, target.User)
	}
	if got.Port != target.Port {
		t.Fatalf("Port = %d, want %d", got.Port, target.Port)
	}
	if got.KeyPath != target.KeyPath {
		t.Fatalf("KeyPath = %q, want %q", got.KeyPath, target.KeyPath)
	}
	if got.Timeout != defaultSSHTimeout {
		t.Fatalf("Timeout = %s, want %s", got.Timeout, defaultSSHTimeout)
	}
}

func TestClientConfigLoadsPrivateKey(t *testing.T) {
	keyPath := writePrivateKey(t)
	runner := NewSSH(SSHOptions{
		Host:    "share.example.com",
		User:    "mason",
		KeyPath: keyPath,
		Timeout: 5 * time.Second,
	})

	cfg, err := runner.clientConfig()
	if err != nil {
		t.Fatalf("clientConfig: %v", err)
	}
	if cfg.User != "mason" {
		t.Fatalf("User = %q, want mason", cfg.User)
	}
	if cfg.Timeout != 5*time.Second {
		t.Fatalf("Timeout = %s, want 5s", cfg.Timeout)
	}
	if len(cfg.Auth) != 1 {
		t.Fatalf("Auth length = %d, want 1", len(cfg.Auth))
	}
}

func TestAddressDefaultsPort(t *testing.T) {
	runner := NewSSH(SSHOptions{Host: "share.example.com"})
	if got, want := runner.address(), "share.example.com:22"; got != want {
		t.Fatalf("address = %q, want %q", got, want)
	}
}

func TestRunUsesInjectedDialer(t *testing.T) {
	keyPath := writePrivateKey(t)
	dialer := &fakeDialer{
		client: &fakeClient{
			session: &fakeSession{
				stdout: "ok\n",
				stderr: "warn\n",
			},
		},
	}
	runner := NewSSHWithDialer(SSHOptions{
		Host:    "share.example.com",
		User:    "mason",
		KeyPath: keyPath,
	}, dialer)

	result, err := runner.Run(context.Background(), "echo ok")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if dialer.address != "share.example.com:22" {
		t.Fatalf("dial address = %q, want share.example.com:22", dialer.address)
	}
	if dialer.clientConfig.User != "mason" {
		t.Fatalf("dial user = %q, want mason", dialer.clientConfig.User)
	}
	if got, want := result.Stdout, "ok\n"; got != want {
		t.Fatalf("Stdout = %q, want %q", got, want)
	}
	if got, want := result.Stderr, "warn\n"; got != want {
		t.Fatalf("Stderr = %q, want %q", got, want)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestRunReturnsCommandErrorWithOutput(t *testing.T) {
	keyPath := writePrivateKey(t)
	commandErr := errors.New("remote failed")
	runner := NewSSHWithDialer(SSHOptions{
		Host:    "share.example.com",
		User:    "mason",
		KeyPath: keyPath,
	}, &fakeDialer{
		client: &fakeClient{
			session: &fakeSession{
				stderr: "bad\n",
				err:    commandErr,
			},
		},
	})

	result, err := runner.Run(context.Background(), "false")
	if err == nil {
		t.Fatalf("Run succeeded, want error")
	}
	if !errors.Is(err, commandErr) {
		t.Fatalf("Run error = %v, want wrapped command error", err)
	}
	if result.Stderr != "bad\n" {
		t.Fatalf("Stderr = %q, want bad\\n", result.Stderr)
	}
	if result.ExitCode != -1 {
		t.Fatalf("ExitCode = %d, want -1 for unknown command error", result.ExitCode)
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := expandHome("~/.ssh/id_ed25519")
	if err != nil {
		t.Fatalf("expandHome: %v", err)
	}
	if want := filepath.Join(home, ".ssh", "id_ed25519"); got != want {
		t.Fatalf("expandHome = %q, want %q", got, want)
	}
}

func writePrivateKey(t *testing.T) string {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pemBlock, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(pemBlock), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path
}

type fakeDialer struct {
	address      string
	clientConfig *ssh.ClientConfig
	client       SSHClient
	err          error
}

func (d *fakeDialer) Dial(ctx context.Context, network, address string, cfg *ssh.ClientConfig) (SSHClient, error) {
	d.address = address
	d.clientConfig = cfg
	if d.err != nil {
		return nil, d.err
	}
	return d.client, nil
}

type fakeClient struct {
	session SSHSession
	closed  bool
	err     error
}

func (c *fakeClient) NewSession() (SSHSession, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.session, nil
}

func (c *fakeClient) Close() error {
	c.closed = true
	return nil
}

type fakeSession struct {
	stdout       string
	stderr       string
	stdoutWriter io.Writer
	stderrWriter io.Writer
	closed       bool
	err          error
}

func (s *fakeSession) Run(command string) error {
	if s.stdoutWriter != nil {
		_, _ = io.WriteString(s.stdoutWriter, s.stdout)
	}
	if s.stderrWriter != nil {
		_, _ = io.WriteString(s.stderrWriter, s.stderr)
	}
	return s.err
}

func (s *fakeSession) Close() error {
	s.closed = true
	return nil
}

func (s *fakeSession) SetStdout(w io.Writer) {
	s.stdoutWriter = w
}

func (s *fakeSession) SetStderr(w io.Writer) {
	s.stderrWriter = w
}
