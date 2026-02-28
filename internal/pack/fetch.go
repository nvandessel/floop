package pack

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	// MaxPackSize is the maximum allowed download size (50MB).
	MaxPackSize = 50 << 20
	// FetchTimeout is the default HTTP timeout for downloads.
	FetchTimeout = 120 * time.Second
)

// FetchOptions configures pack file downloading.
type FetchOptions struct {
	CacheDir  string // override cache dir (default: ~/.floop/cache/packs)
	Force     bool   // re-download even if cached
	AuthToken string // optional Bearer token for authenticated downloads
}

// FetchResult reports the outcome of a fetch operation.
type FetchResult struct {
	LocalPath string // path to the downloaded file
	Cached    bool   // true if served from cache (no download)
	Size      int64  // file size in bytes
}

// DefaultCacheDir returns the default pack cache directory.
func DefaultCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".floop", "cache", "packs"), nil
}

// Fetch downloads a URL to the given cachePath. If the file already exists
// and Force is false, it returns immediately with Cached=true.
//
// Downloads use atomic write (tmp file + rename) to prevent partial files.
// File size is limited to MaxPackSize (50MB).
func Fetch(ctx context.Context, url string, cachePath string, opts FetchOptions) (*FetchResult, error) {
	// Check cache
	if !opts.Force {
		if info, err := os.Stat(cachePath); err == nil {
			return &FetchResult{
				LocalPath: cachePath,
				Cached:    true,
				Size:      info.Size(),
			}, nil
		}
	}

	// Ensure parent directory exists
	dir := filepath.Dir(cachePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating cache directory: %w", err)
	}

	// Download
	client := &http.Client{Timeout: FetchTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating download request: %w", err)
	}
	if opts.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+opts.AuthToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: HTTP %d from %s", resp.StatusCode, url)
	}

	// Atomic write: download to tmp, then rename
	tmpFile, err := os.CreateTemp(dir, "fpack-download-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath) // cleanup on error; no-op after successful rename
	}()

	// Copy with size limit
	n, err := io.Copy(tmpFile, io.LimitReader(resp.Body, MaxPackSize+1))
	if err != nil {
		return nil, fmt.Errorf("writing download: %w", err)
	}
	if n > MaxPackSize {
		return nil, fmt.Errorf("download exceeds maximum size (%dMB)", MaxPackSize>>20)
	}

	if err := tmpFile.Close(); err != nil {
		return nil, fmt.Errorf("closing temp file: %w", err)
	}

	// Rename to final path
	if err := os.Rename(tmpPath, cachePath); err != nil {
		return nil, fmt.Errorf("moving download to cache: %w", err)
	}

	return &FetchResult{
		LocalPath: cachePath,
		Cached:    false,
		Size:      n,
	}, nil
}
