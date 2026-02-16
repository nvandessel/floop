package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nvandessel/feedback-loop/internal/config"
	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/hooks"
	"github.com/nvandessel/feedback-loop/internal/seed"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize floop with hook scripts and behavior learning",
		Long: `Initialize floop by extracting hook scripts and configuring AI tool integration.

This command extracts embedded hook scripts, configures Claude Code settings,
seeds meta-behaviors, and creates the .floop/ data directory.

Interactive mode (no flags):
  Prompts for installation scope, hooks, and token budget.

Non-interactive mode (any flag provided):
  Uses flag values with sensible defaults. Suitable for scripts and agents.

Examples:
  floop init                          # Interactive setup
  floop init --global                 # Global install, all defaults
  floop init --project                # Project-level install, all defaults
  floop init --global --project       # Both scopes
  floop init --global --hooks=all --token-budget 2000  # Explicit everything`,
		RunE: func(cmd *cobra.Command, args []string) error {
			globalFlag, _ := cmd.Flags().GetBool("global")
			projectFlag, _ := cmd.Flags().GetBool("project")
			hooksFlag, _ := cmd.Flags().GetString("hooks")
			tokenBudget, _ := cmd.Flags().GetInt("token-budget")
			jsonOut, _ := cmd.Flags().GetBool("json")
			root, _ := cmd.Flags().GetString("root")

			// Determine if we're in interactive or non-interactive mode.
			// Any meaningful flag makes it non-interactive.
			interactive := !globalFlag && !projectFlag &&
				!cmd.Flags().Changed("hooks") && !cmd.Flags().Changed("token-budget") &&
				!cmd.Flags().Changed("root")

			var doGlobal, doProject bool

			if interactive {
				if jsonOut {
					return fmt.Errorf("--json requires explicit scope flags (--global and/or --project)")
				}
				var err error
				doGlobal, doProject, hooksFlag, tokenBudget, err = runInteractiveInit()
				if err != nil {
					return err
				}
			} else {
				doGlobal = globalFlag
				doProject = projectFlag
				// If neither scope specified explicitly, default to project
				if !doGlobal && !doProject {
					doProject = true
				}
				if hooksFlag == "" {
					hooksFlag = "all"
				}
			}

			result := map[string]interface{}{
				"status": "initialized",
			}

			if doGlobal {
				globalResult, err := initScope(constants.ScopeGlobal, "", hooksFlag, tokenBudget, jsonOut)
				if err != nil {
					return fmt.Errorf("global init failed: %w", err)
				}
				result["global"] = globalResult
			}

			if doProject {
				projectResult, err := initScope(constants.ScopeLocal, root, hooksFlag, tokenBudget, jsonOut)
				if err != nil {
					return fmt.Errorf("project init failed: %w", err)
				}
				result["project"] = projectResult
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(result)
			} else {
				fmt.Println("\nReady! Your AI agents will now load learned behaviors at session start.")
			}

			return nil
		},
	}

	cmd.Flags().Bool("global", false, "Install hooks globally (~/.claude/)")
	cmd.Flags().Bool("project", false, "Install hooks for this project (.claude/)")
	cmd.Flags().String("hooks", "", "Which hooks to enable: all, injection-only (default: all)")
	cmd.Flags().Int("token-budget", config.Default().TokenBudget.Default, "Token budget for behavior injection")

	return cmd
}

