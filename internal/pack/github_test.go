package pack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveRelease_Latest(t *testing.T) {
	release := GitHubRelease{
		TagName: "v1.0.0",
		Name:    "Release 1.0.0",
		Assets: []GitHubAsset{
			{Name: "floop-core.fpack", BrowserDownloadURL: "https://example.com/floop-core.fpack", Size: 1024},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/test-owner/test-repo/releases/latest" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(release)
	}))
	defer srv.Close()

	client := newGitHubClientForTest(srv.URL, "")
	got, err := client.ResolveRelease(context.Background(), "test-owner", "test-repo", "")
	if err != nil {
		t.Fatalf("ResolveRelease() error = %v", err)
	}

	if got.TagName != "v1.0.0" {
		t.Errorf("TagName = %q, want %q", got.TagName, "v1.0.0")
	}
	if len(got.Assets) != 1 {
		t.Errorf("Assets = %d, want 1", len(got.Assets))
	}
}

func TestResolveRelease_SpecificVersion(t *testing.T) {
	release := GitHubRelease{
		TagName: "v2.0.0",
		Name:    "Release 2.0.0",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases/tags/v2.0.0" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(release)
	}))
	defer srv.Close()

	client := newGitHubClientForTest(srv.URL, "")
	got, err := client.ResolveRelease(context.Background(), "owner", "repo", "v2.0.0")
	if err != nil {
		t.Fatalf("ResolveRelease() error = %v", err)
	}

	if got.TagName != "v2.0.0" {
		t.Errorf("TagName = %q, want %q", got.TagName, "v2.0.0")
	}
}

func TestResolveRelease_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := newGitHubClientForTest(srv.URL, "")
	_, err := client.ResolveRelease(context.Background(), "owner", "repo", "v99.0.0")
	if err == nil {
		t.Fatal("expected error for not found release")
	}
}

func TestResolveRelease_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := newGitHubClientForTest(srv.URL, "")
	_, err := client.ResolveRelease(context.Background(), "owner", "repo", "")
	if err == nil {
		t.Fatal("expected error for rate limit")
	}
	if got := err.Error(); !contains(got, "rate limit") {
		t.Errorf("error = %q, want to contain 'rate limit'", got)
	}
}

func TestResolveRelease_AuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer test-token")
		}
		json.NewEncoder(w).Encode(GitHubRelease{TagName: "v1.0.0"})
	}))
	defer srv.Close()

	client := newGitHubClientForTest(srv.URL, "test-token")
	_, err := client.ResolveRelease(context.Background(), "owner", "repo", "")
	if err != nil {
		t.Fatalf("ResolveRelease() error = %v", err)
	}
}

func TestFindPackAssets(t *testing.T) {
	tests := []struct {
		name   string
		assets []GitHubAsset
		want   int
	}{
		{
			name:   "no assets",
			assets: nil,
			want:   0,
		},
		{
			name: "no fpack assets",
			assets: []GitHubAsset{
				{Name: "README.md"},
				{Name: "checksums.txt"},
			},
			want: 0,
		},
		{
			name: "one fpack asset",
			assets: []GitHubAsset{
				{Name: "floop-core.fpack"},
				{Name: "checksums.txt"},
			},
			want: 1,
		},
		{
			name: "multiple fpack assets",
			assets: []GitHubAsset{
				{Name: "floop-core.fpack"},
				{Name: "floop-testing.fpack"},
				{Name: "checksums.txt"},
			},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := &GitHubRelease{Assets: tt.assets}
			got := FindPackAssets(release)
			if len(got) != tt.want {
				t.Errorf("FindPackAssets() = %d assets, want %d", len(got), tt.want)
			}
		})
	}
}

func TestReleaseVersion(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		{"v1.0.0", "1.0.0"},
		{"1.0.0", "1.0.0"},
		{"v0.1.0-beta", "0.1.0-beta"},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			release := &GitHubRelease{TagName: tt.tag}
			if got := ReleaseVersion(release); got != tt.want {
				t.Errorf("ReleaseVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGitHubCachePath(t *testing.T) {
	got := GitHubCachePath("/cache", "owner", "repo", "v1.0.0", "pack.fpack")
	want := "/cache/owner/repo/v1.0.0/pack.fpack"
	if got != want {
		t.Errorf("GitHubCachePath() = %q, want %q", got, want)
	}
}

func TestHTTPCachePath(t *testing.T) {
	p1 := HTTPCachePath("/cache", "https://example.com/a.fpack")
	p2 := HTTPCachePath("/cache", "https://example.com/b.fpack")
	if p1 == p2 {
		t.Error("different URLs should produce different cache paths")
	}
}

// contains is defined in format_test.go (same package)
