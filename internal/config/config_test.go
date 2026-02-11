package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	config := Default()

	// LLM defaults
	if config.LLM.Provider != "" {
		t.Errorf("expected empty Provider, got '%s'", config.LLM.Provider)
	}
	if config.LLM.Enabled {
		t.Error("expected LLM.Enabled to be false by default")
	}
	if !config.LLM.FallbackToRules {
		t.Error("expected LLM.FallbackToRules to be true by default")
	}
	if config.LLM.Timeout != 5*time.Second {
		t.Errorf("expected Timeout 5s, got %v", config.LLM.Timeout)
	}
	if config.LLM.ComparisonModel != "claude-3-haiku-20240307" {
		t.Errorf("expected ComparisonModel 'claude-3-haiku-20240307', got '%s'", config.LLM.ComparisonModel)
	}

	// Deduplication defaults
	if config.Deduplication.AutoMerge {
		t.Error("expected AutoMerge to be false by default")
	}
	if config.Deduplication.SimilarityThreshold != 0.95 {
		t.Errorf("expected SimilarityThreshold 0.95, got %f", config.Deduplication.SimilarityThreshold)
	}

	// Logging defaults
	if config.Logging.Level != "info" {
		t.Errorf("expected Logging.Level 'info', got '%s'", config.Logging.Level)
	}
}

func TestLoadFromFile(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
llm:
  provider: anthropic
  api_key: test-key
  comparison_model: claude-3-opus
  timeout: 10s
  enabled: true
  fallback_to_rules: false

deduplication:
  auto_merge: true
  similarity_threshold: 0.85
`
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	config, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if config.LLM.Provider != "anthropic" {
		t.Errorf("expected Provider 'anthropic', got '%s'", config.LLM.Provider)
	}
	if config.LLM.APIKey != "test-key" {
		t.Errorf("expected APIKey 'test-key', got '%s'", config.LLM.APIKey)
	}
	if !config.LLM.Enabled {
		t.Error("expected Enabled to be true")
	}
	if config.LLM.FallbackToRules {
		t.Error("expected FallbackToRules to be false")
	}
	if config.Deduplication.AutoMerge != true {
		t.Error("expected AutoMerge to be true")
	}
	if config.Deduplication.SimilarityThreshold != 0.85 {
		t.Errorf("expected SimilarityThreshold 0.85, got %f", config.Deduplication.SimilarityThreshold)
	}
}

func TestLoadFromFile_EnvExpansion(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
llm:
  provider: anthropic
  api_key: ${TEST_API_KEY}
`
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Set the env var
	os.Setenv("TEST_API_KEY", "expanded-key-value")
	defer os.Unsetenv("TEST_API_KEY")

	config, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if config.LLM.APIKey != "expanded-key-value" {
		t.Errorf("expected APIKey 'expanded-key-value', got '%s'", config.LLM.APIKey)
	}
}

func TestEnvOverrides(t *testing.T) {
	// Save and restore env vars
	origProvider := os.Getenv("FLOOP_LLM_PROVIDER")
	origEnabled := os.Getenv("FLOOP_LLM_ENABLED")
	origAutoMerge := os.Getenv("FLOOP_AUTO_MERGE")
	origThreshold := os.Getenv("FLOOP_SIMILARITY_THRESHOLD")
	defer func() {
		os.Setenv("FLOOP_LLM_PROVIDER", origProvider)
		os.Setenv("FLOOP_LLM_ENABLED", origEnabled)
		os.Setenv("FLOOP_AUTO_MERGE", origAutoMerge)
		os.Setenv("FLOOP_SIMILARITY_THRESHOLD", origThreshold)
	}()

	// Set env vars
	os.Setenv("FLOOP_LLM_PROVIDER", "openai")
	os.Setenv("FLOOP_LLM_ENABLED", "true")
	os.Setenv("FLOOP_AUTO_MERGE", "true")
	os.Setenv("FLOOP_SIMILARITY_THRESHOLD", "0.8")

	config := Default()
	applyEnvOverrides(config)

	if config.LLM.Provider != "openai" {
		t.Errorf("expected Provider 'openai', got '%s'", config.LLM.Provider)
	}
	if !config.LLM.Enabled {
		t.Error("expected Enabled to be true")
	}
	if !config.Deduplication.AutoMerge {
		t.Error("expected AutoMerge to be true")
	}
	if config.Deduplication.SimilarityThreshold != 0.8 {
		t.Errorf("expected SimilarityThreshold 0.8, got %f", config.Deduplication.SimilarityThreshold)
	}
}

