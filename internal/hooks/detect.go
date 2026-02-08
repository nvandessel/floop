package hooks

import (
	"os"
	"path/filepath"
)

// DetectionResult holds information about a detected platform.
type DetectionResult struct {
	Platform   Platform
	Name       string
	ConfigPath string
	HasHooks   bool
	Error      error
}

// DetectAll scans the project root for all supported AI tool platforms.
func DetectAll(projectRoot string) []DetectionResult {
	return DefaultRegistry.DetectAllWithStatus(projectRoot)
}

// DetectAllWithStatus scans for platforms and includes hook status.
func (r *Registry) DetectAllWithStatus(projectRoot string) []DetectionResult {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var results []DetectionResult
	for _, p := range r.platforms {
		if !p.Detect(projectRoot) {
			continue
		}

		result := DetectionResult{
			Platform:   p,
			Name:       p.Name(),
			ConfigPath: p.ConfigPath(projectRoot),
		}

		hasHook, err := p.HasFloopHook(projectRoot)
		if err != nil {
			result.Error = err
		} else {
			result.HasHooks = hasHook
		}

		results = append(results, result)
	}

	return results
}

// DetectGlobal detects platforms configured in the user's home directory.
func DetectGlobal() []DetectionResult {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return DetectAll(home)
}

// GlobalConfigPath returns the path to a platform's global config.
func GlobalConfigPath(p Platform) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return p.ConfigPath(home), nil
}

// EnsureClaudeDir creates the .claude directory if it doesn't exist.
// This is useful for projects that don't yet have Claude Code configured.
func EnsureClaudeDir(projectRoot string) error {
	claudeDir := filepath.Join(projectRoot, ".claude")
	return os.MkdirAll(claudeDir, 0700)
}

// PlatformNames returns the names of all registered platforms.
func PlatformNames() []string {
	platforms := DefaultRegistry.All()
	names := make([]string, len(platforms))
	for i, p := range platforms {
		names[i] = p.Name()
	}
	return names
}

// GetPlatformByName returns a platform by name from the default registry.
func GetPlatformByName(name string) Platform {
	return DefaultRegistry.Get(name)
}
