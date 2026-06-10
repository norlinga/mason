package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/norlinga/mason/internal/config"
	"github.com/norlinga/mason/internal/domain"
	"github.com/norlinga/mason/internal/remote"
)

// CheckStatus is the outcome of a single diagnostic.
type CheckStatus string

const (
	CheckOK   CheckStatus = "ok"
	CheckWarn CheckStatus = "warn"
	CheckFail CheckStatus = "fail"
)

// DoctorCheck is one named diagnostic result.
type DoctorCheck struct {
	Name   string
	Status CheckStatus
	Detail string
}

// DoctorReport is the full set of diagnostics for a target.
type DoctorReport struct {
	Checks []DoctorCheck
}

// Healthy reports whether no check failed (warnings are tolerated).
func (r DoctorReport) Healthy() bool {
	for _, c := range r.Checks {
		if c.Status == CheckFail {
			return false
		}
	}
	return true
}

// DoctorDeps are the injectable collaborators for Diagnose.
type DoctorDeps struct {
	// Runner runs read-only remote commands (as the mason user).
	Runner remote.CommandRunner
	// Store fetches the remote manifest.
	Store remote.ManifestStore
	// HTTPGet performs a GET and returns the status code; used to verify the
	// private prefixes are not publicly reachable.
	HTTPGet func(ctx context.Context, url string) (int, error)
}

// Diagnose runs read-only diagnostics for a target and returns a report. It never
// mutates remote state; failing probes become fail/warn checks, not errors.
func Diagnose(ctx context.Context, deps DoctorDeps, target config.Target) DoctorReport {
	var checks []DoctorCheck

	checks = append(checks, checkSSH(ctx, deps.Runner))
	checks = append(checks, checkRsync(ctx, deps.Runner))

	root := target.Root
	checks = append(checks, checkDirWritable(ctx, deps.Runner, "share root", root))
	checks = append(checks, checkDirWritable(ctx, deps.Runner, "objects/", remote.RemotePath(root, "objects")))
	checks = append(checks, checkDirWritable(ctx, deps.Runner, "s/", remote.RemotePath(root, "s")))
	checks = append(checks, checkManifestPrivate(ctx, deps.Runner, remote.RemotePath(root, ".mason")))

	if rp := strings.TrimSpace(target.Deploy.RemotePath); rp != "" && rp != root {
		checks = append(checks, checkDirWritable(ctx, deps.Runner, "deploy tree", rp))
	}

	manifest, manifestCheck := checkManifest(ctx, deps.Store)
	checks = append(checks, manifestCheck)

	checks = append(checks, checkNotPublic(ctx, deps.HTTPGet, target.BaseURL, "/.mason/manifest.json"))
	checks = append(checks, checkNotPublic(ctx, deps.HTTPGet, target.BaseURL, "/objects/"))
	if manifestCheck.Status == CheckFail {
		checks = append(checks, warn(DoctorCheck{Name: "symlink serving"}, "manifest unavailable; probe skipped"))
	} else {
		checks = append(checks, checkSymlinkServing(ctx, deps.HTTPGet, target.BaseURL, manifest))
	}

	checks = append(checks, checkCaddy(ctx, deps.Runner, target)...)

	return DoctorReport{Checks: checks}
}

func checkSSH(ctx context.Context, runner remote.CommandRunner) DoctorCheck {
	c := DoctorCheck{Name: "ssh connection"}
	if runner == nil {
		return fail(c, "no ssh runner")
	}
	if _, err := runner.Run(ctx, "true"); err != nil {
		return fail(c, err.Error())
	}
	return ok(c, "reachable")
}

func checkRsync(ctx context.Context, runner remote.CommandRunner) DoctorCheck {
	c := DoctorCheck{Name: "rsync available"}
	if runner == nil {
		return fail(c, "no ssh runner")
	}
	res, err := runner.Run(ctx, "command -v rsync")
	if err != nil || res.ExitCode != 0 {
		return fail(c, "rsync not found on server (required for share/deploy transfers)")
	}
	return ok(c, strings.TrimSpace(res.Stdout))
}

