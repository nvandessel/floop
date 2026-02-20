package main

import (
	"fmt"
	"os"

	"github.com/nvandessel/feedback-loop/internal/config"
	"github.com/nvandessel/feedback-loop/internal/llm"
	"github.com/spf13/cobra"
)

// createLLMClient creates an LLM client based on config settings.
// Returns nil if LLM is not enabled or configured.
// Supports providers: anthropic, openai, ollama, subagent, local.
// When provider is "subagent" or not specified with LLM enabled, attempts auto-detection.
func createLLMClient(cfg *config.FloopConfig) llm.Client {
	if cfg == nil {
		return nil
	}

	// If no explicit provider but LLM is enabled, try subagent auto-detection
	if cfg.LLM.Enabled && cfg.LLM.Provider == "" {
		if client := llm.DetectAndCreate(); client != nil {
			return client
		}
		return llm.NewFallbackClient()
	}

	if !cfg.LLM.Enabled || cfg.LLM.Provider == "" {
		return nil
	}

	clientCfg := llm.ClientConfig{
		Provider: cfg.LLM.Provider,
		APIKey:   cfg.LLM.APIKey,
		BaseURL:  cfg.LLM.BaseURL,
		Model:    cfg.LLM.ComparisonModel,
		Timeout:  cfg.LLM.Timeout,
	}

	switch cfg.LLM.Provider {
	case "ollama", "openai":
		return llm.NewOpenAIClient(clientCfg)
	case "anthropic":
		return llm.NewAnthropicClient(clientCfg)
	case "local":
		return llm.NewLocalClient(llm.LocalConfig{
			LibPath:            cfg.LLM.LocalLibPath,
			ModelPath:          cfg.LLM.LocalModelPath,
			EmbeddingModelPath: cfg.LLM.LocalEmbeddingModelPath,
			GPULayers:          cfg.LLM.LocalGPULayers,
			ContextSize:        cfg.LLM.LocalContextSize,
		})
	case "subagent":
		if client := llm.DetectAndCreate(); client != nil {
			return client
		}
		return llm.NewFallbackClient()
	default:
		return llm.NewFallbackClient()
	}
}

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "floop",
		Short: "Feedback loop - behavior learning for AI agents",
		Long: `floop manages learned behaviors and conventions for AI coding agents.

It captures corrections, extracts reusable behaviors, and provides
context-aware behavior activation for consistent agent operation.`,
	}

	// Global flags
	rootCmd.PersistentFlags().Bool("json", false, "Output as JSON (for agent consumption)")
	rootCmd.PersistentFlags().String("root", ".", "Project root directory")

	// Add subcommands
	rootCmd.AddCommand(
		newVersionCmd(),
		newInitCmd(),
		newLearnCmd(),
		newReprocessCmd(),
		newListCmd(),
		newActiveCmd(),
		newGraphCmd(),
		newShowCmd(),
		newWhyCmd(),
		newPromptCmd(),
		newMCPServerCmd(),
		// Curation commands
		newForgetCmd(),
		newDeprecateCmd(),
		newRestoreCmd(),
		newMergeCmd(),
		// Management commands
		newDeduplicateCmd(),
		newValidateCmd(),
		newConfigCmd(),
		// Token optimization commands
		newSummarizeCmd(),
		newStatsCmd(),
		// Hook support commands
		newDetectCorrectionCmd(),
		newActivateCmd(),
		// Graph management commands
		newConnectCmd(),
		newDeriveEdgesCmd(),
		// Backup/restore commands
		newBackupCmd(),
		newRestoreFromBackupCmd(),
		// Hook management commands
		newUpgradeCmd(),
		// Tag management commands
		newTagsCmd(),
		// Native hook commands (replacing shell scripts)
		newHookCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
