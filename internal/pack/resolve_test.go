package pack

import (
	"path/filepath"
	"testing"
)

func TestResolveSource(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		wantKind  SourceKind
		wantOwner string
		wantRepo  string
		wantVer   string
		wantURL   string
		wantErr   bool
	}{
		{
			name:    "empty source",
			source:  "",
			wantErr: true,
		},
		{
			name:      "github latest",
			source:    "gh:nvandessel/floop",
			wantKind:  SourceGitHub,
			wantOwner: "nvandessel",
			wantRepo:  "floop",
		},
		{
			name:      "github with version",
			source:    "gh:nvandessel/floop@v1.2.3",
			wantKind:  SourceGitHub,
			wantOwner: "nvandessel",
			wantRepo:  "floop",
			wantVer:   "v1.2.3",
		},
		{
			name:    "github missing repo",
			source:  "gh:nvandessel",
			wantErr: true,
		},
		{
			name:    "github empty after prefix",
			source:  "gh:",
			wantErr: true,
		},
		{
			name:    "github empty owner",
			source:  "gh:/repo",
			wantErr: true,
		},
		{
			name:    "github empty repo",
			source:  "gh:owner/",
			wantErr: true,
		},
		{
			name:    "github empty version after @",
			source:  "gh:owner/repo@",
			wantErr: true,
		},
		{
			name:     "https url",
			source:   "https://example.com/pack.fpack",
			wantKind: SourceHTTP,
			wantURL:  "https://example.com/pack.fpack",
		},
		{
			name:     "http url",
			source:   "http://example.com/pack.fpack",
			wantKind: SourceHTTP,
			wantURL:  "http://example.com/pack.fpack",
		},
		{
			name:     "local relative path",
			source:   "./my-pack.fpack",
			wantKind: SourceLocal,
		},
		{
			name:     "local absolute path",
			source:   "/tmp/my-pack.fpack",
			wantKind: SourceLocal,
		},
		{
			name:     "local filename only",
			source:   "my-pack.fpack",
			wantKind: SourceLocal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveSource(tt.source)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveSource() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if got.Kind != tt.wantKind {
				t.Errorf("Kind = %v, want %v", got.Kind, tt.wantKind)
			}
			if got.Raw != tt.source {
				t.Errorf("Raw = %q, want %q", got.Raw, tt.source)
			}

			switch tt.wantKind {
			case SourceGitHub:
				if got.Owner != tt.wantOwner {
					t.Errorf("Owner = %q, want %q", got.Owner, tt.wantOwner)
				}
				if got.Repo != tt.wantRepo {
					t.Errorf("Repo = %q, want %q", got.Repo, tt.wantRepo)
				}
				if got.Version != tt.wantVer {
					t.Errorf("Version = %q, want %q", got.Version, tt.wantVer)
				}
			case SourceHTTP:
				if got.URL != tt.wantURL {
					t.Errorf("URL = %q, want %q", got.URL, tt.wantURL)
				}
			case SourceLocal:
				if !filepath.IsAbs(got.FilePath) {
					t.Errorf("FilePath = %q, want absolute path", got.FilePath)
				}
			}
		})
	}
}

func TestResolveSource_Canonical(t *testing.T) {
	tests := []struct {
		name          string
		source        string
		wantCanonical string
	}{
		{
			name:          "github canonical normalizes",
			source:        "gh:owner/repo",
			wantCanonical: "gh:owner/repo",
		},
		{
			name:          "github canonical with version",
			source:        "gh:owner/repo@v1.0.0",
			wantCanonical: "gh:owner/repo@v1.0.0",
		},
		{
			name:          "http canonical is identity",
			source:        "https://example.com/pack.fpack",
			wantCanonical: "https://example.com/pack.fpack",
		},
		{
			name:          "local canonical is absolute",
			source:        "/tmp/test.fpack",
			wantCanonical: "/tmp/test.fpack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveSource(tt.source)
			if err != nil {
				t.Fatalf("ResolveSource() error = %v", err)
			}
			if got.Canonical != tt.wantCanonical {
				t.Errorf("Canonical = %q, want %q", got.Canonical, tt.wantCanonical)
			}
		})
	}
}

func TestSourceKind_String(t *testing.T) {
	tests := []struct {
		kind SourceKind
		want string
	}{
		{SourceLocal, "local"},
		{SourceHTTP, "http"},
		{SourceGitHub, "github"},
		{SourceKind(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
