package constants

// Scope represents the scope of a behavior (local to project or global)
type Scope string

const (
	// ScopeLocal indicates the behavior applies only to the current project
	ScopeLocal Scope = "local"

	// ScopeGlobal indicates the behavior applies across all projects
	ScopeGlobal Scope = "global"

	// ScopeBoth indicates the operation should consider both scopes
	ScopeBoth Scope = "both"
)

// Valid returns true if the scope is a recognized value.
func (s Scope) Valid() bool {
	switch s {
	case ScopeLocal, ScopeGlobal, ScopeBoth:
		return true
	}
	return false
}

// String returns the string representation of the scope.
func (s Scope) String() string {
	return string(s)
}
