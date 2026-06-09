package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/config"
	"github.com/norlinga/mason/internal/remote"
)

// scriptPrompter answers from a map keyed by label, falling back to the offered default.
type scriptPrompter struct {
	answers map[string]string
	asked   []string
}

func (p *scriptPrompter) Ask(label, def string) (string, error) {
	p.asked = append(p.asked, label)
	if v, ok := p.answers[label]; ok {
		return v, nil
	}
	return def, nil
}

// fakeKeygen records generation and creates a placeholder key file.
type fakeKeygen struct {
	called int
	err    error
}

func (k *fakeKeygen) Generate(_ context.Context, keyPath string) error {
	k.called++
	if k.err != nil {
		return k.err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(keyPath, []byte("FAKE KEY"), 0o600)
}

// fakeRunner stands in for an SSH command runner.
type fakeRunner struct {
	result remote.CommandResult
	err    error
}

func (r fakeRunner) Run(context.Context, string) (remote.CommandResult, error) {
	return r.result, r.err
}

func homeDirFunc(dir string) func() (string, error) {
	return func() (string, error) { return dir, nil }
}

func TestBuildConfigDefaultsAndValidates(t *testing.T) {
	cfg, err := BuildConfig(Input{Host: "h.example.com", Root: "/srv/share", BaseURL: "https://h.example.com"})
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	if cfg.Default != "home" {
		t.Fatalf("default = %q, want home", cfg.Default)
	}
	target := cfg.Targets["home"]
	if target.User != "mason" {
		t.Fatalf("user = %q, want default mason", target.User)
	}
}

func TestBuildConfigMissingRequired(t *testing.T) {
	if _, err := BuildConfig(Input{Root: "/srv/share", BaseURL: "https://x"}); err == nil {
		t.Fatal("BuildConfig err = nil, want missing-host error")
	}
}

func TestMarshalConfigRoundTrips(t *testing.T) {
	cfg, err := BuildConfig(Input{
		TargetName: "prod", Host: "h", User: "mason", Root: "/srv/share",
		BaseURL: "https://h", DefaultPath: "inbox/", BuildDir: "./public", RemotePath: "/srv/site",
	})
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	data, err := MarshalConfig(cfg)
	if err != nil {
		t.Fatalf("MarshalConfig: %v", err)
	}
	parsed, err := config.Parse(data)
	if err != nil {
		t.Fatalf("re-parse rendered config: %v", err)
	}
	got := parsed.Targets["prod"]
	if got.Share.DefaultPath != "inbox/" || got.Deploy.RemotePath != "/srv/site" {
		t.Fatalf("round-trip lost fields: %#v", got)
	}
}

func TestWarnings(t *testing.T) {
	tests := []struct {
		name   string
		target config.Target
		want   string // substring expected, or "" for none
	}{
		{"mason ok", config.Target{User: "mason", BaseURL: "https://x"}, ""},
		{"root warns", config.Target{User: "root", BaseURL: "https://x"}, "root"},
		{"personal warns", config.Target{User: "aaron", BaseURL: "https://x"}, "dedicated"},
		{"http warns", config.Target{User: "mason", BaseURL: "http://x"}, "TLS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Warnings(tt.target)
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("warnings = %v, want none", got)
				}
				return
			}
			joined := strings.Join(got, "\n")
			if !strings.Contains(joined, tt.want) {
				t.Fatalf("warnings = %v, want substring %q", got, tt.want)
			}
		})
	}
}

func TestResolveKeyPath(t *testing.T) {
	home := homeDirFunc("/home/u")
	got, err := ResolveKeyPath("", home)
	if err != nil {
		t.Fatalf("ResolveKeyPath default: %v", err)
	}
	if got != "/home/u/.ssh/id_ed25519" {
		t.Fatalf("default key = %q", got)
	}
	got, err = ResolveKeyPath("~/keys/mason", home)
	if err != nil {
		t.Fatalf("ResolveKeyPath ~: %v", err)
	}
	if got != "/home/u/keys/mason" {
		t.Fatalf("~ expansion = %q", got)
	}
	got, _ = ResolveKeyPath("/abs/key", home)
	if got != "/abs/key" {
		t.Fatalf("absolute key = %q", got)
	}
}

func TestWriteConfigFileRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mason")
	if err := WriteConfigFile(path, []byte("a: 1\n"), false); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WriteConfigFile(path, []byte("a: 2\n"), false); err == nil {
		t.Fatal("second write err = nil, want refuse-overwrite")
	}
	if err := WriteConfigFile(path, []byte("a: 3\n"), true); err != nil {
		t.Fatalf("force write: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "a: 3\n" {
		t.Fatalf("force did not overwrite: %q", data)
	}
}

func TestTestConnection(t *testing.T) {
	if err := TestConnection(context.Background(), fakeRunner{result: remote.CommandResult{ExitCode: 0}}); err != nil {
		t.Fatalf("ok runner: %v", err)
	}
	if err := TestConnection(context.Background(), fakeRunner{result: remote.CommandResult{ExitCode: 1, Stderr: "denied"}}); err == nil {
		t.Fatal("nonzero exit err = nil, want error")
	}
	if err := TestConnection(context.Background(), fakeRunner{err: errors.New("dial")}); err == nil {
		t.Fatal("dial error err = nil, want error")
	}
	if err := TestConnection(context.Background(), nil); err == nil {
		t.Fatal("nil runner err = nil, want error")
	}
}

