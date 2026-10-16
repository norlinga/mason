// Package config loads Mason's configuration (v2): the per-machine ~/.mason, which names
// the default server and the share store, merged with the nearest project .mason, which
// names the project's site(s) and build folder. See CONFIG.md for the user-facing reference.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// FileName is the config file name for both the user and project files.
	FileName = ".mason"
	// DefaultUser is the SSH user when a server destination does not name one.
	DefaultUser = "mason"
	// DefaultWebRoot is the parent of derived site and share paths (/var/www/<host>).
	DefaultWebRoot = "/var/www"
)

// Server is an SSH destination. Port and Key are optional: when zero/empty, OpenSSH
// resolves them from ~/.ssh/config, the agent, and its defaults.
type Server struct {
	User string
	Host string
	Port int
	Key  string
}

// String renders the destination as user@host, or user@host:port when a port is set.
func (s Server) String() string {
	if s.Port != 0 {
		return s.User + "@" + net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	}
	return s.User + "@" + s.Host
}

// Site is one deployable site: a public URL served from Path on Server.
type Site struct {
	// Name selects the site on the command line: its key under sites:, or its URL host.
	Name string
	// URL is scheme://host with no path.
	URL string
	// Host is the URL's lowercase hostname; it is the site's identity on the server.
	Host  string
	Path  string
	Build string
	SPA   bool
	// Server is nil when no server is configured anywhere; Config.Site reports that.
	Server *Server
}

// Share is the personal share store.
type Share struct {
	URL   string
	Host  string
	Path  string
	Inbox string
	// Server is nil when no server is configured; Config.RequireShare reports that.
	Server *Server
}

// Config is the merged result of the user file and the nearest project file.
type Config struct {
	// UserFile is the user config path, or "" when it does not exist.
	UserFile string
	// ProjectFile is the project config path, or "" when none was found.
	ProjectFile string
	// Server is the default server (project overrides user), or nil when unset.
	Server *Server
	// Build is the project's build folder as written ("" when unset).
	Build string

	share *Share
	sites []Site
	dir   string
}

// LoadOptions controls where Load looks for config files.
type LoadOptions struct {
	// Dir is where project discovery starts; "" means the current directory.
	Dir string
	// ProjectFile, when set, is used instead of discovery and must exist.
	ProjectFile string
	// UserFile overrides ~/.mason (tests, or --user-config).
	UserFile string
	// NoProject skips project discovery (validating a user file on its own).
	NoProject bool
}

// V1FormatError reports a config file in the pre-v2 targets/default shape.
type V1FormatError struct{ Path string }

func (e *V1FormatError) Error() string {
	return fmt.Sprintf("%s uses the pre-v2 config format (default:/targets:); see CONFIG.md#migrating-from-v1", e.Path)
}

// Load reads and merges the user and project config files. Missing files are not errors;
// a command that needs a site, server, or share store asks for it and gets an actionable
// error then.
func Load(opts LoadOptions) (*Config, error) {
	dir := opts.Dir
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve config search path: %w", err)
	}

	userPath := opts.UserFile
	if userPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
		userPath = filepath.Join(home, FileName)
	}

	cfg := &Config{dir: absDir}

	var user *rawFile
	if fileExists(userPath) {
		user, err = readFile(userPath, userFile)
		if err != nil {
			return nil, err
		}
		cfg.UserFile = userPath
	}

	projectPath := opts.ProjectFile
	if projectPath != "" {
		if !fileExists(projectPath) {
			return nil, fmt.Errorf("config file %s does not exist", projectPath)
		}
	} else if !opts.NoProject {
		projectPath = discoverProject(absDir, userPath)
	}
	var project *rawFile
	if projectPath != "" {
		project, err = readFile(projectPath, projectFile)
		if err != nil {
			return nil, err
		}
		cfg.ProjectFile = projectPath
	}

	if err := cfg.merge(user, project); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Sites returns the project's sites in file order (the first is the default).
func (c *Config) Sites() []Site {
	return append([]Site(nil), c.sites...)
}

