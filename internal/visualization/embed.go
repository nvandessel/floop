package visualization

import "embed"

// assets contains the embedded visualization assets (JS libraries).
//
//go:embed assets/*
var assets embed.FS

// templates contains the embedded HTML templates.
//
//go:embed templates/*
var templates embed.FS
