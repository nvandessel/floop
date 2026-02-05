package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/hooks"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize feedback loop tracking in current directory",
		Long: `Initialize feedback loop tracking and optionally configure AI tool hooks.

This command creates the .floop/ directory for storing behaviors and corrections.
By default, it also detects AI coding tools (Claude Code, etc.) and configures
hooks to auto-inject behaviors at session start.

Examples:
  floop init                        # Initialize with auto-detected hooks
  floop init --hooks=false          # Initialize without configuring hooks
  floop init --global               # Initialize global user directory
  floop init --platform "Claude Code"  # Only configure Claude Code hooks`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			globalInit, _ := cmd.Flags().GetBool("global")
			configureHooks, _ := cmd.Flags().GetBool("hooks")
			platformFilter, _ := cmd.Flags().GetString("platform")
			jsonOut, _ := cmd.Flags().GetBool("json")

			var floopDir string
			var hookRoot string

			if globalInit {
				// Initialize global directory
				if err := store.EnsureGlobalFloopDir(); err != nil {
					return fmt.Errorf("failed to initialize global directory: %w", err)
				}
				var err error
				floopDir, err = store.GlobalFloopPath()
				if err != nil {
					return fmt.Errorf("failed to get global path: %w", err)
				}
				// For global init, configure hooks in home directory
				hookRoot, _ = os.UserHomeDir()
			} else {
				// Initialize local directory (default)
				floopDir = filepath.Join(root, ".floop")
				hookRoot = root
			}

			// Create .floop directory
			if err := os.MkdirAll(floopDir, 0755); err != nil {
				return fmt.Errorf("failed to create .floop directory: %w", err)
			}

			// Create manifest.yaml
			manifestPath := filepath.Join(floopDir, "manifest.yaml")
			if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
				manifest := `# Feedback Loop Manifest
version: "1.0"
created: %s

# Behaviors learned from corrections are stored in this directory
# Run 'floop list' to see all behaviors
# Run 'floop active' to see behaviors active in current context
`
				content := fmt.Sprintf(manifest, time.Now().Format(time.RFC3339))
				if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
					return fmt.Errorf("failed to create manifest.yaml: %w", err)
				}
			}

			// Create corrections log for dogfooding
			correctionsPath := filepath.Join(floopDir, "corrections.jsonl")
			if _, err := os.Stat(correctionsPath); os.IsNotExist(err) {
				if err := os.WriteFile(correctionsPath, []byte{}, 0644); err != nil {
					return fmt.Errorf("failed to create corrections.jsonl: %w", err)
				}
			}

			// Prepare result for JSON output
			result := map[string]interface{}{
				"status": "initialized",
				"path":   floopDir,
			}
			if globalInit {
				result["scope"] = string(constants.ScopeGlobal)
			}

			// Human-readable output for floop init
			if !jsonOut {
				fmt.Printf("Created %s\n", floopDir)
			}

			// Configure AI tool hooks if enabled
			var hookResults []hooks.ConfigureResult
			if configureHooks {
				hookResults = configureAIToolHooks(hookRoot, platformFilter, jsonOut)
				if len(hookResults) > 0 {
					result["hooks"] = hookResults
				}
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(result)
			} else if configureHooks && len(hookResults) == 0 {
				fmt.Println("\nNo AI tools detected. Hooks not configured.")
				fmt.Println("To manually configure hooks later, ensure .claude/ exists and run 'floop init' again.")
			}

			return nil
		},
	}

	cmd.Flags().Bool("global", false, "Initialize global user directory (~/.floop/) instead of local project directory")
	cmd.Flags().Bool("hooks", true, "Configure AI tool hooks for auto-injection (default: true)")
	cmd.Flags().String("platform", "", "Only configure hooks for specific platform (e.g., 'Claude Code')")

	return cmd
}

// configureAIToolHooks detects AI tools and configures hooks for behavior injection.
func configureAIToolHooks(projectRoot string, platformFilter string, jsonOut bool) []hooks.ConfigureResult {
	// Detect platforms
	detected := hooks.DetectAll(projectRoot)

	if len(detected) == 0 {
		return nil
	}

	if !jsonOut {
		fmt.Println("\nDetected AI tools:")
		for _, d := range detected {
			status := ""
			if d.HasHooks {
				status = " (hooks already configured)"
			}
			fmt.Printf("  - %s%s\n", d.Name, status)
		}
		fmt.Println("\nConfiguring hooks...")
	}

	var results []hooks.ConfigureResult
	for _, d := range detected {
		// Filter by platform if specified
		if platformFilter != "" && d.Name != platformFilter {
			continue
		}

		result := hooks.ConfigurePlatform(d.Platform, projectRoot)
		results = append(results, result)

		if !jsonOut {
			if result.Error != nil {
				fmt.Printf("  - %s: ERROR - %v\n", result.Platform, result.Error)
			} else if result.Skipped {
				fmt.Printf("  - %s: skipped (%s)\n", result.Platform, result.SkipReason)
			} else if result.Created {
				fmt.Printf("  - %s: created %s\n", result.Platform, result.ConfigPath)
			} else {
				fmt.Printf("  - %s: updated %s\n", result.Platform, result.ConfigPath)
			}
		}
	}

	if !jsonOut && len(results) > 0 {
		fmt.Println("\nBehaviors will auto-inject at session start.")
	}

	return results
}
