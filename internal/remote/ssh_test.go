package remote

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestSSHArgsLeaveUnsetOptionsToSSHConfig(t *testing.T) {
	args := SSHOptions{User: "mason", Host: "example.com"}.Args()
	for _, banned := range []string{"-p", "-i", "IdentitiesOnly=yes", "StrictHostKeyChecking=accept-new"} {
		if slices.Contains(args, banned) {
			t.Fatalf("args contain %q without it being configured: %v", banned, args)
		}
	}
	if !slices.Contains(args, "BatchMode=yes") {
		t.Fatalf("args missing BatchMode=yes: %v", args)
	}
	if !slices.Contains((SSHOptions{Host: "h", AcceptNewHostKey: true}).Args(), "StrictHostKeyChecking=accept-new") {
		t.Fatal("AcceptNewHostKey not rendered")
	}
}

func TestSSHRunPassesDestinationAndCommand(t *testing.T) {
	runner := &fakeProcessRunner{result: ProcessResult{Stdout: "ok\n"}}
	ssh := NewSSHWithRunner(SSHOptions{User: "mason", Host: "example.com"}, "ssh-test", runner)

	res, err := ssh.Run(context.Background(), "echo ok")
	if err != nil || res.Stdout != "ok\n" || res.ExitCode != 0 {
		t.Fatalf("Run = %+v, %v", res, err)
	}
	n := len(runner.args)
	if runner.name != "ssh-test" || runner.args[n-3] != "--" || runner.args[n-2] != "mason@example.com" || runner.args[n-1] != "echo ok" {
		t.Fatalf("invocation = %s %v", runner.name, runner.args)
	}
}

func TestSSHRunRemoteFailureDoesNotEchoCommand(t *testing.T) {
	runner := &fakeProcessRunner{
		result: ProcessResult{Stderr: "boom\n", ExitCode: 3},
		err:    &exec.ExitError{},
	}
	ssh := NewSSHWithRunner(SSHOptions{Host: "h"}, "ssh", runner)
	res, err := ssh.Run(context.Background(), "a-very-long-secret-script")
	if err == nil || res.ExitCode != 3 || res.Stderr != "boom\n" {
		t.Fatalf("Run = %+v, %v", res, err)
	}
	if strings.Contains(err.Error(), "a-very-long-secret-script") {
		t.Fatalf("error echoes the command: %v", err)
	}
	var ce *ConnectError
	if errors.As(err, &ce) {
		t.Fatalf("remote failure misreported as connect error: %v", err)
	}
}

func TestSSHConnectErrors(t *testing.T) {
	tests := map[string]string{
		"Host key verification failed.\r\n":                      "mason setup example.com",
		"mason@example.com: Permission denied (publickey).\r\n":  "not authorized for mason@example.com",
		"ssh: Could not resolve hostname example.com: Name or s": "cannot resolve host example.com",
		"kex_exchange_identification: something odd\n":           "kex_exchange_identification: something odd",
	}
	for stderr, want := range tests {
		runner := &fakeProcessRunner{result: ProcessResult{Stderr: stderr, ExitCode: 255}, err: &exec.ExitError{}}
		res, err := NewSSHWithRunner(SSHOptions{User: "mason", Host: "example.com"}, "ssh", runner).Run(context.Background(), "true")
		var ce *ConnectError
		if !errors.As(err, &ce) || res.ExitCode != -1 || !strings.Contains(err.Error(), want) {
			t.Errorf("stderr %q: err = %v (exit %d), want ConnectError containing %q", stderr, err, res.ExitCode, want)
		}
	}
}

func TestControlDirFitsSocketLimit(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/"+strings.Repeat("x", 80))
	dir := controlDir()
	if dir == "" || len(dir) > maxControlDirLen || !strings.HasPrefix(dir, "/tmp/mason-") {
		t.Fatalf("controlDir = %q (len %d), want a short /tmp fallback", dir, len(dir))
	}
	t.Setenv("XDG_CACHE_HOME", "/home/u/.cache")
	if got := controlDir(); got != "/home/u/.cache/mason/ssh" {
		t.Fatalf("controlDir = %q", got)
	}
}
