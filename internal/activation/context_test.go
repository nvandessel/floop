package activation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
)

func TestContextBuilder_Build(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*ContextBuilder)
		wantFile string
		wantLang string
		wantTask string
	}{
		{
			name: "empty builder",
			setup: func(b *ContextBuilder) {
				// no setup
			},
			wantFile: "",
			wantLang: "",
			wantTask: "",
		},
		{
			name: "with go file",
			setup: func(b *ContextBuilder) {
				b.WithFile("internal/models/behavior.go")
			},
			wantFile: "internal/models/behavior.go",
			wantLang: "go",
			wantTask: "",
		},
		{
			name: "with python file",
			setup: func(b *ContextBuilder) {
				b.WithFile("scripts/deploy.py")
			},
			wantFile: "scripts/deploy.py",
			wantLang: "python",
			wantTask: "",
		},
		{
			name: "with task",
			setup: func(b *ContextBuilder) {
				b.WithTask("refactor")
			},
			wantFile: "",
			wantLang: "",
			wantTask: "refactor",
		},
		{
			name: "with file and task",
			setup: func(b *ContextBuilder) {
				b.WithFile("main.go").WithTask("debug")
			},
			wantFile: "main.go",
			wantLang: "go",
			wantTask: "debug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewContextBuilder()
			tt.setup(builder)
			ctx := builder.Build()

			if ctx.FilePath != tt.wantFile {
				t.Errorf("FilePath = %v, want %v", ctx.FilePath, tt.wantFile)
			}
			if ctx.FileLanguage != tt.wantLang {
				t.Errorf("FileLanguage = %v, want %v", ctx.FileLanguage, tt.wantLang)
			}
			if ctx.Task != tt.wantTask {
				t.Errorf("Task = %v, want %v", ctx.Task, tt.wantTask)
			}
		})
	}
}

func TestContextBuilder_WithCustom(t *testing.T) {
	builder := NewContextBuilder()
	builder.WithCustom("project_type", "cli")
	builder.WithCustom("team", "platform")

	ctx := builder.Build()

	if ctx.Custom["project_type"] != "cli" {
		t.Errorf("Custom[project_type] = %v, want cli", ctx.Custom["project_type"])
	}
	if ctx.Custom["team"] != "platform" {
		t.Errorf("Custom[team] = %v, want platform", ctx.Custom["team"])
	}
}

func TestContextBuilder_Chaining(t *testing.T) {
	ctx := NewContextBuilder().
		WithFile("src/main.go").
		WithTask("implement").
		WithEnvironment("dev").
		WithRepoRoot("/tmp/test").
		WithCustom("priority", "high").
		Build()

	if ctx.FilePath != "src/main.go" {
		t.Errorf("FilePath = %v, want src/main.go", ctx.FilePath)
	}
	if ctx.Task != "implement" {
		t.Errorf("Task = %v, want implement", ctx.Task)
	}
	if ctx.Environment != "dev" {
		t.Errorf("Environment = %v, want dev", ctx.Environment)
	}
	if ctx.Custom["priority"] != "high" {
		t.Errorf("Custom[priority] = %v, want high", ctx.Custom["priority"])
	}
}