func checkDirWritable(ctx context.Context, runner remote.CommandRunner, name, path string) DoctorCheck {
	c := DoctorCheck{Name: name + " writable"}
	if runner == nil {
		return fail(c, "no ssh runner")
	}
	cmd := fmt.Sprintf("test -d %s && test -w %s", shellQuote(path), shellQuote(path))
	res, err := runner.Run(ctx, cmd)
	if err != nil || res.ExitCode != 0 {
		return fail(c, fmt.Sprintf("%s missing or not writable by this user", path))
	}
	return ok(c, path)
}

func checkManifestPrivate(ctx context.Context, runner remote.CommandRunner, path string) DoctorCheck {
	c := DoctorCheck{Name: ".mason private"}
	if runner == nil {
		return fail(c, "no ssh runner")
	}
	res, err := runner.Run(ctx, fmt.Sprintf("stat -c %%a %s", shellQuote(path)))
	if err != nil || res.ExitCode != 0 {
		return fail(c, fmt.Sprintf("%s missing", path))
	}
	modeStr := strings.TrimSpace(res.Stdout)
	mode, perr := strconv.ParseInt(modeStr, 8, 32)
	if perr != nil {
		return warn(c, fmt.Sprintf("unexpected mode %q", modeStr))
	}
	if mode&0o077 != 0 {
		return warn(c, fmt.Sprintf("mode %s; want 700 so the manifest stays private", modeStr))
	}
	return ok(c, "mode "+modeStr)
}

func checkManifest(ctx context.Context, store remote.ManifestStore) (*domain.Manifest, DoctorCheck) {
	c := DoctorCheck{Name: "manifest"}
	if store == nil {
		return nil, fail(c, "no manifest store")
	}
	m, err := store.Fetch(ctx)
	if err != nil {
		return nil, fail(c, err.Error())
	}
	return m, ok(c, fmt.Sprintf("%d shares, %d blobs", len(m.Shares), len(m.Blobs)))
}

// probeURLFromManifest picks the first enabled slug URL, else the first enabled object path.
func probeURLFromManifest(m *domain.Manifest, baseURL string) (string, bool) {
	if m == nil {
		return "", false
	}
	for i := range m.Shares {
		share := &m.Shares[i]
		if share.Slug != nil && share.Slug.State == domain.EndpointOK {
			return domain.SlugURL(baseURL, share.Slug.Value, share.Slug.Ext), true
		}
	}
	for i := range m.Shares {
		share := &m.Shares[i]
		if share.Path != nil && share.Path.State == domain.EndpointOK {
			return domain.ObjectURL(baseURL, share.Path.Value), true
		}
	}
	return "", false
}

func checkSymlinkServing(ctx context.Context, get func(context.Context, string) (int, error), baseURL string, m *domain.Manifest) DoctorCheck {
	c := DoctorCheck{Name: "symlink serving"}
	if get == nil {
		return warn(c, "no http probe configured")
	}
	probeURL, found := probeURLFromManifest(m, baseURL)
	if !found {
		return warn(c, "no enabled endpoints to probe")
	}
	status, err := get(ctx, probeURL)
	if err != nil {
		return warn(c, fmt.Sprintf("could not reach %s: %v", probeURL, err))
	}
	if status < 200 || status >= 300 {
		return fail(c, fmt.Sprintf("%s returned %d; Caddy must serve manifest links via symlink", probeURL, status))
	}
	return ok(c, fmt.Sprintf("%s status %d", probeURL, status))
}

