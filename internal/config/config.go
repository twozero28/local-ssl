package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigFileName = "devlink.yaml"
)

// Config represents the persisted configuration.
type Config struct {
	Projects map[string]*Project `yaml:"projects"`
}

// Project describes a single local project environment.
type Project struct {
	Domains []string `yaml:"domains"`
	Routes  []*Route `yaml:"routes"`
}

// Route describes a proxied route.
type Route struct {
	Path            string `yaml:"path"`
	Upstream        string `yaml:"upstream"`
	StripPathPrefix *bool  `yaml:"stripPathPrefix,omitempty"`
	Websocket       bool   `yaml:"websocket,omitempty"`
	SpaFallback     bool   `yaml:"spaFallback,omitempty"`
}

// New creates a default configuration instance.
func New() *Config {
	return &Config{
		Projects: map[string]*Project{},
	}
}

// Clone creates a deep copy of the config.
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	clone := New()
	for name, proj := range c.Projects {
		cloneProj := &Project{
			Domains: append([]string{}, proj.Domains...),
		}
		for _, route := range proj.Routes {
			cloneRoute := *route
			cloneProj.Routes = append(cloneProj.Routes, &cloneRoute)
		}
		clone.Projects[name] = cloneProj
	}
	return clone
}

// Load reads a configuration from disk. If the file does not exist a new
// configuration is returned.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return New(), nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := New()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if cfg.Projects == nil {
		cfg.Projects = map[string]*Project{}
	}
	return cfg, nil
}

// Save writes the configuration to disk, creating parent directories if
// necessary.
func Save(path string, cfg *Config) error {
	if cfg == nil {
		return errors.New("config is nil")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