// Site resolves a site by name or host; "" selects the default (first) site.
func (c *Config) Site(name string) (Site, error) {
	if len(c.sites) == 0 {
		if c.ProjectFile == "" {
			return Site{}, fmt.Errorf("no project .mason in %s or its parents — run 'mason init' in your project", c.dir)
		}
		return Site{}, fmt.Errorf("%s defines no site — add 'site: https://example.com'", c.ProjectFile)
	}

	site := c.sites[0]
	if name = strings.TrimSpace(name); name != "" {
		found := false
		for _, s := range c.sites {
			if s.Name == name || s.Host == strings.ToLower(name) || s.URL == strings.ToLower(name) {
				site, found = s, true
				break
			}
		}
		if !found {
			names := make([]string, 0, len(c.sites))
			for _, s := range c.sites {
				names = append(names, s.Name)
			}
			return Site{}, fmt.Errorf("no site %q in %s (have: %s)", name, c.ProjectFile, strings.Join(names, ", "))
		}
	}

	if site.Server == nil {
		return Site{}, fmt.Errorf("no server for %s — add 'server: <host>' to %s, or run 'mason setup <host>'", site.Name, c.ProjectFile)
	}
	return site, nil
}

// RequireShare returns the share store, or an actionable error when none is configured.
func (c *Config) RequireShare() (Share, error) {
	if c.share == nil {
		return Share{}, errors.New("no share store configured — add 'share: https://share.example.com' to ~/.mason")
	}
	if c.share.Server == nil {
		return Share{}, errors.New("no server for the share store — add 'server: <host>' to ~/.mason, or run 'mason setup <host>'")
	}
	return *c.share, nil
}

func (c *Config) merge(user, project *rawFile) error {
	if user == nil {
		user = &rawFile{}
	}
	if project == nil {
		project = &rawFile{}
	}

	userKey := user.key.value
	projectKey := project.key.value
	if projectKey == "" {
		projectKey = userKey
	}

	var userServer *Server
	if user.server.value != "" {
		s, err := parseServerAt(user.server, userKey)
		if err != nil {
			return err
		}
		userServer = &s
	}
	c.Server = userServer
	if project.server.value != "" {
		s, err := parseServerAt(project.server, projectKey)
		if err != nil {
			return err
		}
		c.Server = &s
	} else if userServer != nil && projectKey != userKey {
		s := *userServer
		s.Key = projectKey
		c.Server = &s
	}

	if user.share != nil {
		share, err := resolveShare(user.share, userServer, userKey)
		if err != nil {
			return err
		}
		c.share = &share
	}

	c.Build = project.build.value
	seenHost := map[string]string{}
	for _, rs := range project.sites {
		site, err := resolveSite(rs, c.Server, projectKey, c.Build)
		if err != nil {
			return err
		}
		if other, ok := seenHost[site.Host]; ok {
			return fmt.Errorf("%s:%d: sites %q and %q both use host %s", rs.url.file, rs.url.line, other, site.Name, site.Host)
		}
		seenHost[site.Host] = site.Name
		c.sites = append(c.sites, site)
	}
	return nil
}

func resolveSite(rs rawSite, defaultServer *Server, key, build string) (Site, error) {
	siteURL, host, err := normalizeURL(rs.url)
	if err != nil {
		return Site{}, err
	}
	name := rs.name
	if name == "" {
		name = host
	}
	server := defaultServer
	if rs.server.value != "" {
		s, err := parseServerAt(rs.server, key)
		if err != nil {
			return Site{}, err
		}
		server = &s
	}
	return Site{
		Name:   name,
		URL:    siteURL,
		Host:   host,
		Path:   pathOrDefault(rs.path.value, host),
		Build:  build,
		SPA:    rs.spa,
		Server: server,
	}, nil
}

func resolveShare(rs *rawShare, defaultServer *Server, key string) (Share, error) {
	shareURL, host, err := normalizeURL(rs.url)
	if err != nil {
		return Share{}, err
	}
	server := defaultServer
	if rs.server.value != "" {
		s, err := parseServerAt(rs.server, key)
		if err != nil {
			return Share{}, err
		}
		server = &s
	}
	return Share{
		URL:    shareURL,
		Host:   host,
		Path:   pathOrDefault(rs.path.value, host),
		Inbox:  rs.inbox.value,
		Server: server,
	}, nil
}

