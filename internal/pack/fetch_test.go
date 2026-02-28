package pack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetch_Download(t *testing.T) {
	content := "fake-fpack-content-for-testing"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(content))
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, "test.fpack")

	result, err := Fetch(context.Background(), srv.URL+"/test.fpack", cachePath, FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if result.Cached {
		t.Error("expected Cached = false for fresh download")
	}
	if result.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", result.Size, len(content))
	}
	if result.LocalPath != cachePath {
		t.Errorf("LocalPath = %q, want %q", result.LocalPath, cachePath)
	}

	// Verify file contents
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("reading cached file: %v", err)
	}
	if string(data) != content {
		t.Errorf("file content = %q, want %q", string(data), content)
	}
}

func TestFetch_CacheHit(t *testing.T) {
	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, "cached.fpack")

	// Pre-populate cache
	if err := os.WriteFile(cachePath, []byte("cached-content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Server should not be called
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called for cache hit")
	}))
	defer srv.Close()

	result, err := Fetch(context.Background(), srv.URL+"/test.fpack", cachePath, FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if !result.Cached {
		t.Error("expected Cached = true for cache hit")
	}
}

func TestFetch_ForceRedownload(t *testing.T) {
	newContent := "new-content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(newContent))
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, "force.fpack")

	// Pre-populate cache with old content
	if err := os.WriteFile(cachePath, []byte("old-content"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Fetch(context.Background(), srv.URL+"/test.fpack", cachePath, FetchOptions{Force: true})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if result.Cached {
		t.Error("expected Cached = false for force redownload")
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != newContent {
		t.Errorf("content = %q, want %q", string(data), newContent)
	}
}

func TestFetch_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cachePath := filepath.Join(t.TempDir(), "fail.fpack")

	_, err := Fetch(context.Background(), srv.URL+"/missing.fpack", cachePath, FetchOptions{})
	if err == nil {
		t.Fatal("expected error for HTTP 404")
	}
}

func TestFetch_SizeLimit(t *testing.T) {
	// Serve content larger than MaxPackSize
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Write just over the limit
		w.Write([]byte(strings.Repeat("x", MaxPackSize+1)))
	}))
	defer srv.Close()

	cachePath := filepath.Join(t.TempDir(), "big.fpack")

	_, err := Fetch(context.Background(), srv.URL+"/big.fpack", cachePath, FetchOptions{})
	if err == nil {
		t.Fatal("expected error for oversized download")
	}
}

func TestFetch_AuthToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer my-token" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer my-token")
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cachePath := filepath.Join(t.TempDir(), "auth.fpack")

	_, err := Fetch(context.Background(), srv.URL+"/test.fpack", cachePath, FetchOptions{AuthToken: "my-token"})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
}

func TestFetch_CreatesParentDirs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("content"))
	}))
	defer srv.Close()

	cachePath := filepath.Join(t.TempDir(), "deep", "nested", "dir", "test.fpack")

	result, err := Fetch(context.Background(), srv.URL+"/test.fpack", cachePath, FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.LocalPath != cachePath {
		t.Errorf("LocalPath = %q, want %q", result.LocalPath, cachePath)
	}
}

func TestDefaultCacheDir(t *testing.T) {
	dir, err := DefaultCacheDir()
	if err != nil {
		t.Fatalf("DefaultCacheDir() error = %v", err)
	}
	if !strings.Contains(dir, ".floop") {
		t.Errorf("DefaultCacheDir() = %q, want to contain '.floop'", dir)
	}
}
