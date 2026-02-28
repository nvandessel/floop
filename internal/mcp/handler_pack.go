package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/floop/internal/pack"
	"github.com/nvandessel/floop/internal/pathutil"
	"github.com/nvandessel/floop/internal/ratelimit"
)

// handleFloopPackInstall implements the floop_pack_install tool.
func (s *Server) handleFloopPackInstall(ctx context.Context, req *sdk.CallToolRequest, args FloopPackInstallInput) (_ *sdk.CallToolResult, _ FloopPackInstallOutput, retErr error) {
	// Resolve source: prefer Source, fall back to deprecated FilePath
	source := args.Source
	if source == "" {
		source = args.FilePath
	}

	start := time.Now()
	defer func() {
		s.auditTool("floop_pack_install", start, retErr, sanitizeToolParams("floop_pack_install", map[string]interface{}{
			"source": source,
		}), "local")
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_pack_install"); err != nil {
		return nil, FloopPackInstallOutput{}, err
	}

	if source == "" {
		return nil, FloopPackInstallOutput{}, fmt.Errorf("source is required (or use deprecated file_path)")
	}

	cfg := s.floopConfig
	if cfg == nil {
		return nil, FloopPackInstallOutput{}, fmt.Errorf("config not available")
	}

	// Resolve source kind to determine install path
	resolved, err := pack.ResolveSource(source)
	if err != nil {
		return nil, FloopPackInstallOutput{}, fmt.Errorf("invalid pack source: %w", err)
	}

	var result *pack.InstallResult

	switch resolved.Kind {
	case pack.SourceLocal:
		// Validate path: restrict to ~/.floop/packs/ only
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, FloopPackInstallOutput{}, fmt.Errorf("getting home directory: %w", err)
		}
		packsDir := filepath.Join(homeDir, ".floop", "packs")
		if err := pathutil.ValidatePath(resolved.FilePath, []string{packsDir}); err != nil {
			return nil, FloopPackInstallOutput{}, fmt.Errorf("pack install path rejected (must be under ~/.floop/packs/): %w", err)
		}

		result, err = pack.Install(ctx, s.store, resolved.FilePath, cfg, pack.InstallOptions{
			DeriveEdges: true, // Always derive edges for MCP callers (agent workflows)
		})
		if err != nil {
			return nil, FloopPackInstallOutput{}, fmt.Errorf("pack install failed: %w", err)
		}

	case pack.SourceHTTP, pack.SourceGitHub:
		// Remote sources bypass path validation, go through InstallFromSource
		results, err := pack.InstallFromSource(ctx, s.store, source, cfg, pack.InstallFromSourceOptions{
			DeriveEdges: true,
		})
		if err != nil {
			return nil, FloopPackInstallOutput{}, fmt.Errorf("pack install failed: %w", err)
		}
		if len(results) == 0 {
			return nil, FloopPackInstallOutput{}, fmt.Errorf("pack install returned no results")
		}
		result = results[0]
	}

	// Save config with updated pack list
	if saveErr := cfg.Save(); saveErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save config: %v\n", saveErr)
	}

	return nil, FloopPackInstallOutput{
		PackID:       result.PackID,
		Version:      result.Version,
		Added:        result.Added,
		Updated:      result.Updated,
		Skipped:      result.Skipped,
		EdgesAdded:   result.EdgesAdded,
		EdgesSkipped: result.EdgesSkipped,
		DerivedEdges: result.DerivedEdges,
		Message:      fmt.Sprintf("Installed %s v%s: %d added, %d updated, %d skipped", result.PackID, result.Version, len(result.Added), len(result.Updated), len(result.Skipped)),
	}, nil
}