// NewSite builds a site from raw values using the same rules as loading a project file
// (URL normalization, /var/www/<host> path default). name "" means the URL host.
func NewSite(name, rawURL, path, build string, server *Server) (Site, error) {
	return resolveSite(rawSite{name: name, url: scalar{value: rawURL, file: "site", line: 0}, path: scalar{value: path}}, server, "", build)
}

// NewShare builds a share store from raw values using the same rules as loading the
// user file (URL normalization, /var/www/<host> path default).
func NewShare(rawURL, path string, server *Server) (Share, error) {
	return resolveShare(&rawShare{url: scalar{value: rawURL, file: "share", line: 0}, path: scalar{value: path}}, server, "")
}

func pathOrDefault(p, host string) string {
	if p = strings.TrimSpace(p); p != "" {
		return p
	}
	return path.Join(DefaultWebRoot, host)
}

// ParseServer parses an SSH destination "[user@]host[:port]" (IPv6 as "[addr]:port").
// The user defaults to DefaultUser. key is carried through unchanged.
func ParseServer(dest, key string) (Server, error) {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return Server{}, errors.New("server is empty")
	}
	if strings.Contains(dest, "://") || strings.Contains(dest, "/") {
		return Server{}, fmt.Errorf("server %q should be an SSH destination like example.com or mason@example.com:2222, not a URL", dest)
	}
	if strings.ContainsAny(dest, " \t") {
		return Server{}, fmt.Errorf("server %q contains whitespace", dest)
	}

	s := Server{User: DefaultUser, Key: strings.TrimSpace(key)}
	if i := strings.LastIndex(dest, "@"); i >= 0 {
		s.User, dest = dest[:i], dest[i+1:]
		if s.User == "" {
			return Server{}, errors.New("server has an empty user before '@'")
		}
	}

	host := dest
	switch {
	case strings.HasPrefix(dest, "[") && strings.HasSuffix(dest, "]"):
		host = strings.Trim(dest, "[]")
	case strings.HasPrefix(dest, "[") || strings.Count(dest, ":") == 1:
		h, p, err := net.SplitHostPort(dest)
		if err != nil {
			return Server{}, fmt.Errorf("server %q: %w", dest, err)
		}
		port, err := strconv.Atoi(p)
		if err != nil || port < 1 || port > 65535 {
			return Server{}, fmt.Errorf("server %q has an invalid port", dest)
		}
		host, s.Port = h, port
	}
	if host == "" {
		return Server{}, errors.New("server has no host")
	}
	s.Host = host
	return s, nil
}

func parseServerAt(v scalar, key string) (Server, error) {
	s, err := ParseServer(v.value, key)
	if err != nil {
		return Server{}, fmt.Errorf("%s:%d: %w", v.file, v.line, err)
	}
	return s, nil
}

// normalizeURL reduces a site or share URL to scheme://host, defaulting to https.
func normalizeURL(v scalar) (string, string, error) {
	raw := strings.TrimSpace(v.value)
	where := fmt.Sprintf("%s:%d", v.file, v.line)
	if v.line == 0 {
		where = v.file
	}
	if raw == "" {
		return "", "", fmt.Errorf("%s: url is empty", where)
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("%s: invalid url %q: %w", where, v.value, err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", "", fmt.Errorf("%s: url %q must use https:// (or http://)", where, v.value)
	}
	if u.Hostname() == "" {
		return "", "", fmt.Errorf("%s: url %q has no host", where, v.value)
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", "", fmt.Errorf("%s: url %q must be just scheme://host — sites are served from the root of their host", where, v.value)
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), strings.ToLower(u.Hostname()), nil
}

// discoverProject walks upward from dir for a .mason, skipping the user file itself so
// running from $HOME never mistakes ~/.mason for a project.
func discoverProject(dir, userPath string) string {
	userAbs, _ := filepath.Abs(userPath)
	current := dir
	for {
		candidate := filepath.Join(current, FileName)
		if candidate != userAbs && fileExists(candidate) {
			return candidate
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
