package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate the behavior graph for consistency issues",
		Long: `Validate the behavior graph for consistency issues.

This command checks for:
  - Dangling references (behaviors referencing non-existent IDs)
  - Self-references (behaviors that require/override/conflict with themselves)
  - Cycles in relationship graphs
  - Edge property issues (zero weight, missing timestamps)

Examples:
  floop validate                  # Validate local store
  floop validate --scope global   # Validate global store only
  floop validate --scope both     # Validate both stores`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			scope, _ := cmd.Flags().GetString("scope")

			// Validate scope
			storeScope := store.StoreScope(scope)
			if storeScope != store.ScopeLocal && storeScope != store.ScopeGlobal && storeScope != store.ScopeBoth {
				return fmt.Errorf("invalid scope: %s (must be local, global, or both)", scope)
			}

			// Check local initialization if needed
			if storeScope == store.ScopeLocal || storeScope == store.ScopeBoth {
				floopDir := filepath.Join(root, ".floop")
				if _, err := os.Stat(floopDir); os.IsNotExist(err) {
					return fmt.Errorf(".floop not initialized. Run 'floop init' first")
				}
			}

			// Check global initialization if needed
			if storeScope == store.ScopeGlobal || storeScope == store.ScopeBoth {
				globalPath, err := store.GlobalFloopPath()
				if err != nil {
					return fmt.Errorf("failed to get global path: %w", err)
				}
				if _, err := os.Stat(globalPath); os.IsNotExist(err) {
					return fmt.Errorf("global .floop not initialized. Run 'floop init --global' first")
				}
			}

			ctx := context.Background()

			// Handle validation based on scope
			if storeScope == store.ScopeBoth {
				return runMultiStoreValidation(ctx, root, jsonOut)
			}

			return runSingleStoreValidation(ctx, root, storeScope, jsonOut)
		},
	}

	cmd.Flags().String("scope", "local", "Store scope: local, global, or both")

	return cmd
}

// runSingleStoreValidation validates a single store.
func runSingleStoreValidation(ctx context.Context, root string, scope store.StoreScope, jsonOut bool) error {
	// Open the appropriate store
	var graphStore *store.SQLiteGraphStore
	var err error

	switch scope {
	case store.ScopeLocal:
		graphStore, err = store.NewSQLiteGraphStore(root)
	case store.ScopeGlobal:
		homeDir, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return fmt.Errorf("failed to get home directory: %w", homeErr)
		}
		graphStore, err = store.NewSQLiteGraphStore(homeDir)
	}

	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer graphStore.Close()

	// Run validation
	validationErrors, err := graphStore.ValidateBehaviorGraph(ctx)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	return outputValidationResults(validationErrors, scope, jsonOut)
}

// runMultiStoreValidation validates both local and global stores.
func runMultiStoreValidation(ctx context.Context, root string, jsonOut bool) error {
	multiStore, err := store.NewMultiGraphStore(root)
	if err != nil {
		return fmt.Errorf("failed to open stores: %w", err)
	}
	defer multiStore.Close()

	// Run validation
	validationErrors, err := multiStore.ValidateBehaviorGraph(ctx)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	return outputValidationResults(validationErrors, store.ScopeBoth, jsonOut)
}

// outputValidationResults formats and outputs validation results.
func outputValidationResults(validationErrors []store.ValidationError, scope store.StoreScope, jsonOut bool) error {
	valid := len(validationErrors) == 0

	if jsonOut {
		output := map[string]interface{}{
			"valid":       valid,
			"error_count": len(validationErrors),
			"scope":       string(scope),
		}

		if len(validationErrors) > 0 {
			errors := make([]map[string]interface{}, len(validationErrors))
			for i, ve := range validationErrors {
				errors[i] = map[string]interface{}{
					"behavior_id": ve.BehaviorID,
					"field":       ve.Field,
					"ref_id":      ve.RefID,
					"issue":       ve.Issue,
				}
			}
			output["errors"] = errors
		}

		if valid {
			output["message"] = "Behavior graph is valid"
		} else {
			output["message"] = fmt.Sprintf("Found %d validation error(s)", len(validationErrors))
		}

		return json.NewEncoder(os.Stdout).Encode(output)
	}

	// Human-readable output
	fmt.Printf("Validating %s store(s)...\n\n", scope)

	if valid {
		fmt.Println("✓ Behavior graph is valid - no issues found.")
		return nil
	}

	fmt.Printf("✗ Found %d validation error(s):\n\n", len(validationErrors))

	for i, ve := range validationErrors {
		fmt.Printf("%d. [%s] %s\n", i+1, ve.Issue, ve.BehaviorID)
		fmt.Printf("   Field: %s\n", ve.Field)
		fmt.Printf("   References: %s\n\n", ve.RefID)
	}

	return nil
}