func TestValidate_Valid(t *testing.T) {
	config := Default()
	if err := config.Validate(); err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}
}

func TestValidate_InvalidThreshold(t *testing.T) {
	tests := []struct {
		name      string
		threshold float64
	}{
		{"negative", -0.1},
		{"greater than 1", 1.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Default()
			config.Deduplication.SimilarityThreshold = tt.threshold
			if err := config.Validate(); err == nil {
				t.Error("expected validation error for invalid threshold")
			}
		})
	}
}

func TestValidate_InvalidProvider(t *testing.T) {
	config := Default()
	config.LLM.Provider = "invalid-provider"
	if err := config.Validate(); err == nil {
		t.Error("expected validation error for invalid provider")
	}
}

func TestValidate_ValidProviders(t *testing.T) {
	validProviders := []string{"", "anthropic", "openai", "subagent"}

	for _, provider := range validProviders {
		t.Run(provider, func(t *testing.T) {
			config := Default()
			config.LLM.Provider = provider
			if err := config.Validate(); err != nil {
				t.Errorf("expected provider '%s' to be valid, got error: %v", provider, err)
			}
		})
	}
}

func TestRedactedAPIKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{"empty", "", ""},
		{"short", "abc", "(set)"},
		{"exactly 11 chars", "abcdefghijk", "(set)"},
		{"exactly 12 chars", "abcdefghijkl", "abcd...ijkl"},
		{"normal", "sk-ant-api03-abcdefghijklmnop", "sk-a...mnop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := LLMConfig{APIKey: tt.key}
			got := cfg.RedactedAPIKey()
			if got != tt.want {
				t.Errorf("RedactedAPIKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLLMConfigString(t *testing.T) {
	cfg := LLMConfig{
		Provider:        "anthropic",
		APIKey:          "sk-ant-api03-secretkey1234567890",
		ComparisonModel: "claude-3-haiku",
		Enabled:         true,
	}

	s := cfg.String()

	// Must not contain the full API key
	if strings.Contains(s, cfg.APIKey) {
		t.Errorf("String() must not contain full API key, got: %s", s)
	}

	// Must contain the redacted version
	if !strings.Contains(s, cfg.RedactedAPIKey()) {
		t.Errorf("String() should contain redacted key %q, got: %s", cfg.RedactedAPIKey(), s)
	}

	// Must contain provider and model info
	if !strings.Contains(s, "anthropic") {
		t.Errorf("String() should contain provider, got: %s", s)
	}
	if !strings.Contains(s, "claude-3-haiku") {
		t.Errorf("String() should contain model, got: %s", s)
	}
}

func TestEnvOverrides_LogLevel(t *testing.T) {
	origLogLevel := os.Getenv("FLOOP_LOG_LEVEL")
	defer os.Setenv("FLOOP_LOG_LEVEL", origLogLevel)

	os.Setenv("FLOOP_LOG_LEVEL", "debug")

	config := Default()
	applyEnvOverrides(config)

	if config.Logging.Level != "debug" {
		t.Errorf("expected Logging.Level 'debug', got '%s'", config.Logging.Level)
	}
}

func TestLoadFromFile_LoggingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
logging:
  level: trace
`
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	config, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if config.Logging.Level != "trace" {
		t.Errorf("expected Logging.Level 'trace', got '%s'", config.Logging.Level)
	}
}

func TestValidate_InvalidLogLevel(t *testing.T) {
	config := Default()
	config.Logging.Level = "verbose"
	if err := config.Validate(); err == nil {
		t.Error("expected validation error for invalid log level")
	}
}

func TestValidate_ValidLogLevels(t *testing.T) {
	validLevels := []string{"", "info", "debug", "trace"}

	for _, level := range validLevels {
		t.Run(level, func(t *testing.T) {
			config := Default()
			config.Logging.Level = level
			if err := config.Validate(); err != nil {
				t.Errorf("expected log level '%s' to be valid, got error: %v", level, err)
			}
		})
	}
}

func TestLoadFromFile_NotFound(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadFromFile_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	invalidYAML := `
llm:
  provider: [invalid yaml
`
	if err := os.WriteFile(configPath, []byte(invalidYAML), 0600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := LoadFromFile(configPath)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}
