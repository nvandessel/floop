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

	// Logging contains settings for operational and decision logging.
	Logging LoggingConfig `json:"logging" yaml:"logging"`
}

// LoggingConfig configures floop's logging behavior.
type LoggingConfig struct {
	// Level sets the log verbosity: "info" (default), "debug", or "trace".
	// "debug" enables decision logging to .floop/decisions.jsonl.
	// "trace" additionally includes full LLM prompt/response content.
	Level string `json:"level" yaml:"level"`
}

// LLMConfig configures LLM-based behavior comparison and merging.
type LLMConfig struct {
	// Provider identifies the LLM backend: "anthropic", "openai", "ollama", "subagent", "local", or "" for disabled.
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

	// LocalModelPath is the path to a GGUF model file for local text generation.
	// Only used when provider is "local". Requires building with -tags llamacpp.
	LocalModelPath string `json:"local_model_path,omitempty" yaml:"local_model_path,omitempty"`

	// LocalEmbeddingModelPath is the path to a GGUF model file for local embeddings.
	// If empty, LocalModelPath is used. Only used when provider is "local".
	LocalEmbeddingModelPath string `json:"local_embedding_model_path,omitempty" yaml:"local_embedding_model_path,omitempty"`

	// LocalGPULayers is the number of model layers to offload to GPU (0 = CPU only).
	// Only used when provider is "local".
	LocalGPULayers int `json:"local_gpu_layers,omitempty" yaml:"local_gpu_layers,omitempty"`

	// LocalContextSize is the context window size in tokens for local models.
	// Defaults to 512 if not set. Only used when provider is "local".
	LocalContextSize int `json:"local_context_size,omitempty" yaml:"local_context_size,omitempty"`
}

// RedactedAPIKey returns the API key with most characters masked.
// Shows first 4 and last 4 characters, e.g., "sk-a...xyz9".
// Returns "" for empty keys and "(set)" for keys shorter than 12 chars.
func (c LLMConfig) RedactedAPIKey() string {
	if c.APIKey == "" {
		return ""
	}
	if len(c.APIKey) < 12 {
		return "(set)"
	}
	return c.APIKey[:4] + "..." + c.APIKey[len(c.APIKey)-4:]
}

// String implements fmt.Stringer to prevent accidental API key logging.
// It returns a representation with the API key redacted.
func (c LLMConfig) String() string {
	return fmt.Sprintf("LLMConfig{Provider:%s, Enabled:%t, APIKey:%s, Model:%s}",
		c.Provider, c.Enabled, c.RedactedAPIKey(), c.ComparisonModel)
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
		Logging: LoggingConfig{
			Level: "info",
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

	validProviders := map[string]bool{"": true, "anthropic": true, "openai": true, "ollama": true, "subagent": true, "local": true}
	if !validProviders[c.LLM.Provider] {
		return fmt.Errorf("invalid provider: %s (valid: anthropic, openai, ollama, subagent, local, or empty)", c.LLM.Provider)
	}

	validLevels := map[string]bool{"info": true, "debug": true, "trace": true}
	if c.Logging.Level != "" && !validLevels[c.Logging.Level] {
		return fmt.Errorf("invalid log level: %s (valid: info, debug, trace, or empty for default)", c.Logging.Level)
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

	// Local model config from environment
	if v := os.Getenv("FLOOP_LOCAL_MODEL_PATH"); v != "" {
		config.LLM.LocalModelPath = v
	}
	if v := os.Getenv("FLOOP_LOCAL_EMBEDDING_MODEL_PATH"); v != "" {
		config.LLM.LocalEmbeddingModelPath = v
	}
	if v := os.Getenv("FLOOP_LOCAL_GPU_LAYERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			config.LLM.LocalGPULayers = n
		}
	}
	if v := os.Getenv("FLOOP_LOCAL_CONTEXT_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			config.LLM.LocalContextSize = n
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

	if v := os.Getenv("FLOOP_LOG_LEVEL"); v != "" {
		config.Logging.Level = v
	}
}

// expandEnvVars expands ${VAR} patterns in a string with environment variable values.
func expandEnvVars(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return os.Expand(s, os.Getenv)
}
