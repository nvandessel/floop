package main

import (
	"testing"
)

func TestVaultCmd_HasSubcommands(t *testing.T) {
	cmd := newVaultCmd()

	subs := cmd.Commands()
	names := make(map[string]bool)
	for _, sub := range subs {
		names[sub.Name()] = true
	}

	expected := []string{"init", "push", "pull", "sync", "status"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestVaultInitCmd_RequiresURI(t *testing.T) {
	cmd := newVaultInitCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --uri is missing")
	}
}

func TestVaultPushCmd_FlagParsing(t *testing.T) {
	cmd := newVaultPushCmd()
	// Just verify flags exist — we can't run RunE without config
	f := cmd.Flags()
	if f.Lookup("force") == nil {
		t.Error("missing --force flag")
	}
	if f.Lookup("dry-run") == nil {
		t.Error("missing --dry-run flag")
	}
	if f.Lookup("scope") == nil {
		t.Error("missing --scope flag")
	}
}

func TestVaultPullCmd_FlagParsing(t *testing.T) {
	cmd := newVaultPullCmd()
	f := cmd.Flags()
	if f.Lookup("force") == nil {
		t.Error("missing --force flag")
	}
	if f.Lookup("from") == nil {
		t.Error("missing --from flag")
	}
}
