package models

import "testing"

func TestNewBehaviorKinds(t *testing.T) {
	tests := []struct {
		kind     BehaviorKind
		wantType MemoryType
	}{
		{BehaviorKindDirective, MemoryTypeSemantic},
		{BehaviorKindConstraint, MemoryTypeSemantic},
		{BehaviorKindPreference, MemoryTypeSemantic},
		{BehaviorKindProcedure, MemoryTypeProcedural},
		{BehaviorKindEpisodic, MemoryTypeEpisodic},
		{BehaviorKindWorkflow, MemoryTypeProcedural},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			got := MemoryTypeForKind(tt.kind)
			if got != tt.wantType {
				t.Errorf("MemoryTypeForKind(%s) = %s, want %s", tt.kind, got, tt.wantType)
			}
		})
	}
}