// initScope performs initialization for a single scope (global or project).
func initScope(scope constants.Scope, projectRoot string, hooksMode string, tokenBudget int, jsonOut bool) (map[string]interface{}, error) {
	var configRoot string // where .claude/settings.json lives
	var hookScope hooks.HookScope

	if scope == constants.ScopeGlobal {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("getting home directory: %w", err)
		}
		configRoot = homeDir
		hookScope = hooks.ScopeGlobal
	} else {
		configRoot = projectRoot
		hookScope = hooks.ScopeProject
	}

	result := map[string]interface{}{
		"scope": string(scope),
	}

	// 1. Create .floop directory
	var floopDir string
	if scope == constants.ScopeGlobal {
		if err := store.EnsureGlobalFloopDir(); err != nil {
			return nil, fmt.Errorf("creating global .floop: %w", err)
		}
		var err error
		floopDir, err = store.GlobalFloopPath()
		if err != nil {
			return nil, fmt.Errorf("getting global .floop path: %w", err)
		}
	} else {
		floopDir = filepath.Join(configRoot, ".floop")
		if err := os.MkdirAll(floopDir, 0700); err != nil {
			return nil, fmt.Errorf("creating .floop directory: %w", err)
		}
	}
	result["floop_dir"] = floopDir

	// Create manifest.yaml if it doesn't exist
	manifestPath := filepath.Join(floopDir, "manifest.yaml")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		manifest := fmt.Sprintf("# Feedback Loop Manifest\nversion: \"1.0\"\ncreated: %s\n",
			time.Now().Format(time.RFC3339))
		if err := os.WriteFile(manifestPath, []byte(manifest), 0600); err != nil {
			return nil, fmt.Errorf("creating manifest.yaml: %w", err)
		}
	}

	if !jsonOut {
		fmt.Printf("Created %s\n", floopDir)
	}

	// 2. Configure Claude Code settings.json
	// (Hook scripts are no longer extracted — native Go subcommands are used instead)
	if hooksMode != "" {
		p := hooks.NewClaudePlatform()

		// Ensure .claude directory exists
		if err := hooks.EnsureClaudeDir(configRoot); err != nil {
			return nil, fmt.Errorf("creating .claude directory: %w", err)
		}

		configResult := hooks.ConfigurePlatform(p, configRoot, hookScope, "")
		if configResult.Error != nil {
			return nil, fmt.Errorf("configuring hooks: %w", configResult.Error)
		}

		action := "Updated"
		if configResult.Created {
			action = "Created"
		}
		result["settings"] = configResult.ConfigPath

		if !jsonOut {
			fmt.Printf("%s %s\n", action, configResult.ConfigPath)
		}
	}

	// 3. Seed meta-behaviors (global only)
	if scope == constants.ScopeGlobal {
		homeDir, _ := os.UserHomeDir()
		globalStore, err := store.NewSQLiteGraphStore(homeDir)
		if err != nil {
			return nil, fmt.Errorf("opening global store for seeding: %w", err)
		}
		defer globalStore.Close()

		seedResult, err := seed.NewSeeder(globalStore).SeedGlobalStore(context.Background())
		if err != nil {
			return nil, fmt.Errorf("seeding global store: %w", err)
		}

		if !jsonOut {
			if len(seedResult.Added) > 0 {
				fmt.Printf("Seeded %d meta-behavior(s)\n", len(seedResult.Added))
			}
			if len(seedResult.Updated) > 0 {
				fmt.Printf("Updated %d meta-behavior(s)\n", len(seedResult.Updated))
			}
		}
		result["seeds"] = map[string]interface{}{
			"added":   len(seedResult.Added),
			"updated": len(seedResult.Updated),
			"skipped": len(seedResult.Skipped),
		}
	}

	return result, nil
}

// runInteractiveInit prompts the user for init configuration.
func runInteractiveInit() (doGlobal, doProject bool, hooksMode string, tokenBudget int, err error) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("\nWelcome to floop! Let's set up behavior learning for your AI agents.")

	// Scope
	fmt.Println("? Installation scope")
	fmt.Println("  1) Global (all projects) — recommended")
	fmt.Println("  2) Project (this project only)")
	fmt.Println("  3) Both (global + this project)")
	fmt.Print("  Choose [1]: ")
	scopeChoice := readLine(reader)
	switch scopeChoice {
	case "", "1":
		doGlobal = true
	case "2":
		doProject = true
	case "3":
		doGlobal = true
		doProject = true
	default:
		return false, false, "", 0, fmt.Errorf("invalid scope choice: %s", scopeChoice)
	}

	// Hooks
	fmt.Println("\n? Which hooks to enable?")
	fmt.Println("  1) All hooks — recommended")
	fmt.Println("  2) Behavior injection only (skip correction detection & dynamic context)")
	fmt.Print("  Choose [1]: ")
	hookChoice := readLine(reader)
	switch hookChoice {
	case "", "1":
		hooksMode = "all"
	case "2":
		hooksMode = "injection-only"
	default:
		return false, false, "", 0, fmt.Errorf("invalid hooks choice: %s", hookChoice)
	}

	// Token budget
	fmt.Println("\n? Token budget for behavior injection")
	fmt.Println("  1) 2000 (default — fits ~40 behaviors)")
	fmt.Println("  2) 1000 (conservative — fits ~20 behaviors)")
	fmt.Println("  3) Custom")
	fmt.Print("  Choose [1]: ")
	budgetChoice := readLine(reader)
	switch budgetChoice {
	case "", "1":
		tokenBudget = 2000
	case "2":
		tokenBudget = 1000
	case "3":
		fmt.Print("  Enter token budget: ")
		customBudget := readLine(reader)
		tokenBudget, err = strconv.Atoi(customBudget)
		if err != nil {
			return false, false, "", 0, fmt.Errorf("invalid token budget: %s", customBudget)
		}
	default:
		return false, false, "", 0, fmt.Errorf("invalid budget choice: %s", budgetChoice)
	}

	fmt.Println()
	return doGlobal, doProject, hooksMode, tokenBudget, nil
}

// readLine reads a line from the reader, trimming whitespace.
func readLine(reader *bufio.Reader) string {
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}
