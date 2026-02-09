package hooks

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExtractScripts extracts embedded hook scripts to targetDir, injecting version
// and tokenBudget into template placeholders. Returns the list of extracted file paths.
func ExtractScripts(targetDir, version string, tokenBudget int) ([]string, error) {
	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return nil, fmt.Errorf("creating hook directory: %w", err)
	}

	entries, err := scripts.ReadDir("scripts")
	if err != nil {
		return nil, fmt.Errorf("reading embedded scripts: %w", err)
	}

	budgetStr := fmt.Sprintf("%d", tokenBudget)
	var extracted []string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		content, err := scripts.ReadFile("scripts/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("reading embedded script %s: %w", entry.Name(), err)
		}

		// Inject version and token budget
		output := strings.ReplaceAll(string(content), "{{VERSION}}", version)
		output = strings.ReplaceAll(output, "{{TOKEN_BUDGET}}", budgetStr)

		destPath := filepath.Join(targetDir, entry.Name())
		if err := os.WriteFile(destPath, []byte(output), 0755); err != nil {
			return nil, fmt.Errorf("writing script %s: %w", entry.Name(), err)
		}

		extracted = append(extracted, destPath)
	}

	return extracted, nil
}

// ScriptVersion parses the "# version: X.Y.Z" header from an installed script.
// Returns empty string if no version header is found.
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
func InstalledScripts(hookDir string) ([]string, error) {
	pattern := filepath.Join(hookDir, "floop-*.sh")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing hook scripts: %w", err)
	}
	return matches, nil
}
