package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const projectConfigName = ".mason"

// Config is Mason's YAML configuration file shape.
type Config struct {
	Default string            `yaml:"default"`
	Targets map[string]Target `yaml:"targets"`
}

// Target describes one remote destination.
type Target struct {
	Host    string       `yaml:"host"`
	User    string       `yaml:"user"`
	Root    string       `yaml:"root"`
	BaseURL string       `yaml:"base_url"`
	KeyPath string       `yaml:"key_path,omitempty"`
	Port    int          `yaml:"port,omitempty"`
	Share   ShareConfig  `yaml:"share,omitempty"`
	Deploy  DeployConfig `yaml:"deploy,omitempty"`
}

// ShareConfig contains share workflow settings.
type ShareConfig struct {
	DefaultPath string `yaml:"default_path,omitempty"`
}

// DeployConfig contains deploy workflow settings.
type DeployConfig struct {
	BuildDir   string `yaml:"build_dir,omitempty"`
	RemotePath string `yaml:"remote_path,omitempty"`
}

// ResolvedTarget is a named target selected for a command.
type ResolvedTarget struct {
	Name   string
	Target Target
}

// Parse decodes and validates YAML config bytes.
func Parse(data []byte) (*Config, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadFile reads, decodes, and validates a config file.
func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("load config %q: %w", path, err)
	}
	return cfg, nil
}

// Validate checks required config invariants.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("config is empty")
	}
	c.Default = strings.TrimSpace(c.Default)
	if c.Default == "" {
		return fmt.Errorf("config default target is required")
	}
	if len(c.Targets) == 0 {
		return fmt.Errorf("config must define at least one target")
	}
	for name, target := range c.Targets {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("target name cannot be empty")
		}
		if err := target.validate(name); err != nil {
			return err
		}
	}
	if _, ok := c.Targets[c.Default]; !ok {
		return fmt.Errorf("default target %q is not defined", c.Default)
	}
	return nil
}

func (t Target) validate(name string) error {
	required := map[string]string{
		"host":     t.Host,
		"user":     t.User,
		"root":     t.Root,
		"base_url": t.BaseURL,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("target %q requires %s", name, field)
		}
	}
	if t.Port < 0 || t.Port > 65535 {
		return fmt.Errorf("target %q has invalid port %d", name, t.Port)
	}
	return nil
}

// ResolveTarget selects the default target or an explicit override.
func (c *Config) ResolveTarget(override string) (ResolvedTarget, error) {
	if err := c.Validate(); err != nil {
		return ResolvedTarget{}, err
	}
	name := strings.TrimSpace(override)
	if name == "" {
		name = c.Default
	}
	target, ok := c.Targets[name]
	if !ok {
		return ResolvedTarget{}, fmt.Errorf("target %q is not defined", name)
	}
	return ResolvedTarget{Name: name, Target: target}, nil
}

// Discover searches upward from startDir for .mason, then falls back to ~/.mason.
func Discover(startDir string) (string, error) {
	if strings.TrimSpace(startDir) == "" {
		startDir = "."
	}

	current, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("resolve config search path: %w", err)
	}
	info, err := os.Stat(current)
	if err != nil {
		return "", fmt.Errorf("stat config search path %q: %w", current, err)
	}
	if !info.IsDir() {
		current = filepath.Dir(current)
	}

	for {
		candidate := filepath.Join(current, projectConfigName)
		if fileExists(candidate) {
			return candidate, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	candidate := filepath.Join(home, projectConfigName)
	if fileExists(candidate) {
		return candidate, nil
	}
	return "", fmt.Errorf("no .mason config found")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
