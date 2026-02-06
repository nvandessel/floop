package main

import (
	"testing"
)

func TestValidEdgeKinds(t *testing.T) {
	tests := []struct {
		kind string
		want bool
	}{
		{"requires", true},
		{"overrides", true},
		{"conflicts", true},
		{"similar-to", true},
		{"learned-from", true},
		{"invalid", false},
		{"", false},
		{"REQUIRES", false}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			if got := validEdgeKinds[tt.kind]; got != tt.want {
				t.Errorf("validEdgeKinds[%q] = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}

func TestNewConnectCmd(t *testing.T) {
	cmd := newConnectCmd()

	if cmd.Use != "connect <source> <target> <kind>" {
		t.Errorf("Use = %q, want connect <source> <target> <kind>", cmd.Use)
	}

	// Verify flags exist
	if cmd.Flags().Lookup("weight") == nil {
		t.Error("missing --weight flag")
	}
	if cmd.Flags().Lookup("bidirectional") == nil {
		t.Error("missing --bidirectional flag")
	}

	// Verify default weight
	weight, _ := cmd.Flags().GetFloat64("weight")
	if weight != 0.8 {
		t.Errorf("default weight = %v, want 0.8", weight)
	}
}