func TestDetectEnvironment(t *testing.T) {
	// Save original env vars to restore later
	origCI := os.Getenv("CI")
	origGitHub := os.Getenv("GITHUB_ACTIONS")
	origGitLab := os.Getenv("GITLAB_CI")
	origJenkins := os.Getenv("JENKINS_URL")
	origCircle := os.Getenv("CIRCLECI")
	origTravis := os.Getenv("TRAVIS")

	// Cleanup function
	cleanup := func() {
		os.Setenv("CI", origCI)
		os.Setenv("GITHUB_ACTIONS", origGitHub)
		os.Setenv("GITLAB_CI", origGitLab)
		os.Setenv("JENKINS_URL", origJenkins)
		os.Setenv("CIRCLECI", origCircle)
		os.Setenv("TRAVIS", origTravis)
	}
	defer cleanup()

	tests := []struct {
		name  string
		setup func()
		want  string
	}{
		{
			name: "development - no CI vars",
			setup: func() {
				os.Unsetenv("CI")
				os.Unsetenv("GITHUB_ACTIONS")
				os.Unsetenv("GITLAB_CI")
				os.Unsetenv("JENKINS_URL")
				os.Unsetenv("CIRCLECI")
				os.Unsetenv("TRAVIS")
			},
			want: "development",
		},
		{
			name: "github actions",
			setup: func() {
				os.Unsetenv("CI")
				os.Setenv("GITHUB_ACTIONS", "true")
				os.Unsetenv("GITLAB_CI")
				os.Unsetenv("JENKINS_URL")
				os.Unsetenv("CIRCLECI")
				os.Unsetenv("TRAVIS")
			},
			want: "github-actions",
		},
		{
			name: "gitlab ci",
			setup: func() {
				os.Unsetenv("CI")
				os.Unsetenv("GITHUB_ACTIONS")
				os.Setenv("GITLAB_CI", "true")
				os.Unsetenv("JENKINS_URL")
				os.Unsetenv("CIRCLECI")
				os.Unsetenv("TRAVIS")
			},
			want: "gitlab-ci",
		},
		{
			name: "jenkins",
			setup: func() {
				os.Unsetenv("CI")
				os.Unsetenv("GITHUB_ACTIONS")
				os.Unsetenv("GITLAB_CI")
				os.Setenv("JENKINS_URL", "http://jenkins.example.com")
				os.Unsetenv("CIRCLECI")
				os.Unsetenv("TRAVIS")
			},
			want: "jenkins",
		},
		{
			name: "circleci",
			setup: func() {
				os.Unsetenv("CI")
				os.Unsetenv("GITHUB_ACTIONS")
				os.Unsetenv("GITLAB_CI")
				os.Unsetenv("JENKINS_URL")
				os.Setenv("CIRCLECI", "true")
				os.Unsetenv("TRAVIS")
			},
			want: "circleci",
		},
		{
			name: "travis",
			setup: func() {
				os.Unsetenv("CI")
				os.Unsetenv("GITHUB_ACTIONS")
				os.Unsetenv("GITLAB_CI")
				os.Unsetenv("JENKINS_URL")
				os.Unsetenv("CIRCLECI")
				os.Setenv("TRAVIS", "true")
			},
			want: "travis",
		},
		{
			name: "generic CI true",
			setup: func() {
				os.Setenv("CI", "true")
				os.Unsetenv("GITHUB_ACTIONS")
				os.Unsetenv("GITLAB_CI")
				os.Unsetenv("JENKINS_URL")
				os.Unsetenv("CIRCLECI")
				os.Unsetenv("TRAVIS")
			},
			want: "ci",
		},
		{
			name: "generic CI 1",
			setup: func() {
				os.Setenv("CI", "1")
				os.Unsetenv("GITHUB_ACTIONS")
				os.Unsetenv("GITLAB_CI")
				os.Unsetenv("JENKINS_URL")
				os.Unsetenv("CIRCLECI")
				os.Unsetenv("TRAVIS")
			},
			want: "ci",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			got := detectEnvironment()
			if got != tt.want {
				t.Errorf("detectEnvironment() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContextBuilder_Build_WithProjectType(t *testing.T) {
	// Create a temp directory with go.mod
	dir, err := os.MkdirTemp("", "context_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	// Create go.mod file
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644); err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}

	ctx := NewContextBuilder().
		WithRepoRoot(dir).
		Build()

	if ctx.ProjectType != models.ProjectTypeGo {
		t.Errorf("ProjectType = %v, want %v", ctx.ProjectType, models.ProjectTypeGo)
	}
}

func TestContextBuilder_Build_EnvironmentAutoDetect(t *testing.T) {
	// Save original env vars
	origCI := os.Getenv("CI")
	origGitHub := os.Getenv("GITHUB_ACTIONS")
	origFloopEnv := os.Getenv("FLOOP_ENV")
	defer func() {
		os.Setenv("CI", origCI)
		os.Setenv("GITHUB_ACTIONS", origGitHub)
		if origFloopEnv == "" {
			os.Unsetenv("FLOOP_ENV")
		} else {
			os.Setenv("FLOOP_ENV", origFloopEnv)
		}
	}()

	// Test 1: No env override, no CI - should be development
	os.Unsetenv("FLOOP_ENV")
	os.Unsetenv("CI")
	os.Unsetenv("GITHUB_ACTIONS")
	ctx := NewContextBuilder().Build()
	if ctx.Environment != "development" {
		t.Errorf("Environment = %q, want %q", ctx.Environment, "development")
	}

	// Test 2: CI=true, no override - should be ci
	os.Setenv("CI", "true")
	ctx = NewContextBuilder().Build()
	if ctx.Environment != "ci" {
		t.Errorf("Environment = %q, want %q", ctx.Environment, "ci")
	}

	// Test 3: Manual override takes precedence
	ctx = NewContextBuilder().WithEnvironment("staging").Build()
	if ctx.Environment != "staging" {
		t.Errorf("Environment = %q, want %q", ctx.Environment, "staging")
	}
}
