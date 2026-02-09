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
		Short: "Upgrade floop hook scripts to match the current binary version",
		Long: `Upgrade installed floop hook scripts to the current binary version.

Detects hook installations in global (~/.claude/) and project (.claude/) scopes,
compares script versions against the binary version, and re-extracts scripts
that are out of date.

Examples:
  floop upgrade           # Upgrade stale scripts
  floop upgrade --force   # Re-extract all scripts regardless of version`,
		RunE: func(cmd *cobra.Command, args []string) error {
			force, _ := cmd.Flags().GetBool("force")
			jsonOut, _ := cmd.Flags().GetBool("json")
			tokenBudget, _ := cmd.Flags().GetInt("token-budget")

			results := make(map[string]interface{})

			// Check global installation
			homeDir, err := os.UserHomeDir()
			if err == nil {
				globalHookDir := filepath.Join(homeDir, ".claude", "hooks")
				globalResult, err := upgradeScope("global", globalHookDir, homeDir, hooks.ScopeGlobal, force, tokenBudget, jsonOut)
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
			projectHookDir := filepath.Join(root, ".claude", "hooks")
			projectResult, err := upgradeScope("project", projectHookDir, root, hooks.ScopeProject, force, tokenBudget, jsonOut)
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

	cmd.Flags().Bool("force", false, "Re-extract all scripts regardless of version")
	cmd.Flags().Int("token-budget", defaultTokenBudget, "Token budget for behavior injection")

	return cmd
}

// upgradeScope checks and upgrades scripts in a single scope.
// Returns nil result if no installation found.
func upgradeScope(scopeName, hookDir, configRoot string, scope hooks.HookScope, force bool, tokenBudget int, jsonOut bool) (map[string]interface{}, error) {
	installed, err := hooks.InstalledScripts(hookDir)
	if err != nil {
		return nil, fmt.Errorf("checking installed scripts: %w", err)
	}
	if len(installed) == 0 {
		return nil, nil
	}

	// Check versions of installed scripts
	stale := false
	for _, script := range installed {
		scriptVer, err := hooks.ScriptVersion(script)
		if err != nil {
			return nil, fmt.Errorf("reading version from %s: %w", script, err)
		}
		if scriptVer != version {
			stale = true
			break
		}
	}

	result := map[string]interface{}{
		"scope":           scopeName,
		"installed_count": len(installed),
		"current_version": version,
	}

	if !stale && !force {
		result["status"] = "up_to_date"
		if !jsonOut {
			fmt.Printf("%s: hooks are up to date (v%s)\n", scopeName, version)
		}
		return result, nil
	}

	// Re-extract scripts
	extracted, err := hooks.ExtractScripts(hookDir, version, tokenBudget)
	if err != nil {
		return nil, fmt.Errorf("extracting scripts: %w", err)
	}

	// Re-configure settings.json
	p := hooks.NewClaudePlatform()
	configResult := hooks.ConfigurePlatform(p, configRoot, scope, hookDir)
	if configResult.Error != nil {
		return nil, fmt.Errorf("updating settings.json: %w", configResult.Error)
	}

	result["status"] = "upgraded"
	result["scripts_extracted"] = len(extracted)

	if !jsonOut {
		reason := "stale"
		if force {
			reason = "forced"
		}
		fmt.Printf("%s: upgraded %d hook script(s) to v%s (%s)\n",
			scopeName, len(extracted), version, reason)
	}

	return result, nil
}
