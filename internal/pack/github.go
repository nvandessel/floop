package pack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GitHubRelease represents a GitHub release.
type GitHubRelease struct {
	TagName string        `json:"tag_name"`
	Name    string        `json:"name"`
	Assets  []GitHubAsset `json:"assets"`
}

// GitHubAsset represents a file attached to a release.
type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int    `json:"size"`
	ContentType        string `json:"content_type"`
}

// GitHubClient interacts with the GitHub REST API.
type GitHubClient struct {
	httpClient *http.Client
	token      string
	baseURL    string // for testing; defaults to https://api.github.com
}

// NewGitHubClient creates a GitHubClient with token resolved from environment.
//
// Token resolution order:
//  1. GITHUB_TOKEN env var
//  2. `gh auth token` command output
//  3. empty (unauthenticated, subject to rate limits)
func NewGitHubClient() *GitHubClient {
	return &GitHubClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      resolveGitHubToken(),
		baseURL:    "https://api.github.com",
	}
}

// newGitHubClientForTest creates a GitHubClient pointed at a test server.
func newGitHubClientForTest(baseURL, token string) *GitHubClient {
	return &GitHubClient{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		token:      token,
		baseURL:    baseURL,
	}
}

// ResolveRelease fetches release metadata from GitHub.
// If version is empty, it fetches the latest release.
// If version is set, it fetches the release tagged with that version.
func (c *GitHubClient) ResolveRelease(ctx context.Context, owner, repo, version string) (*GitHubRelease, error) {
	var endpoint string
	if version == "" {
		endpoint = fmt.Sprintf("%s/repos/%s/%s/releases/latest", c.baseURL, owner, repo)
	} else {
		endpoint = fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", c.baseURL, owner, repo, version)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching release: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit for JSON response
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		// success
	case http.StatusNotFound:
		if version != "" {
			return nil, fmt.Errorf("release %q not found for %s/%s", version, owner, repo)
		}
		return nil, fmt.Errorf("no releases found for %s/%s", owner, repo)
	case http.StatusForbidden:
		return nil, fmt.Errorf("GitHub API rate limit exceeded for %s/%s; set GITHUB_TOKEN env var to authenticate", owner, repo)
	default:
		return nil, fmt.Errorf("GitHub API error %d for %s/%s: %s", resp.StatusCode, owner, repo, string(body))
	}

	var release GitHubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("parsing release JSON: %w", err)
	}

	return &release, nil
}

// FindPackAssets returns all .fpack assets from a release.
func FindPackAssets(release *GitHubRelease) []GitHubAsset {
	var assets []GitHubAsset
	for _, a := range release.Assets {
		if strings.HasSuffix(a.Name, ".fpack") {
			assets = append(assets, a)
		}
	}
	return assets
}

// AssetDownloadURL returns the download URL for a release asset.
// It prefers browser_download_url which works without authentication.
func AssetDownloadURL(asset GitHubAsset) string {
	return asset.BrowserDownloadURL
}

// ReleaseVersion returns the version from a release tag, normalizing the v prefix.
func ReleaseVersion(release *GitHubRelease) string {
	return strings.TrimPrefix(release.TagName, "v")
}

// resolveGitHubToken tries to find a GitHub token from environment or gh CLI.
func resolveGitHubToken() string {
	// 1. GITHUB_TOKEN env var
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}

	// 2. gh auth token (with timeout to avoid hanging)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "auth", "token")
	out, err := cmd.Output()
	if err == nil {
		token := strings.TrimSpace(string(out))
		if token != "" {
			return token
		}
	}

	return ""
}

// CachePath returns the cache file path for a GitHub release asset.
func GitHubCachePath(cacheDir, owner, repo, version, assetName string) string {
	return filepath.Join(cacheDir, owner, repo, version, assetName)
}

// HTTPCachePath returns the cache file path for an HTTP URL download.
func HTTPCachePath(cacheDir, url string) string {
	// Use a simple hash of the URL for the filename
	h := fnvHash(url)
	return filepath.Join(cacheDir, "url", fmt.Sprintf("%x.fpack", h))
}

// fnvHash computes a simple FNV-1a hash for cache key generation.
func fnvHash(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}
