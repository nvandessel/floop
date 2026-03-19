package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/nvandessel/floop/internal/config"
	"github.com/nvandessel/floop/internal/llm"
	"github.com/spf13/cobra"
)

// createLLMClient creates an LLM client based on config settings.
// Returns nil if LLM is not enabled or configured.
// Supports providers: anthropic, openai, ollama, subagent, local.
// When provider is "subagent" or not specified with LLM enabled, attempts auto-detection.
// An optional timeout overrides the default for subagent and API clients.
func createLLMClient(cfg *config.FloopConfig, timeout ...time.Duration) llm.Client {
	if cfg == nil {
		return nil
	}

	// resolveTimeout returns the explicit override or falls back to config/default.
	resolveTimeout := func(fallback time.Duration) time.Duration {
		if len(timeout) > 0 && timeout[0] > 0 {
			return timeout[0]
		}
		if fallback > 0 {
			return fallback
		}
		return 30 * time.Second
	}

	// If no explicit provider but LLM is enabled, try subagent auto-detection
	if cfg.LLM.Enabled && cfg.LLM.Provider == "" {
		subCfg := llm.DefaultSubagentConfig()
		subCfg.Timeout = resolveTimeout(subCfg.Timeout)
		client := llm.NewSubagentClient(subCfg)
		if client.Available() {
			return client
		}
		return nil
	}

	if !cfg.LLM.Enabled || cfg.LLM.Provider == "" {
		return nil
	}

	clientCfg := llm.ClientConfig{
		Provider: cfg.LLM.Provider,
		APIKey:   cfg.LLM.APIKey,
		BaseURL:  cfg.LLM.BaseURL,
		Model:    cfg.LLM.ComparisonModel,
		Timeout:  resolveTimeout(cfg.LLM.Timeout),
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
		subCfg := llm.DefaultSubagentConfig()
		subCfg.Timeout = resolveTimeout(subCfg.Timeout)
		client := llm.NewSubagentClient(subCfg)
		if client.Available() {
			return client
		}
		return nil
	default:
		return nil
	}
}

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// resolveVersion populates version, commit, and date from Go module build info
// when ldflags have not been set. This enables proper version reporting for
// binaries installed via "go install".
func resolveVersion() {
	if version != "dev" {
		return
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if len(setting.Value) >= 7 {
				commit = setting.Value[:7]
			} else if setting.Value != "" {
				commit = setting.Value
			}
		case "vcs.time":
			date = setting.Value
		}
	}
}

// versionString formats version information for display.
func versionString() string {
	return fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date)
}

func main() {
	resolveVersion()

	rootCmd := &cobra.Command{
		Use:     "floop",
		Short:   "Feedback loop - behavior learning for AI agents",
		Version: versionString(),
		Long: `floop manages learned behaviors and conventions for AI coding agents.

It captures corrections, extracts reusable behaviors, and provides
context-aware behavior activation for consistent agent operation.`,
	}

	rootCmd.SetVersionTemplate("floop version {{.Version}}\n")

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
		newPackCmd(),
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
		// Memory consolidation commands
		newIngestCmd(),
		newConsolidateCmd(),
		newEventsCmd(),
		newMigrateCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
