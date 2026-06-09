package domain

import (
	"fmt"
	"strings"
)

// NormalizeObjectPath canonicalizes a curated object path for manifest and symlink use.
// Paths are relative to the site root, use forward slashes, and must not contain "..".
func NormalizeObjectPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("object path is required")
	}

	normalized := strings.ReplaceAll(trimmed, "\\", "/")
	parts := strings.Split(normalized, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return "", fmt.Errorf("object path cannot contain %q", part)
		}
		if len(clean) == 0 {
			if prefix, ok := reservedObjectPathPrefixes[part]; ok {
				return "", fmt.Errorf("object path cannot use reserved prefix %q", prefix)
			}
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return "", fmt.Errorf("object path is required")
	}
	return strings.Join(clean, "/"), nil
}

var reservedObjectPathPrefixes = map[string]string{
	".mason":  ".mason/",
	"objects": "objects/",
	"s":       "s/",
}

// ResolveSharePath resolves the object path for mason share.
// With no --as, the file lands at defaultPath/filename; when defaultPath is empty,
// filename alone is used. A trailing slash on --as means "into this directory, keep
// the filename"; otherwise --as is the exact object path.
func ResolveSharePath(as, defaultPath, filename string) (string, error) {
	name := strings.TrimSpace(filename)
	if name == "" {
		return "", fmt.Errorf("filename is required")
	}

	as = strings.TrimSpace(as)
	if as != "" {
		as = strings.ReplaceAll(as, "\\", "/")
		if strings.HasSuffix(as, "/") {
			dir := strings.TrimSuffix(as, "/")
			return joinObjectPath(dir, name)
		}
		return NormalizeObjectPath(as)
	}

	defaultPath = strings.TrimSpace(defaultPath)
	if defaultPath != "" {
		return joinObjectPath(defaultPath, name)
	}
	return NormalizeObjectPath(name)
}

func joinObjectPath(dir, name string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return NormalizeObjectPath(name)
	}
	normalizedDir, err := NormalizeObjectPath(dir)
	if err != nil {
		return "", err
	}
	normalizedName, err := NormalizeObjectPath(name)
	if err != nil {
		return "", err
	}
	return normalizedDir + "/" + normalizedName, nil
}
