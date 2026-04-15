package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nvandessel/floop/internal/config"
	"github.com/nvandessel/floop/internal/store"
	"github.com/nvandessel/floop/internal/vault"
	"github.com/spf13/cobra"
)

// defaultVaultDims is the default vector dimensions for vault sync.
// This should match the embedding model dimensions used by the system.
const defaultVaultDims = 768

func newVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Lance-native S3 backup and sync",
		Long: `Manage vault sync for floop's behavioral memory store.

Vault sync provides bidirectional synchronization between local floop stores
and a remote S3-compatible backend (MinIO, AWS S3, R2).

Commands:
  init     Configure vault remote and test connectivity
  push     Push local state to remote
  pull     Pull remote state to local
  sync     Bidirectional sync (pull then push)
  status   Show sync state and divergence
  verify   Verify integrity of local and remote data`,
	}

	cmd.AddCommand(
		newVaultInitCmd(),
		newVaultPushCmd(),
		newVaultPullCmd(),
		newVaultSyncCmd(),
		newVaultStatusCmd(),
		newVaultVerifyCmd(),
	)

	return cmd
}

func newVaultInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Configure vault remote and test connectivity",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Flags().GetBool("json")

			uri, _ := cmd.Flags().GetString("uri")
			endpoint, _ := cmd.Flags().GetString("endpoint")
			region, _ := cmd.Flags().GetString("region")
			accessKey, _ := cmd.Flags().GetString("access-key")
			secretKey, _ := cmd.Flags().GetString("secret-key")
			pathStyle, _ := cmd.Flags().GetBool("path-style")
			machineID, _ := cmd.Flags().GetString("machine-id")

			if uri == "" {
				return fmt.Errorf("--uri is required")
			}

			// Load and update config
			cfg, err := config.Load()
			if err != nil {
				cfg = config.Default()
			}

			freshInit := !cfg.Vault.Configured()

			cfg.Vault.Remote.URI = uri
			if endpoint != "" {
				cfg.Vault.Remote.Endpoint = endpoint
			}
			if region != "" {
				cfg.Vault.Remote.Region = region
			}
			if accessKey != "" {
				cfg.Vault.Remote.AccessKeyID = accessKey
			} else if cfg.Vault.Remote.AccessKeyID == "" {
				cfg.Vault.Remote.AccessKeyID = os.Getenv("FLOOP_VAULT_ACCESS_KEY")
			}
			if secretKey != "" {
				cfg.Vault.Remote.SecretAccessKey = secretKey
			} else if cfg.Vault.Remote.SecretAccessKey == "" {
				cfg.Vault.Remote.SecretAccessKey = os.Getenv("FLOOP_VAULT_SECRET_KEY")
			}
			cfg.Vault.Remote.PathStyle = pathStyle
			if machineID != "" {
				cfg.Vault.MachineID = machineID
			}

			// Set defaults
			if cfg.Vault.Remote.Region == "" {
				cfg.Vault.Remote.Region = "us-east-1"
			}
			if cfg.Vault.Sync.Timeout == "" {
				cfg.Vault.Sync.Timeout = "30s"
			}
			if freshInit {
				cfg.Vault.Sync.IncludeProjects = true
			}

			// Validate
			if err := cfg.Vault.Validate(); err != nil {
				return fmt.Errorf("invalid vault config: %w", err)
			}

			// Save config
			if err := cfg.Save(); err != nil {
				return fmt.Errorf("cannot save config: %w", err)
			}

			// Test connectivity
			homeDir, homeErr := os.UserHomeDir()
			if homeErr != nil {
				return fmt.Errorf("cannot determine home directory: %w", homeErr)
			}
			vectorDir := filepath.Join(homeDir, ".floop", "vectors")
			svc, err := vault.NewVaultService(&cfg.Vault, vectorDir, version, defaultVaultDims)
			if err != nil {
				return fmt.Errorf("creating vault service: %w", err)
			}

			ctx := context.Background()
			if err := svc.Init(ctx); err != nil {
				return err
			}

			resolvedMachineID := cfg.Vault.ResolveMachineID()

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"status":     "initialized",
					"uri":        cfg.Vault.Remote.URI,
					"endpoint":   cfg.Vault.Remote.Endpoint,
					"machine_id": resolvedMachineID,
					"message":    "Vault initialized. Run 'floop vault push' to sync.",
				})
			}

			fmt.Printf("Vault initialized successfully.\n")
			fmt.Printf("  URI: %s\n", cfg.Vault.Remote.URI)
			fmt.Printf("  Endpoint: %s\n", cfg.Vault.Remote.Endpoint)
			fmt.Printf("  Machine ID: %s\n", resolvedMachineID)
			fmt.Printf("\nRun 'floop vault push' to sync.\n")
			return nil
		},
	}

	cmd.Flags().String("uri", "", "S3 URI (s3://bucket/prefix)")
	cmd.Flags().String("endpoint", "", "S3 endpoint URL")
	cmd.Flags().String("region", "us-east-1", "AWS region")
	cmd.Flags().String("access-key", "", "Access key ID (or set FLOOP_VAULT_ACCESS_KEY)")
	cmd.Flags().String("secret-key", "", "Secret access key (or set FLOOP_VAULT_SECRET_KEY)")
	cmd.Flags().Bool("path-style", true, "Use path-style requests (default true for MinIO)")
	cmd.Flags().String("machine-id", "", "Machine identifier (default: hostname)")

	return cmd
}

func newVaultPushCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push local state to remote",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			force, _ := cmd.Flags().GetBool("force")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			scope, _ := cmd.Flags().GetString("scope")

			svc, graphStore, cleanup, err := setupVaultCmd(root)
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := context.Background()
			result, err := svc.Push(ctx, graphStore, root, vault.PushOptions{
				Force:  force,
				DryRun: dryRun,
				Scope:  scope,
			})
			if err != nil {
				return fmt.Errorf("push failed: %w", err)
			}

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"vector_rows_pushed": result.Vectors.RowsPushed,
					"node_count":         result.Graph.NodeCount,
					"edge_count":         result.Graph.EdgeCount,
					"dry_run":            dryRun,
					"duration":           result.Duration.String(),
					"message":            formatPushMessage(result, dryRun),
				})
			}

			if dryRun {
				fmt.Println("Would push:")
				fmt.Printf("  Vectors: %d rows\n", result.Vectors.RowsPushed)
				fmt.Printf("  Graph: %d nodes, %d edges\n", result.Graph.NodeCount, result.Graph.EdgeCount)
			} else {
				fmt.Printf("Push complete (%s)\n", result.Duration.Round(time.Millisecond))
				fmt.Printf("  Vectors: %d rows pushed\n", result.Vectors.RowsPushed)
				fmt.Printf("  Graph: %d nodes, %d edges\n", result.Graph.NodeCount, result.Graph.EdgeCount)
			}
			return nil
		},
	}

	cmd.Flags().Bool("force", false, "Overwrite remote state without diffing")
	cmd.Flags().Bool("dry-run", false, "Show what would be pushed without pushing")
	cmd.Flags().String("scope", "global", "Scope: global, local, or both")

	return cmd
}

func newVaultPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull remote state to local",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			force, _ := cmd.Flags().GetBool("force")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			from, _ := cmd.Flags().GetString("from")
			scope, _ := cmd.Flags().GetString("scope")

			svc, graphStore, cleanup, err := setupVaultCmd(root)
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := context.Background()
			result, err := svc.Pull(ctx, graphStore, vault.PullOptions{
				Force:       force,
				DryRun:      dryRun,
				FromMachine: from,
				Scope:       scope,
			})
			if err != nil {
				return fmt.Errorf("pull failed: %w", err)
			}

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"vector_rows_pulled": result.Vectors.RowsPulled,
					"node_count":         result.Graph.NodeCount,
					"edge_count":         result.Graph.EdgeCount,
					"dry_run":            dryRun,
					"duration":           result.Duration.String(),
					"message":            formatPullMessage(result, dryRun),
				})
			}

			if dryRun {
				fmt.Println("Would pull:")
				fmt.Printf("  Vectors: %d rows\n", result.Vectors.RowsPulled)
				fmt.Printf("  Graph: %d nodes, %d edges\n", result.Graph.NodeCount, result.Graph.EdgeCount)
			} else {
				fmt.Printf("Pull complete (%s)\n", result.Duration.Round(time.Millisecond))
				fmt.Printf("  Vectors: %d rows pulled\n", result.Vectors.RowsPulled)
				fmt.Printf("  Graph: %d nodes, %d edges\n", result.Graph.NodeCount, result.Graph.EdgeCount)
			}
			return nil
		},
	}

	cmd.Flags().Bool("force", false, "Overwrite local state without diffing")
	cmd.Flags().Bool("dry-run", false, "Show what would be pulled without pulling")
	cmd.Flags().String("from", "", "Machine ID to pull from (default: own machine)")
	cmd.Flags().String("scope", "global", "Scope: global, local, or both")

	return cmd
}

func newVaultSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Bidirectional sync (pull then push)",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			scope, _ := cmd.Flags().GetString("scope")

			svc, graphStore, cleanup, err := setupVaultCmd(root)
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := context.Background()
			result, err := svc.Sync(ctx, graphStore, root, vault.SyncOptions{
				DryRun: dryRun,
				Scope:  scope,
			})
			if err != nil {
				return fmt.Errorf("sync failed: %w", err)
			}

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"pulled": map[string]interface{}{
						"vector_rows": result.Pulled.Vectors.RowsPulled,
						"nodes":       result.Pulled.Graph.NodeCount,
						"edges":       result.Pulled.Graph.EdgeCount,
					},
					"pushed": map[string]interface{}{
						"vector_rows": result.Pushed.Vectors.RowsPushed,
						"nodes":       result.Pushed.Graph.NodeCount,
						"edges":       result.Pushed.Graph.EdgeCount,
					},
					"message": "Sync complete",
				})
			}

			fmt.Println("Sync complete")
			fmt.Printf("  Pulled: %d vector rows, %d nodes, %d edges\n",
				result.Pulled.Vectors.RowsPulled, result.Pulled.Graph.NodeCount, result.Pulled.Graph.EdgeCount)
			fmt.Printf("  Pushed: %d vector rows, %d nodes, %d edges\n",
				result.Pushed.Vectors.RowsPushed, result.Pushed.Graph.NodeCount, result.Pushed.Graph.EdgeCount)
			return nil
		},
	}

	cmd.Flags().Bool("dry-run", false, "Show sync plan without executing")
	cmd.Flags().String("scope", "global", "Scope: global, local, or both")

	return cmd
}

func newVaultStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show sync state and divergence",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")

			svc, graphStore, cleanup, err := setupVaultCmd(root)
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := context.Background()
			result, err := svc.Status(ctx, graphStore)
			if err != nil {
				return fmt.Errorf("status failed: %w", err)
			}

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(result)
			}

			fmt.Printf("Vault: configured (%s)\n", result.URI)
			fmt.Printf("Machine: %s\n", result.MachineID)
			if !result.LastPush.IsZero() {
				fmt.Printf("Last push: %s (%s ago)\n", result.LastPush.Format(time.RFC3339), time.Since(result.LastPush).Round(time.Minute))
			} else {
				fmt.Println("Last push: never")
			}
			if !result.LastPull.IsZero() {
				fmt.Printf("Last pull: %s (%s ago)\n", result.LastPull.Format(time.RFC3339), time.Since(result.LastPull).Round(time.Minute))
			} else {
				fmt.Println("Last pull: never")
			}
			fmt.Printf("Local: %d vector rows, %d nodes\n", result.LocalVectorRows, result.LocalNodeCount)
			fmt.Printf("Remote: %d vector rows\n", result.RemoteVectorRows)
			fmt.Printf("Status: %s\n", result.Status)
			return nil
		},
	}
}

func newVaultVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify integrity of local and remote data",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			remoteOnly, _ := cmd.Flags().GetBool("remote-only")
			localOnly, _ := cmd.Flags().GetBool("local-only")

			svc, graphStore, cleanup, err := setupVaultCmd(root)
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := context.Background()
			result, err := svc.Verify(ctx, graphStore, vault.VerifyOptions{
				RemoteOnly: remoteOnly,
				LocalOnly:  localOnly,
			})
			if err != nil {
				return fmt.Errorf("verify failed: %w", err)
			}

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(result)
			}

			if result.OK {
				fmt.Println("Verification passed")
			} else {
				fmt.Println("Verification FAILED")
			}
			for _, issue := range result.Issues {
				fmt.Printf("  - %s\n", issue)
			}
			if result.LocalVectorRows >= 0 {
				fmt.Printf("  Local vectors: %d rows\n", result.LocalVectorRows)
			}
			if result.RemoteVectorRows >= 0 {
				fmt.Printf("  Remote vectors: %d rows\n", result.RemoteVectorRows)
			}
			if !result.OK {
				return fmt.Errorf("verification failed with %d issue(s)", len(result.Issues))
			}
			return nil
		},
	}

	cmd.Flags().Bool("remote-only", false, "Only verify remote data")
	cmd.Flags().Bool("local-only", false, "Only verify local data")

	return cmd
}

// setupVaultCmd loads config, creates store and vault service.
func setupVaultCmd(root string) (*vault.VaultService, store.GraphStore, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("loading config: %w", err)
	}

	if !cfg.Vault.Configured() {
		return nil, nil, nil, fmt.Errorf("vault not configured — run 'floop vault init'")
	}

	graphStore, err := store.NewMultiGraphStore(root)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("opening store: %w", err)
	}

	homeDir, homeErr := os.UserHomeDir()
	if homeErr != nil {
		return nil, nil, nil, fmt.Errorf("cannot determine home directory: %w", homeErr)
	}
	vectorDir := filepath.Join(homeDir, ".floop", "vectors")

	svc, err := vault.NewVaultService(&cfg.Vault, vectorDir, version, defaultVaultDims)
	if err != nil {
		graphStore.Close()
		return nil, nil, nil, err
	}

	cleanup := func() {
		graphStore.Close()
	}

	return svc, graphStore, cleanup, nil
}

func formatPushMessage(r *vault.PushResult, dryRun bool) string {
	if dryRun {
		return fmt.Sprintf("Would push: %d vectors, %d nodes, %d edges",
			r.Vectors.RowsPushed, r.Graph.NodeCount, r.Graph.EdgeCount)
	}
	return fmt.Sprintf("Push complete: %d vectors, %d nodes, %d edges",
		r.Vectors.RowsPushed, r.Graph.NodeCount, r.Graph.EdgeCount)
}

func formatPullMessage(r *vault.PullResult, dryRun bool) string {
	if dryRun {
		return fmt.Sprintf("Would pull: %d vectors, %d nodes, %d edges",
			r.Vectors.RowsPulled, r.Graph.NodeCount, r.Graph.EdgeCount)
	}
	return fmt.Sprintf("Pull complete: %d vectors, %d nodes, %d edges",
		r.Vectors.RowsPulled, r.Graph.NodeCount, r.Graph.EdgeCount)
}
