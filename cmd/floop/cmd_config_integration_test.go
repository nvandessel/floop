package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/nvandessel/floop/internal/config"
)

func setupConfigTest(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Create ~/.floop/config.yaml with default config
	homeDir := filepath.Join(tmpDir, "home")
	floopDir := filepath.Join(homeDir, ".floop")
	if err := os.MkdirAll(floopDir, 0700); err != nil {
		t.Fatalf("failed to create .floop: %v", err)
	}

	cfg := config.Default()
	cfg.LLM.Provider = "anthropic"
	cfg.LLM.Enabled = true

	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(floopDir, "config.yaml"), data, 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	return tmpDir
}

// Config subcommands write to os.Stdout directly (not cmd.OutOrStdout()),
// so we verify they execute without error rather than checking output buffer.

func TestConfigListCmd(t *testing.T) {
	setupConfigTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "list"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("config list failed: %v", err)
	}
}

func TestConfigListCmdJSON(t *testing.T) {
	setupConfigTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "list", "--json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("config list --json failed: %v", err)
	}
}

func TestConfigGetCmd(t *testing.T) {
	setupConfigTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "get", "llm.provider"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("config get failed: %v", err)
	}
}

func TestConfigGetCmdJSON(t *testing.T) {
	setupConfigTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "get", "llm.provider", "--json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("config get --json failed: %v", err)
	}
}

func TestConfigGetCmdUnknownKey(t *testing.T) {
	setupConfigTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "get", "nonexistent.key"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("config get unknown key failed: %v", err)
	}
}

func TestConfigSetCmd(t *testing.T) {
	setupConfigTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "set", "llm.provider", "openai"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("config set failed: %v", err)
	}
}

func TestConfigSetCmdJSON(t *testing.T) {
	setupConfigTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "set", "llm.provider", "openai", "--json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("config set --json failed: %v", err)
	}
}

func TestConfigSetCmdInvalidValue(t *testing.T) {
	setupConfigTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "set", "llm.provider", "invalid_provider"})

	// Should not return error (prints error message instead)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("config set invalid: %v", err)
	}
}

func TestConfigSetCmdUnknownKey(t *testing.T) {
	setupConfigTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "set", "nonexistent.key", "value"})

	// Should not return error (prints error message instead)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("config set unknown key: %v", err)
	}
}
