package pack

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SourceKind classifies the type of pack source.
type SourceKind int

const (
	// SourceLocal is a local file path.
	SourceLocal SourceKind = iota
	// SourceHTTP is an HTTP/HTTPS URL.
	SourceHTTP
	// SourceGitHub is a GitHub shorthand (gh:owner/repo[@version]).
	SourceGitHub
)

// String returns a human-readable name for the source kind.
func (k SourceKind) String() string {
	switch k {
	case SourceLocal:
		return "local"
	case SourceHTTP:
		return "http"
	case SourceGitHub:
		return "github"
	default:
		return "unknown"
	}
}

// ResolvedSource contains the parsed components of a pack source string.
type ResolvedSource struct {
	Kind      SourceKind
	Raw       string // original input
	Canonical string // normalized for storage in config
	FilePath  string // for SourceLocal: absolute path
	URL       string // for SourceHTTP: full URL
	Owner     string // for SourceGitHub
	Repo      string // for SourceGitHub
	Version   string // for SourceGitHub ("" = latest)
}

// ResolveSource parses a source string into its components.
//
// Supported formats:
//   - gh:owner/repo          → SourceGitHub (latest release)
//   - gh:owner/repo@v1.2.3   → SourceGitHub (specific version)
//   - https://example.com/x  → SourceHTTP
//   - http://example.com/x   → SourceHTTP
//   - ./path or /abs/path    → SourceLocal
func ResolveSource(source string) (*ResolvedSource, error) {
	if source == "" {
		return nil, fmt.Errorf("source is required")
	}

	// GitHub shorthand: gh:owner/repo[@version]
	if strings.HasPrefix(source, "gh:") {
		return resolveGitHub(source)
	}

	// HTTP/HTTPS URL
	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") {
		return &ResolvedSource{
			Kind:      SourceHTTP,
			Raw:       source,
			Canonical: source,
			URL:       source,
		}, nil
	}

	// Everything else is a local path
	return resolveLocal(source)
}

// resolveGitHub parses gh:owner/repo[@version].
func resolveGitHub(source string) (*ResolvedSource, error) {
	rest := strings.TrimPrefix(source, "gh:")
	if rest == "" {
		return nil, fmt.Errorf("invalid GitHub source %q: expected gh:owner/repo", source)
	}

	// Split on @ for version
	var repoRef, version string
	if idx := strings.Index(rest, "@"); idx >= 0 {
		repoRef = rest[:idx]
		version = rest[idx+1:]
		if version == "" {
			return nil, fmt.Errorf("invalid GitHub source %q: version after @ is empty", source)
		}
	} else {
		repoRef = rest
	}

	// Split owner/repo
	parts := strings.SplitN(repoRef, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid GitHub source %q: expected gh:owner/repo", source)
	}

	canonical := "gh:" + parts[0] + "/" + parts[1]
	if version != "" {
		canonical += "@" + version
	}

	return &ResolvedSource{
		Kind:      SourceGitHub,
		Raw:       source,
		Canonical: canonical,
		Owner:     parts[0],
		Repo:      parts[1],
		Version:   version,
	}, nil
}

// resolveLocal resolves a local file path to an absolute path.
func resolveLocal(source string) (*ResolvedSource, error) {
	absPath, err := filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("resolving local path %q: %w", source, err)
	}

	return &ResolvedSource{
		Kind:      SourceLocal,
		Raw:       source,
		Canonical: absPath,
		FilePath:  absPath,
	}, nil
}
