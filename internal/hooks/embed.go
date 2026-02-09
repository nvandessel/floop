package hooks

import "embed"

// scripts contains the embedded hook scripts for extraction.
//
//go:embed scripts/*.sh
var scripts embed.FS
