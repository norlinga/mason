package remote

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveSSH runs against a real server when MASON_LIVE_SSH=user@host is set. It checks
// that multiplexed connections do not hold the output pipes open (a ControlPersist master
// inheriting stderr would make Run block until the master exits).
func TestLiveSSH(t *testing.T) {
	dest := os.Getenv("MASON_LIVE_SSH")
	if dest == "" {
		t.Skip("set MASON_LIVE_SSH=user@host to run")
	}
	user, host, _ := strings.Cut(dest, "@")
	opts := SSHOptions{User: user, Host: host, ControlDir: t.TempDir()}
	ssh := NewSSH(opts)

	for i, want := range []string{"first", "second"} {
		start := time.Now()
		res, err := ssh.Run(context.Background(), "echo "+want)
		if err != nil {
			t.Fatalf("run %d: %v (stderr %q)", i, err, res.Stderr)
		}
		if strings.TrimSpace(res.Stdout) != want {
			t.Fatalf("run %d stdout = %q", i, res.Stdout)
		}
		if took := time.Since(start); took > 10*time.Second {
			t.Fatalf("run %d took %s — output pipes held open by the control master?", i, took)
		}
		t.Logf("run %d: %s", i, time.Since(start))
	}

	res, err := ssh.Run(context.Background(), "exit 3")
	if err == nil || res.ExitCode != 3 {
		t.Fatalf("exit 3: code %d err %v", res.ExitCode, err)
	}
}
