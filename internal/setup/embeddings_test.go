package setup

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDetectInstalled(t *testing.T) {
	t.Run("returns not available when nothing exists", func(t *testing.T) {
		baseDir := t.TempDir()
		result := DetectInstalled(baseDir)
		if result.Available {
			t.Error("expected Available=false when nothing exists")
		}
		if result.LibPath != "" {
			t.Errorf("expected empty LibPath, got %q", result.LibPath)
		}
		if result.ModelPath != "" {
			t.Errorf("expected empty ModelPath, got %q", result.ModelPath)
		}
	})

	t.Run("detects lib directory", func(t *testing.T) {
		baseDir := t.TempDir()
		libDir := filepath.Join(baseDir, "lib")
		os.MkdirAll(libDir, 0755)

		// Create a fake library file
		libName := "libllama.so"
		if runtime.GOOS == "darwin" {
			libName = "libllama.dylib"
		}
		os.WriteFile(filepath.Join(libDir, libName), []byte("fake"), 0644)

		result := DetectInstalled(baseDir)
		if result.LibPath != libDir {
			t.Errorf("expected LibPath=%q, got %q", libDir, result.LibPath)
		}
		// Still not available (no model)
		if result.Available {
			t.Error("expected Available=false when only lib exists")
		}
	})

	t.Run("detects model file", func(t *testing.T) {
		baseDir := t.TempDir()
		modelsDir := filepath.Join(baseDir, "models")
		os.MkdirAll(modelsDir, 0755)

		modelFile := filepath.Join(modelsDir, "nomic-embed-text-v1.5.Q4_K_M.gguf")
		os.WriteFile(modelFile, []byte("fake"), 0644)

		result := DetectInstalled(baseDir)
		if result.ModelPath != modelFile {
			t.Errorf("expected ModelPath=%q, got %q", modelFile, result.ModelPath)
		}
		// Still not available (no lib)
		if result.Available {
			t.Error("expected Available=false when only model exists")
		}
	})

	t.Run("available when both lib and model exist", func(t *testing.T) {
		baseDir := t.TempDir()
		libDir := filepath.Join(baseDir, "lib")
		modelsDir := filepath.Join(baseDir, "models")
		os.MkdirAll(libDir, 0755)
		os.MkdirAll(modelsDir, 0755)

		libName := "libllama.so"
		if runtime.GOOS == "darwin" {
			libName = "libllama.dylib"
		}
		os.WriteFile(filepath.Join(libDir, libName), []byte("fake"), 0644)
		os.WriteFile(filepath.Join(modelsDir, "test.gguf"), []byte("fake"), 0644)

		result := DetectInstalled(baseDir)
		if !result.Available {
			t.Error("expected Available=true when both lib and model exist")
		}
		if result.LibPath != libDir {
			t.Errorf("expected LibPath=%q, got %q", libDir, result.LibPath)
		}
	})

	t.Run("picks first gguf file found", func(t *testing.T) {
		baseDir := t.TempDir()
		modelsDir := filepath.Join(baseDir, "models")
		os.MkdirAll(modelsDir, 0755)

		os.WriteFile(filepath.Join(modelsDir, "alpha.gguf"), []byte("a"), 0644)
		os.WriteFile(filepath.Join(modelsDir, "beta.gguf"), []byte("b"), 0644)

		result := DetectInstalled(baseDir)
		// Should find one of them
		if result.ModelPath == "" {
			t.Error("expected to find a model")
		}
		if filepath.Ext(result.ModelPath) != ".gguf" {
			t.Errorf("expected .gguf extension, got %q", result.ModelPath)
		}
	})
}

func TestDefaultFloopDir(t *testing.T) {
	dir := DefaultFloopDir()
	if dir == "" {
		t.Error("expected non-empty DefaultFloopDir")
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("expected absolute path, got %q", dir)
	}
}

func TestLibraryName(t *testing.T) {
	name := libraryFileName()
	if name == "" {
		t.Error("expected non-empty library filename")
	}
	// On linux should be .so, on darwin .dylib
	switch runtime.GOOS {
	case "linux":
		if name != "libllama.so" {
			t.Errorf("expected libllama.so on linux, got %q", name)
		}
	case "darwin":
		if name != "libllama.dylib" {
			t.Errorf("expected libllama.dylib on darwin, got %q", name)
		}
	}
}

func TestDefaultEmbeddingModelURL(t *testing.T) {
	url := DefaultEmbeddingModelURL()
	if url == "" {
		t.Error("expected non-empty URL")
	}
	if !strings.Contains(url, "nomic-embed-text") {
		t.Errorf("expected URL to contain nomic-embed-text, got %q", url)
	}
}
