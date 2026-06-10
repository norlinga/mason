package setup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/config"
	"github.com/norlinga/mason/internal/domain"
	"github.com/norlinga/mason/internal/remote"
)

// scriptedRunner answers commands via a function, defaulting to a clean exit.
type scriptedRunner struct {
	fn func(cmd string) (remote.CommandResult, error)
}

func (r scriptedRunner) Run(_ context.Context, cmd string) (remote.CommandResult, error) {
	return r.fn(cmd)
}

// healthyRunner returns success for every probe a healthy server would pass.
func healthyRunner(caddyConfig string) scriptedRunner {
	return scriptedRunner{fn: func(cmd string) (remote.CommandResult, error) {
		switch {
		case cmd == "true":
			return remote.CommandResult{ExitCode: 0}, nil
		case cmd == "command -v rsync":
			return remote.CommandResult{Stdout: "/usr/bin/rsync\n", ExitCode: 0}, nil
		case strings.HasPrefix(cmd, "test -d"):
			return remote.CommandResult{ExitCode: 0}, nil
		case strings.HasPrefix(cmd, "stat -c"):
			return remote.CommandResult{Stdout: "700\n", ExitCode: 0}, nil
		case strings.HasPrefix(cmd, "curl -s http://localhost:2019"):
			return remote.CommandResult{Stdout: caddyConfig, ExitCode: 0}, nil
		default:
			return remote.CommandResult{ExitCode: 0}, nil
		}
	}}
}

// memStore returns a fixed manifest (or error).
type memStore struct {
	m   *domain.Manifest
	err error
}

func (s memStore) Fetch(context.Context) (*domain.Manifest, error) { return s.m, s.err }
func (s memStore) Put(context.Context, *domain.Manifest) error     { return nil }
func (s memStore) Update(context.Context, func(*domain.Manifest) error) error {
	return nil
}

func notPublicGet(_ context.Context, url string) (int, error) {
	if strings.Contains(url, "/.mason/") || strings.Contains(url, "/objects/") {
		return 404, nil
	}
	return 200, nil
}

func target() config.Target {
	return config.Target{
		Host:    "share.example.com",
		User:    "mason",
		Root:    "/srv/share",
		BaseURL: "https://share.example.com",
	}
}

const caddyConfigJSON = `{"apps":{"http":{"servers":{"srv0":{"routes":[{"match":[{"host":["share.example.com"]}],"handle":[{"handler":"vars","root":"/srv/share"},{"handler":"file_server"}]}]}}}}}`

const probeBlob = "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

func probeManifest() *domain.Manifest {
	return &domain.Manifest{
		Version: domain.ManifestVersion,
		Blobs: map[string]domain.Blob{
			probeBlob: {Size: 1},
		},
		Shares: []domain.Share{{
			ID:   "01J9Z",
			Blob: probeBlob,
			Slug: &domain.SlugEndpoint{Value: "f76Ab1", Ext: ".mp3", State: domain.EndpointOK},
		}},
		ReservedSlugs: []string{"f76Ab1"},
	}
}

func doctorHTTPGet(probeStatus int) func(context.Context, string) (int, error) {
	return func(_ context.Context, url string) (int, error) {
		if strings.Contains(url, "/.mason/") || strings.Contains(url, "/objects/") {
			return 404, nil
		}
		return probeStatus, nil
	}
}

func statusByName(report DoctorReport, name string) (CheckStatus, bool) {
	for _, c := range report.Checks {
		if c.Name == name {
			return c.Status, true
		}
	}
	return "", false
}

func TestDiagnoseAllHealthy(t *testing.T) {
	deps := DoctorDeps{
		Runner:  healthyRunner(caddyConfigJSON),
		Store:   memStore{m: probeManifest()},
		HTTPGet: doctorHTTPGet(200),
	}
	report := Diagnose(context.Background(), deps, target())
	if !report.Healthy() {
		t.Fatalf("report not healthy: %+v", report.Checks)
	}
	for _, name := range []string{"ssh connection", "rsync available", "share root writable", "symlink serving", "caddy serves host", "caddy root matches"} {
		if s, ok := statusByName(report, name); !ok || s != CheckOK {
			t.Fatalf("check %q = %q (found=%v), want ok", name, s, ok)
		}
	}
}

