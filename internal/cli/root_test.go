package cli

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

func TestRootHelpListsGlobalFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute help: %v", err)
	}

	out := stdout.String()
	for _, want := range []string{
		"--json",
		"--yes",
		"--quiet",
		"--verbose",
		"--config",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help output missing %q:\n%s", want, out)
		}
	}
}

func TestRootHelpDropsV1Surface(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute help: %v", err)
	}
	out := stdout.String()
	for _, gone := range []string{"--target", "remote-init", "site-init", "  target "} {
		if strings.Contains(out, gone) {
			t.Fatalf("help still mentions %q:\n%s", gone, out)
		}
	}
	for _, want := range []string{"setup", "init", "provision", "deploy", "key", "doctor", "share"} {
		if !strings.Contains(out, "  "+want+" ") {
			t.Fatalf("help missing command %q:\n%s", want, out)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute version: %v", err)
	}
	if got, want := stdout.String(), "mason "+currentVersion()+"\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestResolveVersion(t *testing.T) {
	vcs := func(rev, modified string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: rev}, {Key: "vcs.modified", Value: modified}}}
	}
	tests := []struct {
		name    string
		stamped string
		info    *debug.BuildInfo
		ok      bool
		want    string
	}{
		{"stamped wins", "v0.2.0-3-gabc1234", vcs("45b5925aaaa", "false"), true, "v0.2.0-3-gabc1234"},
		{"clean commit", "", vcs("45b5925aaaa", "false"), true, "dev-45b5925"},
		{"dirty commit", "", vcs("45b5925aaaa", "true"), true, "dev-45b5925-dirty"},
		{"go install tag", "", &debug.BuildInfo{Main: debug.Module{Version: "v0.2.0"}}, true, "v0.2.0"},
		{"no vcs info", "", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true, "dev"},
		{"no build info", "", nil, false, "dev"},
	}
	for _, tt := range tests {
		if got := resolveVersion(tt.stamped, tt.info, tt.ok); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}
