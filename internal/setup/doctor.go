package setup

import (
	"context"
	"encoding/json"
	"fmt"
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
	// Group names what was checked: "server <host>", "share <host>", or "site <name>".
	Group  string
	Name   string
	Status CheckStatus
	Detail string
}

// DoctorReport is the full set of diagnostics for a doctor run.
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

// DoctorScope is what to diagnose. Each part is optional: the server checks run for
// every distinct server in scope, the share checks when Share is set, and the site checks
// when Site is set (doctor run inside a project).
type DoctorScope struct {
	Server *config.Server
	Share  *config.Share
	Site   *config.Site
}

// DoctorDeps are the injectable collaborators for Diagnose.
type DoctorDeps struct {
	// RunnerFor returns a read-only remote command runner (as the mason user) for a server.
	RunnerFor func(config.Server) remote.CommandRunner
	// Store fetches the share store's manifest.
	Store remote.ManifestStore
	// HTTPGet performs a GET and returns the status code.
	HTTPGet func(ctx context.Context, url string) (int, error)
}

// Diagnose runs read-only diagnostics for everything in scope and returns a report. It
// never mutates remote state; failing probes become fail/warn checks, not errors.
func Diagnose(ctx context.Context, deps DoctorDeps, scope DoctorScope) DoctorReport {
	var checks []DoctorCheck
	caddyConfigs := map[string]caddyView{}

	servers := scopeServers(scope)
	for _, srv := range servers {
		runner := runnerFor(deps, srv)
		group := "server " + srv.Host
		checks = append(checks, grouped(group, checkSSH(ctx, runner))...)
		checks = append(checks, grouped(group, checkRsync(ctx, runner))...)
		checks = append(checks, grouped(group, checkCaddyImport(ctx, runner))...)
		caddyConfigs[srv.String()] = loadCaddyView(ctx, runner)
	}

	if share := scope.Share; share != nil && share.Server != nil {
		runner := runnerFor(deps, *share.Server)
		group := "share " + share.Host
		root := share.Path
		var sc []DoctorCheck
		sc = append(sc, checkDirWritable(ctx, runner, "share root", root))
		sc = append(sc, checkDirWritable(ctx, runner, "objects/", remote.RemotePath(root, "objects")))
		sc = append(sc, checkDirWritable(ctx, runner, "s/", remote.RemotePath(root, "s")))
		sc = append(sc, checkManifestPrivate(ctx, runner, remote.RemotePath(root, ".mason")))
		manifest, manifestCheck := checkManifest(ctx, deps.Store)
		sc = append(sc, manifestCheck)
		sc = append(sc, checkNotPublic(ctx, deps.HTTPGet, share.URL, "/.mason/manifest.json"))
		sc = append(sc, checkNotPublic(ctx, deps.HTTPGet, share.URL, "/objects/"))
		if manifestCheck.Status == CheckFail {
			sc = append(sc, warn(DoctorCheck{Name: "symlink serving"}, "manifest unavailable; probe skipped"))
		} else {
			sc = append(sc, checkSymlinkServing(ctx, deps.HTTPGet, share.URL, manifest))
		}
		sc = append(sc, checkCaddyServes(caddyConfigs[share.Server.String()], share.Host, root, false)...)
		checks = append(checks, grouped(group, sc...)...)
	}

	if site := scope.Site; site != nil && site.Server != nil {
		runner := runnerFor(deps, *site.Server)
		group := "site " + site.Name
		var sc []DoctorCheck
		sc = append(sc, checkDirWritable(ctx, runner, "web root", site.Path))
		sc = append(sc, checkSnippet(ctx, runner, SnippetPath("", site.Host)))
		sc = append(sc, checkCaddyServes(caddyConfigs[site.Server.String()], site.Host, site.Path, isApexDomain(site.Host))...)
		sc = append(sc, checkSiteResponds(ctx, deps.HTTPGet, site.URL))
		checks = append(checks, grouped(group, sc...)...)
	}

	return DoctorReport{Checks: checks}
}

// scopeServers lists the distinct servers in scope, default server first.
func scopeServers(scope DoctorScope) []config.Server {
	var out []config.Server
	seen := map[string]bool{}
	add := func(s *config.Server) {
		if s != nil && !seen[s.String()] {
			seen[s.String()] = true
			out = append(out, *s)
		}
	}
	add(scope.Server)
	if scope.Share != nil {
		add(scope.Share.Server)
	}
	if scope.Site != nil {
		add(scope.Site.Server)
	}
	return out
}

func runnerFor(deps DoctorDeps, s config.Server) remote.CommandRunner {
	if deps.RunnerFor == nil {
		return nil
	}
	return deps.RunnerFor(s)
}

