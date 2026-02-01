// Package config provides unified configuration loading for floop.
// It supports loading from YAML files and environment variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nvandessel/feedback-loop/internal/constants"
	"gopkg.in/yaml.v3"
)

// FloopConfig contains all floop configuration settings.
type FloopConfig struct {
	// LLM contains settings for LLM-based operations.
	LLM LLMConfig `json:"llm" yaml:"llm"`

	// Deduplication contains settings for behavior deduplication.
	Deduplication DeduplicationConfig `json:"deduplication" yaml:"deduplication"`
}

// LLMConfig configures LLM-based behavior comparison and merging.
type LLMConfig struct {
	// Provider identifies the LLM backend: "anthropic", "openai", "ollama", "subagent", or "" for disabled.
	Provider string `json:"provider" yaml:"provider"`

	// APIKey is the API key for the provider. Supports ${VAR} syntax for env vars.
	// Not required for ollama.
	APIKey string `json:"api_key,omitempty" yaml:"api_key,omitempty"`

	// BaseURL is the API endpoint URL. Used for ollama or custom OpenAI-compatible endpoints.
	// Defaults: ollama=http://localhost:11434/v1, openai=https://api.openai.com/v1
	BaseURL string `json:"base_url,omitempty" yaml:"base_url,omitempty"`

	// ComparisonModel is the model to use for behavior comparison.
	ComparisonModel string `json:"comparison_model,omitempty" yaml:"comparison_model,omitempty"`

	// MergeModel is the model to use for behavior merging (may differ from comparison).
	MergeModel string `json:"merge_model,omitempty" yaml:"merge_model,omitempty"`

	// Timeout is the maximum duration to wait for LLM responses.
	Timeout time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`

	// Enabled indicates whether LLM features are enabled.
	Enabled bool `json:"enabled" yaml:"enabled"`

	// FallbackToRules indicates whether to fall back to Jaccard similarity
	// when LLM is unavailable or fails.
	FallbackToRules bool `json:"fallback_to_rules" yaml:"fallback_to_rules"`
}

// DeduplicationConfig configures behavior deduplication.
type DeduplicationConfig struct {
	// AutoMerge enables automatic merging of detected duplicates.
	AutoMerge bool `json:"auto_merge" yaml:"auto_merge"`

	// SimilarityThreshold is the minimum similarity score for duplicate detection.
	// Range: 0.0 to 1.0
	SimilarityThreshold float64 `json:"similarity_threshold" yaml:"similarity_threshold"`
}

// Default returns a FloopConfig with sensible defaults.
func Default() *FloopConfig {
	return &FloopConfig{
		LLM: LLMConfig{
			Provider:        "",
			APIKey:          "",
			ComparisonModel: "claude-3-haiku-20240307",
			MergeModel:      "claude-3-haiku-20240307",
			Timeout:         5 * time.Second,
			Enabled:         false,
			FallbackToRules: true,
		},
		Deduplication: DeduplicationConfig{
			AutoMerge:           false,
			SimilarityThreshold: constants.DefaultSimilarityThreshold,
		},
	}
}

// Load loads configuration from the default locations and environment variables.
// Order: defaults -> ~/.floop/config.yaml -> environment variables
func Load() (*FloopConfig, error) {
	config := Default()

	// Try to load from default config file
	homeDir, err := os.UserHomeDir()
	if err == nil {
		configPath := filepath.Join(homeDir, ".floop", "config.yaml")
		if _, statErr := os.Stat(configPath); statErr == nil {
			fileConfig, loadErr := LoadFromFile(configPath)
			if loadErr != nil {
				return nil, fmt.Errorf("loading config file: %w", loadErr)
			}
			config = fileConfig
		}
	}

	// Apply environment variable overrides
	applyEnvOverrides(config)

	return config, nil
}

// LoadFromFile loads configuration from a specific YAML file.
func LoadFromFile(path string) (*FloopConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	config := Default()
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Expand environment variables in API key
	config.LLM.APIKey = expandEnvVars(config.LLM.APIKey)

	return config, nil
}

// Validate checks that the configuration is valid.
func (c *FloopConfig) Validate() error {
	if c.Deduplication.SimilarityThreshold < 0 || c.Deduplication.SimilarityThreshold > 1 {
		return fmt.Errorf("similarity_threshold must be between 0 and 1, got %f", c.Deduplication.SimilarityThreshold)
	}

	if c.LLM.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative, got %v", c.LLM.Timeout)
	}

	validProviders := map[string]bool{"": true, "anthropic": true, "openai": true, "ollama": true, "subagent": true}
	if !validProviders[c.LLM.Provider] {
		return fmt.Errorf("invalid provider: %s (valid: anthropic, openai, ollama, subagent, or empty)", c.LLM.Provider)
	}

	return nil
}

// applyEnvOverrides applies environment variable overrides to the config.
func applyEnvOverrides(config *FloopConfig) {
	if v := os.Getenv("FLOOP_LLM_PROVIDER"); v != "" {
		config.LLM.Provider = v
	}

	if v := os.Getenv("FLOOP_LLM_ENABLED"); v != "" {
		config.LLM.Enabled = v == "true" || v == "1"
	}

	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" && config.LLM.Provider == "anthropic" {
		config.LLM.APIKey = v
	}

	if v := os.Getenv("OPENAI_API_KEY"); v != "" && config.LLM.Provider == "openai" {
		config.LLM.APIKey = v
	}

	// Ollama uses OLLAMA_HOST for base URL (no API key needed)
	if config.LLM.Provider == "ollama" {
		if v := os.Getenv("OLLAMA_HOST"); v != "" {
			config.LLM.BaseURL = v
		} else if config.LLM.BaseURL == "" {
			config.LLM.BaseURL = "http://localhost:11434/v1"
		}
	}

	if v := os.Getenv("FLOOP_AUTO_MERGE"); v != "" {
		config.Deduplication.AutoMerge = v == "true" || v == "1"
	}

	if v := os.Getenv("FLOOP_SIMILARITY_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			config.Deduplication.SimilarityThreshold = f
		}
	}
}

// expandEnvVars expands ${VAR} patterns in a string with environment variable values.
func expandEnvVars(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return os.Expand(s, os.Getenv)
}
