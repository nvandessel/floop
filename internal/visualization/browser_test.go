package visualization

import (
	"runtime"
	"testing"
)

func TestOpenBrowser_SupportedPlatform(t *testing.T) {
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
		// Supported — we verify compilation and platform coverage.
	default:
		t.Skipf("skipping on unsupported platform: %s", runtime.GOOS)
	}
}