func TestDiagnosePublicPrefixFails(t *testing.T) {
	deps := DoctorDeps{
		Runner: healthyRunner(caddyConfigJSON),
		Store:  memStore{m: domain.NewManifest()},
		HTTPGet: func(_ context.Context, url string) (int, error) {
			if strings.Contains(url, "/.mason/") {
				return 200, nil // leaked!
			}
			return 404, nil
		},
	}
	report := Diagnose(context.Background(), deps, target())
	if report.Healthy() {
		t.Fatal("report healthy despite public manifest")
	}
	if s, _ := statusByName(report, "/.mason/manifest.json not public"); s != CheckFail {
		t.Fatalf("private-prefix check = %q, want fail", s)
	}
}

func TestDiagnoseObjectsPrefixPublicFails(t *testing.T) {
	deps := DoctorDeps{
		Runner: healthyRunner(caddyConfigJSON),
		Store:  memStore{m: domain.NewManifest()},
		HTTPGet: func(_ context.Context, url string) (int, error) {
			if strings.Contains(url, "/objects/") {
				return 200, nil
			}
			return 404, nil
		},
	}
	report := Diagnose(context.Background(), deps, target())
	if report.Healthy() {
		t.Fatal("report healthy despite public objects prefix")
	}
	if s, _ := statusByName(report, "/objects/ not public"); s != CheckFail {
		t.Fatalf("objects prefix check = %q, want fail", s)
	}
}

func TestDiagnoseRsyncMissingFails(t *testing.T) {
	runner := scriptedRunner{fn: func(cmd string) (remote.CommandResult, error) {
		if cmd == "command -v rsync" {
			return remote.CommandResult{ExitCode: 1}, nil
		}
		return healthyRunner(caddyConfigJSON).fn(cmd)
	}}
	deps := DoctorDeps{Runner: runner, Store: memStore{m: domain.NewManifest()}, HTTPGet: notPublicGet}
	report := Diagnose(context.Background(), deps, target())
	if s, _ := statusByName(report, "rsync available"); s != CheckFail {
		t.Fatalf("rsync check = %q, want fail", s)
	}
	if report.Healthy() {
		t.Fatal("report healthy despite missing rsync")
	}
}

func TestDiagnoseManifestErrorFails(t *testing.T) {
	deps := DoctorDeps{
		Runner:  healthyRunner(caddyConfigJSON),
		Store:   memStore{err: errors.New("permission denied")},
		HTTPGet: notPublicGet,
	}
	report := Diagnose(context.Background(), deps, target())
	if s, _ := statusByName(report, "manifest"); s != CheckFail {
		t.Fatalf("manifest check = %q, want fail", s)
	}
}

func TestDiagnoseManifestModeWarns(t *testing.T) {
	runner := scriptedRunner{fn: func(cmd string) (remote.CommandResult, error) {
		if strings.HasPrefix(cmd, "stat -c") {
			return remote.CommandResult{Stdout: "755\n", ExitCode: 0}, nil
		}
		return healthyRunner(caddyConfigJSON).fn(cmd)
	}}
	deps := DoctorDeps{Runner: runner, Store: memStore{m: domain.NewManifest()}, HTTPGet: notPublicGet}
	report := Diagnose(context.Background(), deps, target())
	s, _ := statusByName(report, ".mason private")
	if s != CheckWarn {
		t.Fatalf(".mason private = %q, want warn for world-readable mode", s)
	}
	if !report.Healthy() {
		t.Fatal("a warning should not make the report unhealthy")
	}
}

