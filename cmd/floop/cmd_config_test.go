package main

import (
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/backup"
	"github.com/nvandessel/floop/internal/config"
)

func TestValueOrDefault(t *testing.T) {
	tests := []struct {
		name         string
		value        string
		defaultValue string
		want         string
	}{
		{"non-empty returns value", "anthropic", "(not set)", "anthropic"},
		{"empty returns default", "", "(not set)", "(not set)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := valueOrDefault(tt.value, tt.defaultValue)
			if got != tt.want {
				t.Errorf("valueOrDefault(%q, %q) = %q, want %q", tt.value, tt.defaultValue, got, tt.want)
			}
		})
	}
}

func TestGetConfigValue(t *testing.T) {
	cfg := &config.FloopConfig{
		LLM: config.LLMConfig{
			Provider:        "anthropic",
			Enabled:         true,
			FallbackToRules: true,
			Timeout:         30 * time.Second,
		},
		Deduplication: config.DeduplicationConfig{
			AutoMerge:           true,
			SimilarityThreshold: 0.85,
		},
	}

	tests := []struct {
		name      string
		key       string
		wantFound bool
	}{
		{"llm.provider", "llm.provider", true},
		{"llm.enabled", "llm.enabled", true},
		{"llm.fallback_to_rules", "llm.fallback_to_rules", true},
		{"llm.timeout", "llm.timeout", true},
		{"llm.api_key", "llm.api_key", true},
		{"llm.base_url", "llm.base_url", true},
		{"llm.comparison_model", "llm.comparison_model", true},
		{"llm.merge_model", "llm.merge_model", true},
		{"deduplication.auto_merge", "deduplication.auto_merge", true},
		{"deduplication.similarity_threshold", "deduplication.similarity_threshold", true},
		{"unknown key", "nonexistent.key", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, found := getConfigValue(cfg, tt.key)
			if found != tt.wantFound {
				t.Errorf("getConfigValue(_, %q) found = %v, want %v", tt.key, found, tt.wantFound)
			}
			if tt.wantFound && value == nil {
				t.Errorf("getConfigValue(_, %q) returned nil value when found", tt.key)
			}
		})
	}

	// Check specific values
	val, _ := getConfigValue(cfg, "llm.provider")
	if val != "anthropic" {
		t.Errorf("llm.provider = %v, want %q", val, "anthropic")
	}

	val, _ = getConfigValue(cfg, "llm.enabled")
	if val != true {
		t.Errorf("llm.enabled = %v, want true", val)
	}

	val, _ = getConfigValue(cfg, "deduplication.similarity_threshold")
	if val != 0.85 {
		t.Errorf("deduplication.similarity_threshold = %v, want 0.85", val)
	}
}

