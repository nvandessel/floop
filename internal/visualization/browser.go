package visualization

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenBrowser opens the specified URL in the user's default browser.
// It supports Linux (xdg-open), macOS (open), and Windows (cmd start).
func OpenBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}
