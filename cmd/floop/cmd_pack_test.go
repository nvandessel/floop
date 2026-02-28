package main

import (
	"testing"
)

func TestNewPackCmd(t *testing.T) {
	cmd := newPackCmd()

	if cmd.Use != "pack" {
		t.Errorf("Use = %q, want %q", cmd.Use, "pack")
	}

	// Verify subcommands exist
	subcommands := map[string]bool{
		"create":  false,
		"install": false,
		"list":    false,
		"info":    false,
		"update":  false,
		"remove":  false,
	}

	for _, sub := range cmd.Commands() {
		if _, ok := subcommands[sub.Name()]; ok {
			subcommands[sub.Name()] = true
		}
	}

	for name, found := range subcommands {
		if !found {
			t.Errorf("missing subcommand %q", name)
		}
	}
}

func TestNewPackCreateCmd_Flags(t *testing.T) {
	cmd := newPackCreateCmd()

	if cmd.Use != "create <output-path>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "create <output-path>")
	}

	requiredFlags := []string{"id", "version"}
	for _, flag := range requiredFlags {
		f := cmd.Flags().Lookup(flag)
		if f == nil {
			t.Errorf("missing --%s flag", flag)
			continue
		}
	}

	optionalFlags := []string{"description", "author", "tags", "source", "filter-tags", "filter-scope", "filter-kinds"}
	for _, flag := range optionalFlags {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("missing --%s flag", flag)
		}
	}
}

func TestNewPackInstallCmd_Args(t *testing.T) {
	cmd := newPackInstallCmd()

	if cmd.Use != "install <source>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "install <source>")
	}

	if f := cmd.Flags().Lookup("derive-edges"); f == nil {
		t.Error("missing --derive-edges flag")
	}

	if f := cmd.Flags().Lookup("all-assets"); f == nil {
		t.Error("missing --all-assets flag")
	}
}

func TestNewPackListCmd(t *testing.T) {
	cmd := newPackListCmd()

	if cmd.Use != "list" {
		t.Errorf("Use = %q, want %q", cmd.Use, "list")
	}
}

func TestNewPackInfoCmd_Args(t *testing.T) {
	cmd := newPackInfoCmd()

	if cmd.Use != "info <pack-id>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "info <pack-id>")
	}
}

func TestNewPackUpdateCmd_Args(t *testing.T) {
	cmd := newPackUpdateCmd()

	if cmd.Use != "update [pack-id|source]" {
		t.Errorf("Use = %q, want %q", cmd.Use, "update [pack-id|source]")
	}

	if f := cmd.Flags().Lookup("derive-edges"); f == nil {
		t.Error("missing --derive-edges flag")
	}

	if f := cmd.Flags().Lookup("all"); f == nil {
		t.Error("missing --all flag")
	}
}

func TestNewPackRemoveCmd_Args(t *testing.T) {
	cmd := newPackRemoveCmd()

	if cmd.Use != "remove <pack-id>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "remove <pack-id>")
	}
}
