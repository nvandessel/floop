package constants

import "testing"

func TestScope_Valid(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		want  bool
	}{
		{
			name:  "local is valid",
			scope: ScopeLocal,
			want:  true,
		},
		{
			name:  "global is valid",
			scope: ScopeGlobal,
			want:  true,
		},
		{
			name:  "both is valid",
			scope: ScopeBoth,
			want:  true,
		},
		{
			name:  "empty string is invalid",
			scope: Scope(""),
			want:  false,
		},
		{
			name:  "arbitrary string is invalid",
			scope: Scope("invalid"),
			want:  false,
		},
		{
			name:  "LOCAL uppercase is invalid",
			scope: Scope("LOCAL"),
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scope.Valid(); got != tt.want {
				t.Errorf("Scope.Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScope_String(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		want  string
	}{
		{
			name:  "local",
			scope: ScopeLocal,
			want:  "local",
		},
		{
			name:  "global",
			scope: ScopeGlobal,
			want:  "global",
		},
		{
			name:  "both",
			scope: ScopeBoth,
			want:  "both",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scope.String(); got != tt.want {
				t.Errorf("Scope.String() = %v, want %v", got, tt.want)
			}
		})
	}
}
