package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/norlinga/mason/internal/config"
)

// buildDirCandidates are common static-site output folders, in preference order.
var buildDirCandidates = []string{"dist", "public", "build", "_site", "out"}

// InitInput is what a new project file records.
type InitInput struct {
	// URL is the site's public address (bare host means https).
	URL string
	// Build is the output folder to deploy, relative to the project.
	Build string
	// Server is optional; when empty the site uses the server from ~/.mason.
	Server string
}

// DetectBuildDir returns the first common output folder that exists in dir, or "".
func DetectBuildDir(dir string) string {
	for _, name := range buildDirCandidates {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.IsDir() {
			return name
		}
	}
	return ""
}

// InitProject writes a new project .mason in dir and returns its path. The file is
// validated by loading it; an invalid file is removed and the error returned.
func InitProject(dir string, in InitInput) (string, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve project directory: %w", err)
	}
	path := filepath.Join(absDir, config.FileName)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists", path)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}

	site, err := config.NewSite("", in.URL, "", in.Build, nil)
	if err != nil {
		return "", err
	}
	build := strings.TrimPrefix(strings.TrimSpace(in.Build), "./")
	if build == "" {
		return "", fmt.Errorf("a build folder is required (the folder your site generator writes, e.g. dist)")
	}

	var b strings.Builder
	b.WriteString("# Mason project config — see CONFIG.md\n")
	if server := strings.TrimSpace(in.Server); server != "" {
		fmt.Fprintf(&b, "server: %s\n", server)
	}
	fmt.Fprintf(&b, "site: %s\n", site.URL)
	fmt.Fprintf(&b, "build: %s\n", build)

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := config.Load(config.LoadOptions{ProjectFile: path, UserFile: path + ".no-user-file"}); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
