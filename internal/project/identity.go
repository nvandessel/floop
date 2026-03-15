// Package project provides project identity resolution for floop.
package project

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the project-level floop configuration.
type Config struct {
	Project struct {
		ID   string `yaml:"id"`
		Name string `yaml:"name"`
	} `yaml:"project"`
}

// ResolveProjectID walks up from startDir looking for .floop/config.yaml
// and returns the project ID. Returns "" if no config found.
func ResolveProjectID(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}

	for {
		configPath := filepath.Join(dir, ".floop", "config.yaml")
		data, err := os.ReadFile(configPath)
		if err == nil {
			var cfg Config
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return "", fmt.Errorf("parse %s: %w", configPath, err)
			}
			return cfg.Project.ID, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("read %s: %w", configPath, err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil // reached filesystem root
		}
		dir = parent
	}
}
