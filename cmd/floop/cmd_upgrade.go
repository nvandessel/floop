package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvandessel/feedback-loop/internal/hooks"
	"github.com/spf13/cobra"
)

func newUpgradeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade floop hooks to native Go subcommands",
		Long: `Upgrade floop hook configuration to use native Go subcommands.

Detects old shell script installations and migrates them to native
"floop hook <name>" commands. Also detects if native hooks are already
configured and reports them as up to date.

Examples:
  floop upgrade           # Migrate .sh scripts to native commands
  floop upgrade --force   # Re-configure even if already native`,
		RunE: func(cmd *cobra.Command, args []string) error {
			force, _ := cmd.Flags().GetBool("force")
			jsonOut, _ := cmd.Flags().GetBool("json")

			results := make(map[string]interface{})

			// Check global installation
			homeDir, err := os.UserHomeDir()
			if err == nil {
				globalResult, err := upgradeScope("global", homeDir, hooks.ScopeGlobal, force, jsonOut)
				if err != nil {
					if !jsonOut {
						fmt.Fprintf(os.Stderr, "Warning: global upgrade failed: %v\n", err)
					}
				} else if globalResult != nil {
					results["global"] = globalResult
				}
			}

			// Check project installation
			root, _ := cmd.Flags().GetString("root")
			projectResult, err := upgradeScope("project", root, hooks.ScopeProject, force, jsonOut)
			if err != nil {
				if !jsonOut {
					fmt.Fprintf(os.Stderr, "Warning: project upgrade failed: %v\n", err)
				}
			} else if projectResult != nil {
				results["project"] = projectResult
			}

			if len(results) == 0 {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"status":  "no_installations",
						"version": version,
					})
				} else {
					fmt.Println("No floop hook installations found.")
					fmt.Println("Run 'floop init' to set up hooks.")
				}
				return nil
			}

			if jsonOut {
				results["version"] = version
				json.NewEncoder(os.Stdout).Encode(results)
			}

			return nil
		},
	}

	cmd.Flags().Bool("force", false, "Re-configure hooks even if already native")

	return cmd
}

// upgradeScope checks and upgrades hooks in a single scope.
// Returns nil result if no installation found.
func upgradeScope(scopeName, configRoot string, scope hooks.HookScope, force bool, jsonOut bool) (map[string]interface{}, error) {
	p := hooks.NewClaudePlatform()

	// Check if native hooks are already configured
	hasHook, err := p.HasFloopHook(configRoot)
	if err != nil {
		return nil, fmt.Errorf("checking hook config: %w", err)
	}

	// Check for old .sh scripts that need migration
	hookDir := filepath.Join(configRoot, ".claude", "hooks")
	oldScripts, _ := hooks.InstalledScripts(hookDir)

	if !hasHook && len(oldScripts) == 0 {
		return nil, nil // no installation found
	}

	result := map[string]interface{}{
		"scope":           scopeName,
		"current_version": version,
	}

	// If old scripts exist, migrate: remove scripts + reconfigure
	if len(oldScripts) > 0 {
		// Remove old .sh scripts
		for _, script := range oldScripts {
			if err := os.Remove(script); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("removing old script %s: %w", script, err)
			}
		}

		// Regenerate config with native commands
		configResult := hooks.ConfigurePlatform(p, configRoot, scope, "")
		if configResult.Error != nil {
			return nil, fmt.Errorf("updating settings.json: %w", configResult.Error)
		}

		result["status"] = "migrated"
		result["scripts_removed"] = len(oldScripts)

		if !jsonOut {
			fmt.Printf("%s: migrated %d shell script(s) to native Go commands\n",
				scopeName, len(oldScripts))
		}

		return result, nil
	}

	// Native hooks already configured
	if !force {
		result["status"] = "up_to_date"
		if !jsonOut {
			fmt.Printf("%s: hooks are up to date (native commands)\n", scopeName)
		}
		return result, nil
	}

	// Force re-configure
	configResult := hooks.ConfigurePlatform(p, configRoot, scope, "")
	if configResult.Error != nil {
		return nil, fmt.Errorf("updating settings.json: %w", configResult.Error)
	}

	result["status"] = "reconfigured"
	if !jsonOut {
		fmt.Printf("%s: reconfigured hooks (forced)\n", scopeName)
	}

	return result, nil
}
