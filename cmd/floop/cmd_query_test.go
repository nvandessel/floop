package main

import (
	"testing"
)

func TestNewShowCmd(t *testing.T) {
	cmd := newShowCmd()
	if cmd.Use != "show [behavior-id]" {
		t.Errorf("Use = %q, want %q", cmd.Use, "show [behavior-id]")
	}
}

func TestNewWhyCmd(t *testing.T) {
	cmd := newWhyCmd()
	if cmd.Use != "why [behavior-id]" {
		t.Errorf("Use = %q, want %q", cmd.Use, "why [behavior-id]")
	}

	for _, flag := range []string{"file", "task", "env"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("missing --%s flag", flag)
		}
	}
}

func TestNewPromptCmd(t *testing.T) {
	cmd := newPromptCmd()
	if cmd.Use != "prompt" {
		t.Errorf("Use = %q, want %q", cmd.Use, "prompt")
	}

	for _, flag := range []string{"file", "task", "env", "format", "max-tokens", "token-budget", "tiered"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("missing --%s flag", flag)
		}
	}

	// Check format default
	format, _ := cmd.Flags().GetString("format")
	if format != "markdown" {
		t.Errorf("default format = %q, want %q", format, "markdown")
	}
}
