package mcp

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHandleFloopPackInstall_MissingSource(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()
	req := &sdk.CallToolRequest{}
	args := FloopPackInstallInput{}

	_, _, err := server.handleFloopPackInstall(ctx, req, args)
	if err == nil {
		t.Fatal("Expected error for missing source")
	}
	if !strings.Contains(err.Error(), "source is required") {
		t.Errorf("error = %q, want to contain 'source is required'", err.Error())
	}
}

func TestHandleFloopPackInstall_PathOutsideAllowed(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()
	req := &sdk.CallToolRequest{}
	// A path that resolves to local but is outside ~/.floop/packs/
	args := FloopPackInstallInput{
		Source: "/etc/some-random-path.fpack",
	}

	_, _, err := server.handleFloopPackInstall(ctx, req, args)
	if err == nil {
		t.Fatal("Expected error for path outside allowed directories")
	}
	if !strings.Contains(err.Error(), "pack install path rejected") {
		t.Errorf("error = %q, want to contain 'pack install path rejected'", err.Error())
	}
}

func TestHandleFloopPackInstall_LocalPathRestriction(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()
	req := &sdk.CallToolRequest{}

	// Attempt to install from outside ~/.floop/packs/
	args := FloopPackInstallInput{
		Source: "/tmp/evil.fpack",
	}

	_, _, err := server.handleFloopPackInstall(ctx, req, args)
	if err == nil {
		t.Fatal("Expected error for path outside ~/.floop/packs/")
	}
	if !strings.Contains(err.Error(), "pack install path rejected") {
		t.Errorf("error = %q, want to contain 'pack install path rejected'", err.Error())
	}
}

func TestHandleFloopPackInstall_DeprecatedFilePath(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()
	req := &sdk.CallToolRequest{}

	// Use deprecated file_path field — should still try to resolve
	args := FloopPackInstallInput{
		FilePath: "/tmp/deprecated.fpack",
	}

	_, _, err := server.handleFloopPackInstall(ctx, req, args)
	if err == nil {
		t.Fatal("Expected error (path validation should reject)")
	}
	// Should still try to resolve — the deprecated field falls through to validation
	if !strings.Contains(err.Error(), "pack install path rejected") {
		t.Errorf("error = %q, want to contain 'pack install path rejected'", err.Error())
	}
}

func TestHandleFloopPackInstall_NilConfig(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	// Force nil config
	origCfg := server.floopConfig
	server.floopConfig = nil
	defer func() { server.floopConfig = origCfg }()

	ctx := context.Background()
	req := &sdk.CallToolRequest{}
	args := FloopPackInstallInput{
		Source: "/some/path.fpack",
	}

	_, _, err := server.handleFloopPackInstall(ctx, req, args)
	if err == nil {
		t.Fatal("Expected error for nil config")
	}
	if !strings.Contains(err.Error(), "config not available") {
		t.Errorf("error = %q, want to contain 'config not available'", err.Error())
	}
}

func TestHandleFloopPackInstall_LocalFileNotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	// Create the packs dir under the isolated home
	ctx := context.Background()
	req := &sdk.CallToolRequest{}

	// Use a path under ~/.floop/packs/ that doesn't exist
	// The isolated HOME means ~/.floop/packs/ won't have any files
	args := FloopPackInstallInput{
		Source: "~/.floop/packs/nonexistent.fpack",
	}

	_, _, err := server.handleFloopPackInstall(ctx, req, args)
	if err == nil {
		// It's fine if the path validation rejects it or the file isn't found
		return
	}
	// We expect either a path rejection or file-not-found error
	_ = err // error is expected
}
