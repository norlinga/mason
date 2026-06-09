package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/norlinga/mason/internal/config"
)

const projectConfigName = ".mason"

// ProjectConfigTemplate returns the default project .mason scaffold for deploy workflows.
func ProjectConfigTemplate() []byte {
	return []byte(`default: home

targets:
  home:
    host: example.com
    user: mason
    root: /srv/site
    base_url: https://example.com
    deploy:
      build_dir: ./public
      remote_path: /srv/site
`)
}

// InitProject writes a new .mason file in dir when one does not already exist.
func InitProject(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve project directory: %w", err)
	}

	path := filepath.Join(absDir, projectConfigName)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists", projectConfigName)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat %s: %w", projectConfigName, err)
	}

	data := ProjectConfigTemplate()
	if _, err := config.Parse(data); err != nil {
		return "", fmt.Errorf("validate project template: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", projectConfigName, err)
	}
	return path, nil
}
