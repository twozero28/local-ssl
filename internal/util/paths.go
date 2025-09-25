package util

import (
	"os"
	"path/filepath"
)

const (
	defaultConfigDirName = ".devlink"
)

// ConfigPath returns the path to the configuration file. The lookup order is:
// 1. DEVLINK_CONFIG environment variable
// 2. $XDG_CONFIG_HOME/devlink/devlink.yaml
// 3. $HOME/.devlink/devlink.yaml
func ConfigPath() string {
	if env := os.Getenv("DEVLINK_CONFIG"); env != "" {
		return env
	}
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "devlink", "devlink.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", defaultConfigDirName, "devlink.yaml")
	}
	return filepath.Join(home, defaultConfigDirName, "devlink.yaml")
}

// StateDir returns a directory for runtime state (certificates, etc.).
func StateDir() string {
	if env := os.Getenv("DEVLINK_STATE_DIR"); env != "" {
		return env
	}
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, "devlink")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", defaultConfigDirName)
	}
	return filepath.Join(home, defaultConfigDirName)
}
