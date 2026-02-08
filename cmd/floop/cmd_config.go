package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nvandessel/feedback-loop/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage floop configuration",
		Long: `View and modify floop configuration settings.

Configuration is stored in ~/.floop/config.yaml.

Examples:
  floop config list                            # Show all settings
  floop config get llm.provider                # Get a specific setting
  floop config set llm.provider anthropic      # Set a setting
  floop config set llm.api_key $ANTHROPIC_API_KEY`,
	}

	cmd.AddCommand(
		newConfigListCmd(),
		newConfigGetCmd(),
		newConfigSetCmd(),
	)

	return cmd
}

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all configuration settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Flags().GetBool("json")

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if jsonOut {
				// Redact API key before JSON serialization to prevent leakage
				redacted := *cfg
				redacted.LLM.APIKey = cfg.LLM.RedactedAPIKey()
				json.NewEncoder(os.Stdout).Encode(redacted)
			} else {
				fmt.Println("Configuration (~/.floop/config.yaml):")
				fmt.Println()
				fmt.Println("LLM Settings:")
				fmt.Printf("  llm.provider:          %s\n", valueOrDefault(cfg.LLM.Provider, "(not set)"))
				fmt.Printf("  llm.enabled:           %v\n", cfg.LLM.Enabled)
				redacted := cfg.LLM.RedactedAPIKey()
				if redacted != "" {
					fmt.Printf("  llm.api_key:           %s\n", redacted)
				} else {
					fmt.Printf("  llm.api_key:           (not set)\n")
				}
				fmt.Printf("  llm.base_url:          %s\n", valueOrDefault(cfg.LLM.BaseURL, "(default)"))
				fmt.Printf("  llm.comparison_model:  %s\n", valueOrDefault(cfg.LLM.ComparisonModel, "(default)"))
				fmt.Printf("  llm.merge_model:       %s\n", valueOrDefault(cfg.LLM.MergeModel, "(default)"))
				fmt.Printf("  llm.timeout:           %v\n", cfg.LLM.Timeout)
				fmt.Printf("  llm.fallback_to_rules: %v\n", cfg.LLM.FallbackToRules)
				fmt.Println()
				fmt.Println("Deduplication Settings:")
				fmt.Printf("  deduplication.auto_merge:            %v\n", cfg.Deduplication.AutoMerge)
				fmt.Printf("  deduplication.similarity_threshold:  %.2f\n", cfg.Deduplication.SimilarityThreshold)
			}

			return nil
		},
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Flags().GetBool("json")
			key := args[0]

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			value, found := getConfigValue(cfg, key)
			if !found {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error": "key not found",
						"key":   key,
					})
				} else {
					fmt.Printf("Unknown configuration key: %s\n", key)
				}
				return nil
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"key":   key,
					"value": value,
				})
			} else {
				fmt.Printf("%s = %v\n", key, value)
			}

			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Flags().GetBool("json")
			key := args[0]
			value := args[1]

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if err := setConfigValue(cfg, key, value); err != nil {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error": err.Error(),
						"key":   key,
					})
				} else {
					fmt.Printf("Error: %v\n", err)
				}
				return nil
			}

			// Save the config
			if err := saveConfig(cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"status": "updated",
					"key":    key,
					"value":  value,
				})
			} else {
				fmt.Printf("Set %s = %s\n", key, value)
			}

			return nil
		},
	}
}

// getConfigValue retrieves a configuration value by dot-notation key.
func getConfigValue(cfg *config.FloopConfig, key string) (interface{}, bool) {
	switch key {
	case "llm.provider":
		return cfg.LLM.Provider, true
	case "llm.api_key":
		return cfg.LLM.RedactedAPIKey(), true
	case "llm.base_url":
		return cfg.LLM.BaseURL, true
	case "llm.comparison_model":
		return cfg.LLM.ComparisonModel, true
	case "llm.merge_model":
		return cfg.LLM.MergeModel, true
	case "llm.timeout":
		return cfg.LLM.Timeout.String(), true
	case "llm.enabled":
		return cfg.LLM.Enabled, true
	case "llm.fallback_to_rules":
		return cfg.LLM.FallbackToRules, true
	case "deduplication.auto_merge":
		return cfg.Deduplication.AutoMerge, true
	case "deduplication.similarity_threshold":
		return cfg.Deduplication.SimilarityThreshold, true
	default:
		return nil, false
	}
}

// setConfigValue sets a configuration value by dot-notation key.
func setConfigValue(cfg *config.FloopConfig, key, value string) error {
	switch key {
	case "llm.provider":
		validProviders := map[string]bool{"": true, "anthropic": true, "openai": true, "ollama": true, "subagent": true}
		if !validProviders[value] {
			return fmt.Errorf("invalid provider: %s (valid: anthropic, openai, ollama, subagent, or empty)", value)
		}
		cfg.LLM.Provider = value
	case "llm.api_key":
		cfg.LLM.APIKey = value
	case "llm.base_url":
		cfg.LLM.BaseURL = value
	case "llm.comparison_model":
		cfg.LLM.ComparisonModel = value
	case "llm.merge_model":
		cfg.LLM.MergeModel = value
	case "llm.timeout":
		d, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid duration: %s", value)
		}
		cfg.LLM.Timeout = d
	case "llm.enabled":
		cfg.LLM.Enabled = value == "true" || value == "1"
	case "llm.fallback_to_rules":
		cfg.LLM.FallbackToRules = value == "true" || value == "1"
	case "deduplication.auto_merge":
		cfg.Deduplication.AutoMerge = value == "true" || value == "1"
	case "deduplication.similarity_threshold":
		var f float64
		if _, err := fmt.Sscanf(value, "%f", &f); err != nil {
			return fmt.Errorf("invalid threshold: %s (must be a number between 0 and 1)", value)
		}
		if f < 0 || f > 1 {
			return fmt.Errorf("threshold must be between 0 and 1, got %f", f)
		}
		cfg.Deduplication.SimilarityThreshold = f
	default:
		return fmt.Errorf("unknown configuration key: %s", key)
	}
	return nil
}

// saveConfig writes the configuration to ~/.floop/config.yaml.
func saveConfig(cfg *config.FloopConfig) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	floopDir := filepath.Join(homeDir, ".floop")
	if err := os.MkdirAll(floopDir, 0700); err != nil {
		return fmt.Errorf("failed to create .floop directory: %w", err)
	}

	configPath := filepath.Join(floopDir, "config.yaml")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// valueOrDefault returns the value if non-empty, otherwise the default.
func valueOrDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}