func TestDiagnoseCaddyUnavailableWarns(t *testing.T) {
	runner := scriptedRunner{fn: func(cmd string) (remote.CommandResult, error) {
		if strings.HasPrefix(cmd, "curl -s http://localhost:2019") {
			return remote.CommandResult{ExitCode: 7}, errors.New("connection refused")
		}
		return healthyRunner(caddyConfigJSON).fn(cmd)
	}}
	deps := DoctorDeps{Runner: runner, Store: memStore{m: domain.NewManifest()}, HTTPGet: notPublicGet}
	report := Diagnose(context.Background(), deps, target())
	for _, name := range []string{"caddy serves host", "caddy root matches"} {
		if s, _ := statusByName(report, name); s != CheckWarn {
			t.Fatalf("%s = %q, want warn when admin API down", name, s)
		}
	}
	if !report.Healthy() {
		t.Fatal("unreachable Caddy admin API must not fail the report")
	}
}

func TestDiagnoseCaddyRootMismatchWarns(t *testing.T) {
	mismatch := `{"x":{"host":["share.example.com"]},"y":{"root":"/var/www"}}`
	deps := DoctorDeps{Runner: healthyRunner(mismatch), Store: memStore{m: domain.NewManifest()}, HTTPGet: notPublicGet}
	report := Diagnose(context.Background(), deps, target())
	if s, _ := statusByName(report, "caddy serves host"); s != CheckOK {
		t.Fatalf("host check = %q, want ok", s)
	}
	if s, _ := statusByName(report, "caddy root matches"); s != CheckWarn {
		t.Fatalf("root check = %q, want warn for mismatched root", s)
	}
}

func TestDiagnoseDeployTreeProbedWhenSet(t *testing.T) {
	tg := target()
	tg.Deploy.RemotePath = "/srv/site"
	deps := DoctorDeps{Runner: healthyRunner(caddyConfigJSON), Store: memStore{m: domain.NewManifest()}, HTTPGet: notPublicGet}
	report := Diagnose(context.Background(), deps, tg)
	if _, ok := statusByName(report, "deploy tree writable"); !ok {
		t.Fatal("deploy tree check missing when remote_path set")
	}
}

func TestDiagnoseEmptyManifestHealthy(t *testing.T) {
	deps := DoctorDeps{Runner: healthyRunner(caddyConfigJSON), Store: memStore{m: domain.NewManifest()}, HTTPGet: notPublicGet}
	report := Diagnose(context.Background(), deps, target())
	if !report.Healthy() {
		t.Fatalf("empty manifest should stay healthy: %+v", report.Checks)
	}
	if s, _ := statusByName(report, "manifest"); s != CheckOK {
		t.Fatalf("manifest = %q, want ok", s)
	}
	if s, _ := statusByName(report, "symlink serving"); s != CheckWarn {
		t.Fatalf("symlink serving = %q, want warn when no endpoints", s)
	}
}

func TestDiagnoseSymlinkServingFails(t *testing.T) {
	deps := DoctorDeps{
		Runner:  healthyRunner(caddyConfigJSON),
		Store:   memStore{m: probeManifest()},
		HTTPGet: doctorHTTPGet(404),
	}
	report := Diagnose(context.Background(), deps, target())
	if report.Healthy() {
		t.Fatal("report healthy despite symlink 404")
	}
	if s, _ := statusByName(report, "symlink serving"); s != CheckFail {
		t.Fatalf("symlink serving = %q, want fail", s)
	}
}

func TestDiagnoseManifestErrorSkipsSymlinkProbe(t *testing.T) {
	deps := DoctorDeps{
		Runner:  healthyRunner(caddyConfigJSON),
		Store:   memStore{err: errors.New("not found")},
		HTTPGet: notPublicGet,
	}
	report := Diagnose(context.Background(), deps, target())
	if s, _ := statusByName(report, "manifest"); s != CheckFail {
		t.Fatalf("manifest = %q, want fail", s)
	}
	if s, _ := statusByName(report, "symlink serving"); s != CheckWarn {
		t.Fatalf("symlink serving = %q, want warn when manifest unavailable", s)
	}
}

func TestCaddyHostsAndRoots(t *testing.T) {
	hosts, roots := caddyHostsAndRoots([]byte(caddyConfigJSON))
	if !contains(hosts, "share.example.com") {
		t.Fatalf("hosts = %v, want share.example.com", hosts)
	}
	if !contains(roots, "/srv/share") {
		t.Fatalf("roots = %v, want /srv/share", roots)
	}
}
