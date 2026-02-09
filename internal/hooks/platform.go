// Package hooks provides platform-specific hook integration for AI coding tools.
// It enables automatic behavior injection at session start via native hook mechanisms.
package hooks

import (
	"fmt"
	"sync"
)

// Platform defines the interface for AI tool platform integration.
// Each platform (Claude Code, Codex, OpenCode, etc.) implements this interface
// to enable automatic behavior injection via platform-native hooks.
type Platform interface {
	// Name returns the human-readable name of the platform.
	Name() string

	// Detect checks if the platform is configured in the project.
	// projectRoot is the root directory of the project to check.
	Detect(projectRoot string) bool

	// ConfigPath returns the path to the platform's configuration file
	// relative to projectRoot.
	ConfigPath(projectRoot string) string

	// ReadConfig reads and parses the existing configuration.
	// Returns nil if no config exists yet.
	ReadConfig(projectRoot string) (map[string]interface{}, error)

	// GenerateHookConfig generates the hook configuration to inject behaviors.
	// existingConfig is the current config (may be nil if none exists).
	// scope controls path generation (global = absolute, project = relative).
	// hookDir is the directory containing the extracted hook scripts.
	// Returns the merged configuration with floop hooks added.
	GenerateHookConfig(existingConfig map[string]interface{}, scope HookScope, hookDir string) (map[string]interface{}, error)

	// WriteConfig writes the configuration to the platform's config file.
	WriteConfig(projectRoot string, config map[string]interface{}) error

	// HasFloopHook checks if floop hooks are already configured.
	HasFloopHook(projectRoot string) (bool, error)
}

// Registry manages registered platforms.
type Registry struct {
	mu        sync.RWMutex
	platforms []Platform
}

// NewRegistry creates a new platform registry.
func NewRegistry() *Registry {
	return &Registry{
		platforms: make([]Platform, 0),
	}
}

// Register adds a platform to the registry.
func (r *Registry) Register(p Platform) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.platforms = append(r.platforms, p)
}

// All returns all registered platforms.
func (r *Registry) All() []Platform {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Platform, len(r.platforms))
	copy(result, r.platforms)
	return result
}

// Get returns a platform by name, or nil if not found.
func (r *Registry) Get(name string) Platform {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.platforms {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// DetectPlatforms returns all platforms detected in the project root.
func (r *Registry) DetectPlatforms(projectRoot string) []Platform {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var detected []Platform
	for _, p := range r.platforms {
		if p.Detect(projectRoot) {
			detected = append(detected, p)
		}
	}
	return detected
}

// ConfigureResult holds the result of configuring a platform.
type ConfigureResult struct {
	Platform   string
	ConfigPath string
	Created    bool // True if config was created, false if updated
	Skipped    bool // True if hooks were already configured
	SkipReason string
	Error      error
}

// ConfigurePlatform configures hooks for a single platform.
func ConfigurePlatform(p Platform, projectRoot string, scope HookScope, hookDir string) ConfigureResult {
	result := ConfigureResult{
		Platform:   p.Name(),
		ConfigPath: p.ConfigPath(projectRoot),
	}

	// Read existing config
	existingConfig, err := p.ReadConfig(projectRoot)
	if err != nil {
		result.Error = fmt.Errorf("failed to read config: %w", err)
		return result
	}

	result.Created = existingConfig == nil

	// Generate hook config (idempotent — removes old floop entries first)
	newConfig, err := p.GenerateHookConfig(existingConfig, scope, hookDir)
	if err != nil {
		result.Error = fmt.Errorf("failed to generate hook config: %w", err)
		return result
	}

	// Write config
	if err := p.WriteConfig(projectRoot, newConfig); err != nil {
		result.Error = fmt.Errorf("failed to write config: %w", err)
		return result
	}

	return result
}

// ConfigureAllDetected configures hooks for all detected platforms.
func (r *Registry) ConfigureAllDetected(projectRoot string, scope HookScope, hookDir string) []ConfigureResult {
	detected := r.DetectPlatforms(projectRoot)
	results := make([]ConfigureResult, 0, len(detected))

	for _, p := range detected {
		result := ConfigurePlatform(p, projectRoot, scope, hookDir)
		results = append(results, result)
	}

	return results
}

// DefaultRegistry is the global registry with all supported platforms.
var DefaultRegistry = NewRegistry()

// RegisterDefaultPlatforms registers all built-in platform implementations.
func RegisterDefaultPlatforms() {
	DefaultRegistry.Register(NewClaudePlatform())
}

func init() {
	RegisterDefaultPlatforms()
}
