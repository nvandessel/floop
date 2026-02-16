package hooks

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScriptVersion parses the "# version: X.Y.Z" header from an installed script.
// Returns empty string if no version header is found.
// Used by the upgrade command to detect old-style shell scripts.
func ScriptVersion(scriptPath string) (string, error) {
	f, err := os.Open(scriptPath)
	if err != nil {
		return "", fmt.Errorf("opening script %s: %w", scriptPath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Check first 5 lines for version header
	for i := 0; i < 5 && scanner.Scan(); i++ {
		line := scanner.Text()
		if strings.HasPrefix(line, "# version: ") {
			return strings.TrimPrefix(line, "# version: "), nil
		}
	}

	return "", nil
}

// InstalledScripts returns paths to all floop-*.sh scripts in hookDir.
// Used by the upgrade command to detect old-style shell scripts that
// need migration to native Go subcommands.
func InstalledScripts(hookDir string) ([]string, error) {
	pattern := filepath.Join(hookDir, "floop-*.sh")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing hook scripts: %w", err)
	}
	return matches, nil
}