func TestRunHappyPathGeneratesKeyWritesConfigAndTests(t *testing.T) {
	home := t.TempDir()
	keygen := &fakeKeygen{}
	deps := Deps{
		Prompter: &scriptPrompter{answers: map[string]string{
			"Remote host":     "h.example.com",
			"Public base URL": "https://h.example.com",
		}},
		Keygen:  keygen,
		Connect: func(config.Target) remote.CommandRunner { return fakeRunner{} },
		HomeDir: homeDirFunc(home),
	}
	res, err := Run(context.Background(), deps, Params{GenerateKey: true, Test: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.KeyGenerated || keygen.called != 1 {
		t.Fatalf("key not generated: %#v (calls=%d)", res, keygen.called)
	}
	if res.ConfigPath != filepath.Join(home, ".mason") {
		t.Fatalf("config path = %q", res.ConfigPath)
	}
	if !res.Tested || !res.TestOK {
		t.Fatalf("test result = %#v, want tested ok", res)
	}
	cfg, err := config.LoadFile(res.ConfigPath)
	if err != nil {
		t.Fatalf("written config invalid: %v", err)
	}
	if cfg.Targets[cfg.Default].Host != "h.example.com" {
		t.Fatalf("written host = %q", cfg.Targets[cfg.Default].Host)
	}
	// The written config records the key path setup actually resolved and used.
	if got, want := cfg.Targets[cfg.Default].KeyPath, filepath.Join(home, ".ssh/id_ed25519"); got != want {
		t.Fatalf("written key_path = %q, want %q", got, want)
	}
}

func TestRunMissingKeyWarnsAndSkipsTest(t *testing.T) {
	home := t.TempDir()
	connectCalled := false
	deps := Deps{
		Connect: func(config.Target) remote.CommandRunner {
			connectCalled = true
			return fakeRunner{}
		},
		HomeDir: homeDirFunc(home),
	}
	params := Params{
		Input: Input{Host: "h", BaseURL: "https://h", Root: "/srv/share"},
		Test:  true, // GenerateKey deliberately false
	}
	res, err := Run(context.Background(), deps, params)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.KeyExists || res.KeyGenerated {
		t.Fatalf("key state = %#v, want missing", res)
	}
	if res.Tested || connectCalled {
		t.Fatal("connectivity test ran despite missing key")
	}
	joined := strings.Join(res.Warnings, "\n")
	if !strings.Contains(joined, "no SSH key") {
		t.Fatalf("warnings = %v, want a missing-key warning", res.Warnings)
	}
	if _, err := os.Stat(res.ConfigPath); err != nil {
		t.Fatal("config not written despite missing key")
	}
}

func TestFillInputDefaultsBaseURLFromHost(t *testing.T) {
	p := &scriptPrompter{answers: map[string]string{"Remote host": "h.example.com"}}
	in, err := fillInput(Input{}, p)
	if err != nil {
		t.Fatalf("fillInput: %v", err)
	}
	if in.BaseURL != "https://h.example.com" {
		t.Fatalf("BaseURL = %q, want https default derived from host", in.BaseURL)
	}
}

func TestRunReportsTestFailureNonFatally(t *testing.T) {
	home := t.TempDir()
	deps := Deps{
		Keygen:  &fakeKeygen{},
		Connect: func(config.Target) remote.CommandRunner { return fakeRunner{err: errors.New("connection refused")} },
		HomeDir: homeDirFunc(home),
	}
	params := Params{
		Input:       Input{Host: "h", BaseURL: "https://h", Root: "/srv/share"},
		GenerateKey: true,
		Test:        true,
	}
	res, err := Run(context.Background(), deps, params)
	if err != nil {
		t.Fatalf("Run returned error for failed test (should be non-fatal): %v", err)
	}
	if res.TestOK {
		t.Fatal("TestOK = true, want false")
	}
	if !strings.Contains(res.TestErr, "connection refused") {
		t.Fatalf("TestErr = %q", res.TestErr)
	}
	if _, err := os.Stat(res.ConfigPath); err != nil {
		t.Fatal("config not written despite failed connectivity test")
	}
}

func TestRunRefusesExistingConfig(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".mason"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := Deps{HomeDir: homeDirFunc(home)}
	params := Params{Input: Input{Host: "h", BaseURL: "https://h", Root: "/srv/share"}}
	if _, err := Run(context.Background(), deps, params); err == nil {
		t.Fatal("Run err = nil, want refuse-overwrite")
	}
}

func TestRunNudgesAwayFromRoot(t *testing.T) {
	home := t.TempDir()
	deps := Deps{HomeDir: homeDirFunc(home)}
	params := Params{Input: Input{Host: "h", User: "root", BaseURL: "https://h", Root: "/srv/share"}}
	res, err := Run(context.Background(), deps, params)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Warnings) == 0 || !strings.Contains(strings.Join(res.Warnings, "\n"), "root") {
		t.Fatalf("warnings = %v, want a root nudge", res.Warnings)
	}
}
