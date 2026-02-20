// Package setup provides installation and detection utilities for floop's
// local embedding dependencies (llama.cpp shared libraries and GGUF models).
package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/hybridgroup/yzma/pkg/download"
)

// DefaultEmbeddingModelURL returns the HuggingFace URL for the default embedding model.
func DefaultEmbeddingModelURL() string {
	return "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q4_K_M.gguf"
}

// EmbeddingSetup describes the detected state of embedding dependencies.
type EmbeddingSetup struct {
	LibPath   string // path to llama.cpp libs directory (empty if not found)
	ModelPath string // path to GGUF model file (empty if not found)
	Available bool   // true if both lib + model found
}

// DefaultFloopDir returns the default floop data directory (~/.floop/).
func DefaultFloopDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".floop")
}

// DetectInstalled checks baseDir for llama.cpp libraries and GGUF models.
// Returns an EmbeddingSetup describing what was found.
func DetectInstalled(baseDir string) EmbeddingSetup {
	var result EmbeddingSetup

	// Check for libraries in baseDir/lib/
	libDir := filepath.Join(baseDir, "lib")
	libFile := filepath.Join(libDir, libraryFileName())
	if _, err := os.Stat(libFile); err == nil {
		result.LibPath = libDir
	}

	// Check for GGUF model in baseDir/models/
	modelsDir := filepath.Join(baseDir, "models")
	entries, err := os.ReadDir(modelsDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && filepath.Ext(entry.Name()) == ".gguf" {
				result.ModelPath = filepath.Join(modelsDir, entry.Name())
				break
			}
		}
	}

	result.Available = result.LibPath != "" && result.ModelPath != ""
	return result
}

// libraryFileName returns the platform-specific library filename.
func libraryFileName() string {
	switch runtime.GOOS {
	case "darwin":
		return "libllama.dylib"
	default:
		return "libllama.so"
	}
}

// DownloadLibraries downloads llama.cpp shared libraries to destDir.
// Automatically detects architecture and OS. Uses CPU processor by default.
func DownloadLibraries(ctx context.Context, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("creating lib directory: %w", err)
	}

	version, err := download.LlamaLatestVersion()
	if err != nil {
		return fmt.Errorf("getting latest llama.cpp version: %w", err)
	}

	arch := runtime.GOARCH
	goos := runtime.GOOS
	processor := "cpu"

	return download.GetWithContext(ctx, arch, goos, processor, version, destDir, download.ProgressTracker)
}

// DownloadEmbeddingModel downloads the default embedding model to destDir.
func DownloadEmbeddingModel(ctx context.Context, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("creating models directory: %w", err)
	}

	return download.GetModelWithContext(ctx, DefaultEmbeddingModelURL(), destDir, download.ProgressTracker)
}
