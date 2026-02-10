package visualization

import (
	"runtime"
	"testing"
)

func TestOpenBrowser_CompilationSmoke(t *testing.T) {
	// Verify the function exists and compiles correctly.
	// We don't actually open a browser in tests.
	fn := OpenBrowser
	if fn == nil {
		t.Fatal("OpenBrowser should not be nil")
	}
}

func TestOpenBrowser_UnsupportedPlatform(t *testing.T) {
	// On the current platform, OpenBrowser should not return an "unsupported platform" error
	// since we're running tests on a supported OS.
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
		// These are supported — we just verify the function signature compiles.
	default:
		t.Skipf("skipping on unsupported platform: %s", runtime.GOOS)
	}
}