func grouped(group string, checks ...DoctorCheck) []DoctorCheck {
	for i := range checks {
		checks[i].Group = group
	}
	return checks
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

// caddyView is Caddy's running config as seen through its admin API (best effort).
type caddyView struct {
	ok           bool
	hosts, roots []string
}

func loadCaddyView(ctx context.Context, runner remote.CommandRunner) caddyView {
	if runner == nil {
		return caddyView{}
	}
	res, err := runner.Run(ctx, "curl -s http://localhost:2019/config/")
	if err != nil || strings.TrimSpace(res.Stdout) == "" {
		return caddyView{}
	}
	hosts, roots := caddyHostsAndRoots([]byte(res.Stdout))
	return caddyView{ok: true, hosts: hosts, roots: roots}
}

// checkCaddyServes reconciles a host and root against Caddy's running config. An
// unreachable admin API is a warning, not a failure. For an apex domain it also checks
// the www→apex redirect host.
func checkCaddyServes(view caddyView, host, root string, apex bool) []DoctorCheck {
	hostCheck := DoctorCheck{Name: "caddy serves host"}
	rootCheck := DoctorCheck{Name: "caddy root matches"}
	if !view.ok {
		msg := "Caddy admin API unavailable; reconcile skipped"
		return []DoctorCheck{warn(hostCheck, msg), warn(rootCheck, msg)}
	}
	if contains(view.hosts, host) {
		hostCheck = ok(hostCheck, host)
	} else {
		hostCheck = warn(hostCheck, fmt.Sprintf("host %q not found in Caddy config", host))
	}
	if contains(view.roots, root) {
		rootCheck = ok(rootCheck, root)
	} else {
		rootCheck = warn(rootCheck, fmt.Sprintf("root %q not served by Caddy", root))
	}
	checks := []DoctorCheck{hostCheck, rootCheck}

	if apex {
		wwwHost := "www." + strings.TrimPrefix(strings.ToLower(host), "www.")
		wwwCheck := DoctorCheck{Name: "caddy redirects www"}
		if contains(view.hosts, wwwHost) {
			wwwCheck = ok(wwwCheck, wwwHost+" → apex")
		} else {
			wwwCheck = warn(wwwCheck, fmt.Sprintf(
				"%s not served by Caddy; run 'mason provision' to add the www→apex redirect, and add a www DNS record so Caddy can issue its certificate",
				wwwHost))
		}
		checks = append(checks, wwwCheck)
	}
	return checks
}

// checkCaddyImport confirms the main Caddyfile imports Mason's snippet directory.
func checkCaddyImport(ctx context.Context, runner remote.CommandRunner) DoctorCheck {
	c := DoctorCheck{Name: "caddy imports mason.d"}
	if runner == nil {
		return fail(c, "no ssh runner")
	}
	glob := defaultSnippetDir + "/*.caddy"
	res, err := runner.Run(ctx, fmt.Sprintf("grep -qF %s %s", shellQuote("import "+glob), shellQuote(defaultCaddyfile)))
	if err != nil || res.ExitCode != 0 {
		return warn(c, fmt.Sprintf("%s has no 'import %s' (or is unreadable); run 'mason setup <server>'", defaultCaddyfile, glob))
	}
	return ok(c, glob)
}

// checkSnippet confirms a site's vhost exists, is Mason-managed, and hides dotfiles.
func checkSnippet(ctx context.Context, runner remote.CommandRunner, snippet string) DoctorCheck {
	c := DoctorCheck{Name: "caddy vhost"}
	if runner == nil {
		return fail(c, "no ssh runner")
	}
	res, err := runner.Run(ctx, fmt.Sprintf("test -f %[1]s && { grep -qxF %[2]s %[1]s && echo managed || echo hand; grep -qF @mason_hidden %[1]s && echo hidden || echo exposed; }",
		shellQuote(snippet), shellQuote(ManagedMarker)))
	if err != nil || res.ExitCode != 0 {
		return fail(c, fmt.Sprintf("%s missing — run 'mason provision'", snippet))
	}
	out := res.Stdout
	switch {
	case strings.Contains(out, "exposed"):
		return warn(c, fmt.Sprintf("%s does not 404 dotfiles — run 'mason provision' (add --force if hand-edited)", snippet))
	case strings.Contains(out, "hand"):
		return warn(c, fmt.Sprintf("%s is hand-edited (no '%s' line); 'mason provision' will not regenerate it", snippet, ManagedMarker))
	}
	return ok(c, snippet)
}

// checkSiteResponds GETs the site's URL; anything below 400 counts as serving.
func checkSiteResponds(ctx context.Context, get func(context.Context, string) (int, error), siteURL string) DoctorCheck {
	c := DoctorCheck{Name: "site responds"}
	if get == nil {
		return warn(c, "no http probe configured")
	}
	status, err := get(ctx, siteURL+"/")
	if err != nil {
		return fail(c, fmt.Sprintf("could not reach %s: %v", siteURL, err))
	}
	if status >= 400 {
		return fail(c, fmt.Sprintf("%s returned %d", siteURL, status))
	}
	return ok(c, fmt.Sprintf("%s status %d", siteURL, status))
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