func TestSetConfigValue(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr bool
	}{
		{"valid provider", "llm.provider", "anthropic", false},
		{"empty provider", "llm.provider", "", false},
		{"subagent provider", "llm.provider", "subagent", false},
		{"invalid provider", "llm.provider", "invalid", true},
		{"api key", "llm.api_key", "sk-test123", false},
		{"base url", "llm.base_url", "https://api.example.com", false},
		{"comparison model", "llm.comparison_model", "claude-3-opus", false},
		{"merge model", "llm.merge_model", "claude-3-sonnet", false},
		{"valid timeout", "llm.timeout", "30s", false},
		{"invalid timeout", "llm.timeout", "invalid", true},
		{"enabled true", "llm.enabled", "true", false},
		{"enabled false", "llm.enabled", "false", false},
		{"fallback true", "llm.fallback_to_rules", "true", false},
		{"auto merge", "deduplication.auto_merge", "true", false},
		{"valid threshold", "deduplication.similarity_threshold", "0.85", false},
		{"threshold too high", "deduplication.similarity_threshold", "1.5", true},
		{"threshold too low", "deduplication.similarity_threshold", "-0.1", true},
		{"invalid threshold", "deduplication.similarity_threshold", "abc", true},
		{"unknown key", "nonexistent.key", "value", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			err := setConfigValue(cfg, tt.key, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("setConfigValue(_, %q, %q) err = %v, wantErr %v", tt.key, tt.value, err, tt.wantErr)
			}
		})
	}

	// Verify values are actually set
	cfg := config.Default()
	if err := setConfigValue(cfg, "llm.provider", "openai"); err != nil {
		t.Fatalf("setConfigValue(llm.provider) failed: %v", err)
	}
	if cfg.LLM.Provider != "openai" {
		t.Errorf("after set, llm.provider = %q, want %q", cfg.LLM.Provider, "openai")
	}

	if err := setConfigValue(cfg, "llm.timeout", "1m"); err != nil {
		t.Fatalf("setConfigValue(llm.timeout) failed: %v", err)
	}
	if cfg.LLM.Timeout != time.Minute {
		t.Errorf("after set, llm.timeout = %v, want %v", cfg.LLM.Timeout, time.Minute)
	}

	if err := setConfigValue(cfg, "llm.enabled", "1"); err != nil {
		t.Fatalf("setConfigValue(llm.enabled) failed: %v", err)
	}
	if !cfg.LLM.Enabled {
		t.Error("after set llm.enabled=1, expected true")
	}

	if err := setConfigValue(cfg, "deduplication.similarity_threshold", "0.75"); err != nil {
		t.Fatalf("setConfigValue(deduplication.similarity_threshold) failed: %v", err)
	}
	if cfg.Deduplication.SimilarityThreshold != 0.75 {
		t.Errorf("after set, threshold = %f, want 0.75", cfg.Deduplication.SimilarityThreshold)
	}
}

func TestNewConfigCmd(t *testing.T) {
	cmd := newConfigCmd()
	if cmd.Use != "config" {
		t.Errorf("Use = %q, want %q", cmd.Use, "config")
	}

	// Verify subcommands
	subCmds := cmd.Commands()
	names := make(map[string]bool)
	for _, sub := range subCmds {
		names[sub.Name()] = true
	}
	for _, expected := range []string{"list", "get", "set"} {
		if !names[expected] {
			t.Errorf("missing %q subcommand", expected)
		}
	}
}

func TestBuildRetentionPolicy(t *testing.T) {
	t.Run("empty config uses default count policy", func(t *testing.T) {
		cfg := &config.BackupConfig{}
		policy := buildRetentionPolicy(cfg)
		if policy == nil {
			t.Fatal("expected non-nil policy")
		}
		// Should be a CountPolicy with default count
		if _, ok := policy.(*backup.CountPolicy); !ok {
			t.Errorf("expected *backup.CountPolicy, got %T", policy)
		}
	})

	t.Run("count only returns CountPolicy", func(t *testing.T) {
		cfg := &config.BackupConfig{
			Retention: config.RetentionConfig{MaxCount: 5},
		}
		policy := buildRetentionPolicy(cfg)
		cp, ok := policy.(*backup.CountPolicy)
		if !ok {
			t.Fatalf("expected *backup.CountPolicy, got %T", policy)
		}
		if cp.MaxCount != 5 {
			t.Errorf("MaxCount = %d, want 5", cp.MaxCount)
		}
	})

	t.Run("count and age returns CompositePolicy", func(t *testing.T) {
		cfg := &config.BackupConfig{
			Retention: config.RetentionConfig{
				MaxCount: 10,
				MaxAge:   "7d",
			},
		}
		policy := buildRetentionPolicy(cfg)
		if _, ok := policy.(*backup.CompositePolicy); !ok {
			t.Errorf("expected *backup.CompositePolicy, got %T", policy)
		}
	})

	t.Run("invalid age is ignored", func(t *testing.T) {
		cfg := &config.BackupConfig{
			Retention: config.RetentionConfig{
				MaxCount: 5,
				MaxAge:   "invalid",
			},
		}
		policy := buildRetentionPolicy(cfg)
		// Should fall back to single CountPolicy since age parse fails
		if _, ok := policy.(*backup.CountPolicy); !ok {
			t.Errorf("expected *backup.CountPolicy (invalid age ignored), got %T", policy)
		}
	})
}
