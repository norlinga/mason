package config

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type fileKind int

const (
	userFile fileKind = iota
	projectFile
)

// scalar is a string value with the position it came from, for error messages.
type scalar struct {
	value string
	file  string
	line  int
}

type rawSite struct {
	name   string
	url    scalar
	path   scalar
	server scalar
	spa    bool
}

type rawShare struct {
	url    scalar
	path   scalar
	inbox  scalar
	server scalar
}

type rawFile struct {
	server scalar
	key    scalar
	build  scalar
	share  *rawShare
	sites  []rawSite
}

var (
	userKeys    = []string{"server", "key", "share"}
	projectKeys = []string{"server", "key", "build", "site", "sites"}
	siteKeys    = []string{"url", "path", "server", "spa"}
	shareKeys   = []string{"url", "path", "inbox", "server"}

	// v1Renames maps v1 field names to where they live in v2, for "did you mean".
	v1Renames = map[string]string{
		"host":         "server",
		"user":         "server (as user@host)",
		"port":         "server (as host:port)",
		"key_path":     "key",
		"base_url":     "site (or share in ~/.mason)",
		"root":         "path",
		"build_dir":    "build",
		"remote_path":  "path",
		"deploy":       "build / site",
		"default_path": "inbox",
	}
)

func readFile(path string, kind fileKind) (*rawFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	f := &rawFile{}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return f, nil
	}
	root := doc.Content[0]
	if root.Kind == yaml.ScalarNode && root.Tag == "!!null" {
		return f, nil
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s:%d: expected a mapping of keys (server:, site:, …)", path, root.Line)
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if k := root.Content[i].Value; k == "targets" || k == "default" {
			return nil, &V1FormatError{Path: path}
		}
	}

	allowed := projectKeys
	if kind == userFile {
		allowed = userKeys
	}
	var sawSite, sawSites bool
	err = eachKey(path, root, allowed, misplaced(kind), func(key string, v *yaml.Node) error {
		var err error
		switch key {
		case "server":
			f.server, err = scalarAt(path, key, v)
		case "key":
			f.key, err = scalarAt(path, key, v)
		case "build":
			f.build, err = scalarAt(path, key, v)
		case "share":
			f.share, err = readShare(path, v)
		case "site":
			sawSite = true
			var s rawSite
			s, err = readSite(path, "", v)
			f.sites = append(f.sites, s)
		case "sites":
			sawSites = true
			if v.Kind != yaml.MappingNode {
				return fmt.Errorf("%s:%d: sites must map names to URLs, e.g. staging: https://staging.example.com", path, v.Line)
			}
			err = eachKey(path, v, nil, nil, func(name string, sv *yaml.Node) error {
				s, err := readSite(path, name, sv)
				f.sites = append(f.sites, s)
				return err
			})
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if sawSite && sawSites {
		return nil, fmt.Errorf("%s: use either site: or sites:, not both", path)
	}
	return f, nil
}

func readSite(path, name string, v *yaml.Node) (rawSite, error) {
	s := rawSite{name: name}
	if strings.ContainsAny(name, " \t/") {
		return s, fmt.Errorf("%s:%d: site name %q must not contain spaces or slashes", path, v.Line, name)
	}
	if v.Kind == yaml.ScalarNode {
		s.url = scalar{value: v.Value, file: path, line: v.Line}
		return s, nil
	}
	if v.Kind != yaml.MappingNode {
		return s, fmt.Errorf("%s:%d: a site is a URL or a mapping with url:", path, v.Line)
	}
	err := eachKey(path, v, siteKeys, nil, func(key string, fv *yaml.Node) error {
		var err error
		switch key {
		case "url":
			s.url, err = scalarAt(path, key, fv)
		case "path":
			s.path, err = scalarAt(path, key, fv)
		case "server":
			s.server, err = scalarAt(path, key, fv)
		case "spa":
			s.spa, err = boolAt(path, key, fv)
		}
		return err
	})
	if err == nil && s.url.value == "" {
		err = fmt.Errorf("%s:%d: site needs a url:", path, v.Line)
	}
	return s, err
}

func readShare(path string, v *yaml.Node) (*rawShare, error) {
	s := &rawShare{}
	if v.Kind == yaml.ScalarNode {
		s.url = scalar{value: v.Value, file: path, line: v.Line}
		return s, nil
	}
	if v.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s:%d: share is a URL or a mapping with url:", path, v.Line)
	}
	err := eachKey(path, v, shareKeys, nil, func(key string, fv *yaml.Node) error {
		var err error
		switch key {
		case "url":
			s.url, err = scalarAt(path, key, fv)
		case "path":
			s.path, err = scalarAt(path, key, fv)
		case "inbox":
			s.inbox, err = scalarAt(path, key, fv)
		case "server":
			s.server, err = scalarAt(path, key, fv)
		}
		return err
	})
	if err == nil && s.url.value == "" {
		err = fmt.Errorf("%s:%d: share needs a url:", path, v.Line)
	}
	return s, err
}

// eachKey visits a mapping's pairs in order, rejecting duplicates and (when allowed is
// non-nil) keys outside allowed, with a hint for misplaced and renamed keys.
func eachKey(path string, m *yaml.Node, allowed []string, misplaced map[string]string, visit func(string, *yaml.Node) error) error {
	seen := map[string]bool{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if seen[k.Value] {
			return fmt.Errorf("%s:%d: duplicate key %q", path, k.Line, k.Value)
		}
		seen[k.Value] = true
		if allowed != nil && !contains(allowed, k.Value) {
			if where, ok := misplaced[k.Value]; ok {
				return fmt.Errorf("%s:%d: %s", path, k.Line, where)
			}
			return fmt.Errorf("%s:%d: unknown key %q%s", path, k.Line, k.Value, suggest(k.Value, allowed))
		}
		if err := visit(k.Value, v); err != nil {
			return err
		}
	}
	return nil
}

func misplaced(kind fileKind) map[string]string {
	if kind == userFile {
		msg := "sites belong in a project .mason (run 'mason init' in the project), not in ~/.mason"
		return map[string]string{"site": msg, "sites": msg, "build": msg}
	}
	return map[string]string{"share": "share is configured once in ~/.mason, not in a project .mason"}
}

func suggest(key string, allowed []string) string {
	if v2, ok := v1Renames[key]; ok {
		return fmt.Sprintf(" (v1 name — in v2 this is %s; see CONFIG.md)", v2)
	}
	best, bestDist := "", 3
	for _, a := range allowed {
		if d := editDistance(key, a); d < bestDist {
			best, bestDist = a, d
		}
	}
	if best != "" {
		return fmt.Sprintf(" (did you mean %q?)", best)
	}
	sorted := append([]string(nil), allowed...)
	sort.Strings(sorted)
	return " (valid keys: " + strings.Join(sorted, ", ") + ")"
}

func scalarAt(path, key string, v *yaml.Node) (scalar, error) {
	if v.Kind != yaml.ScalarNode {
		return scalar{}, fmt.Errorf("%s:%d: %s must be a single value", path, v.Line, key)
	}
	return scalar{value: strings.TrimSpace(v.Value), file: path, line: v.Line}, nil
}

func boolAt(path, key string, v *yaml.Node) (bool, error) {
	if v.Kind == yaml.ScalarNode {
		if b, err := strconv.ParseBool(v.Value); err == nil {
			return b, nil
		}
	}
	return false, fmt.Errorf("%s:%d: %s must be true or false", path, v.Line, key)
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