func checkNotPublic(ctx context.Context, get func(context.Context, string) (int, error), baseURL, suffix string) DoctorCheck {
	c := DoctorCheck{Name: suffix + " not public"}
	if get == nil {
		return warn(c, "no http probe configured")
	}
	target := strings.TrimRight(strings.TrimSpace(baseURL), "/") + suffix
	status, err := get(ctx, target)
	if err != nil {
		return warn(c, fmt.Sprintf("could not reach %s: %v", target, err))
	}
	if status == 200 {
		return fail(c, fmt.Sprintf("%s is publicly reachable (200); Caddy must 404 this prefix", target))
	}
	return ok(c, fmt.Sprintf("status %d", status))
}

// checkCaddy reconciles the target against Caddy's running config (best effort):
// is the host served, and from the configured root? An unreachable admin API is a
// warning, not a failure.
func checkCaddy(ctx context.Context, runner remote.CommandRunner, target config.Target) []DoctorCheck {
	hostCheck := DoctorCheck{Name: "caddy serves host"}
	rootCheck := DoctorCheck{Name: "caddy root matches"}
	if runner == nil {
		return []DoctorCheck{warn(hostCheck, "no ssh runner")}
	}
	res, err := runner.Run(ctx, "curl -s http://localhost:2019/config/")
	if err != nil || strings.TrimSpace(res.Stdout) == "" {
		msg := "Caddy admin API unavailable; reconcile skipped"
		return []DoctorCheck{warn(hostCheck, msg), warn(rootCheck, msg)}
	}

	hosts, roots := caddyHostsAndRoots([]byte(res.Stdout))
	host := hostFromBaseURL(target.BaseURL)
	if host != "" && contains(hosts, host) {
		hostCheck = ok(hostCheck, host)
	} else {
		hostCheck = warn(hostCheck, fmt.Sprintf("host %q not found in Caddy config", host))
	}
	if contains(roots, target.Root) {
		rootCheck = ok(rootCheck, target.Root)
	} else {
		rootCheck = warn(rootCheck, fmt.Sprintf("root %q not served by Caddy", target.Root))
	}
	checks := []DoctorCheck{hostCheck, rootCheck}

	// For an apex domain, site-init also provisions a www→apex redirect. Confirm Caddy
	// serves the www name (which requires a www DNS record so Caddy can get its cert).
	if isApexDomain(host) {
		wwwHost := "www." + strings.TrimPrefix(strings.ToLower(host), "www.")
		wwwCheck := DoctorCheck{Name: "caddy redirects www"}
		if contains(hosts, wwwHost) {
			wwwCheck = ok(wwwCheck, wwwHost+" → apex")
		} else {
			wwwCheck = warn(wwwCheck, fmt.Sprintf(
				"%s not served by Caddy; run 'mason site-init' to add the www→apex redirect, and add a www DNS record so Caddy can issue its certificate",
				wwwHost))
		}
		checks = append(checks, wwwCheck)
	}
	return checks
}

// caddyHostsAndRoots walks a decoded Caddy JSON config collecting every host matcher
// value and every "root" value. The key names are stable across Caddy versions.
func caddyHostsAndRoots(data []byte) (hosts, roots []string) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, nil
	}
	var walk func(interface{})
	walk = func(node interface{}) {
		switch t := node.(type) {
		case map[string]interface{}:
			if r, ok := t["root"].(string); ok {
				roots = append(roots, r)
			}
			if hl, ok := t["host"].([]interface{}); ok {
				for _, h := range hl {
					if s, ok := h.(string); ok {
						hosts = append(hosts, s)
					}
				}
			}
			for _, child := range t {
				walk(child)
			}
		case []interface{}:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(v)
	return hosts, roots
}

func hostFromBaseURL(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	if u.Host != "" {
		return u.Host
	}
	return strings.TrimSpace(baseURL)
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func ok(c DoctorCheck, detail string) DoctorCheck { c.Status = CheckOK; c.Detail = detail; return c }
func warn(c DoctorCheck, detail string) DoctorCheck {
	c.Status = CheckWarn
	c.Detail = detail
	return c
}
func fail(c DoctorCheck, detail string) DoctorCheck {
	c.Status = CheckFail
	c.Detail = detail
	return c
}
