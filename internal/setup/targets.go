package setup

import (
	"fmt"
	"sort"
	"strings"

	"github.com/norlinga/mason/internal/config"
)

// TargetSummary is one row for `target ls`.
type TargetSummary struct {
	Name      string
	Host      string
	User      string
	BaseURL   string
	IsDefault bool
}

// ListTargets returns target summaries sorted by name, marking the default.
func ListTargets(cfg *config.Config) []TargetSummary {
	if cfg == nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Targets))
	for n := range cfg.Targets {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]TargetSummary, 0, len(names))
	for _, n := range names {
		t := cfg.Targets[n]
		out = append(out, TargetSummary{
			Name:      n,
			Host:      t.Host,
			User:      t.User,
			BaseURL:   t.BaseURL,
			IsDefault: n == cfg.Default,
		})
	}
	return out
}

// AddTarget inserts a new target, failing if the name already exists. It becomes the
// default when makeDefault is set or no default exists yet. The config is validated.
func AddTarget(cfg *config.Config, name string, t config.Target, makeDefault bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("target name is required")
	}
	if cfg.Targets == nil {
		cfg.Targets = map[string]config.Target{}
	}
	if _, exists := cfg.Targets[name]; exists {
		return fmt.Errorf("target %q already exists", name)
	}
	cfg.Targets[name] = t
	if makeDefault || strings.TrimSpace(cfg.Default) == "" {
		cfg.Default = name
	}
	return cfg.Validate()
}

// UseTarget sets the default target, failing if it is not defined.
func UseTarget(cfg *config.Config, name string) error {
	name = strings.TrimSpace(name)
	if _, ok := cfg.Targets[name]; !ok {
		return fmt.Errorf("target %q is not defined", name)
	}
	cfg.Default = name
	return cfg.Validate()
}

// RemoveTarget deletes a target and returns the resulting default. Removing the
// current default reassigns it to the alphabetically first remaining target. It
// refuses to remove the only target.
func RemoveTarget(cfg *config.Config, name string) (string, error) {
	name = strings.TrimSpace(name)
	if _, ok := cfg.Targets[name]; !ok {
		return "", fmt.Errorf("target %q is not defined", name)
	}
	if len(cfg.Targets) == 1 {
		return "", fmt.Errorf("cannot remove the only target %q", name)
	}
	delete(cfg.Targets, name)
	if cfg.Default == name {
		names := make([]string, 0, len(cfg.Targets))
		for n := range cfg.Targets {
			names = append(names, n)
		}
		sort.Strings(names)
		cfg.Default = names[0]
	}
	return cfg.Default, cfg.Validate()
}
