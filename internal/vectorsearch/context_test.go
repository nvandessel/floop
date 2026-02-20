package vectorsearch

import (
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
)

func TestComposeContextQuery_FullContext(t *testing.T) {
	ctx := models.ContextSnapshot{
		Task:         "development",
		FileLanguage: "go",
		FilePath:     "/home/user/project/internal/sqlite.go",
		ProjectType:  models.ProjectTypeGo,
		Environment:  "dev",
	}

	got := ComposeContextQuery(ctx)
	want := "development go editing sqlite.go in a go project dev environment"

	if got != want {
		t.Errorf("ComposeContextQuery() = %q, want %q", got, want)
	}
}

func TestComposeContextQuery_EmptyContext(t *testing.T) {
	ctx := models.ContextSnapshot{}

	got := ComposeContextQuery(ctx)
	want := "general software development"

	if got != want {
		t.Errorf("ComposeContextQuery() = %q, want %q", got, want)
	}
}

func TestComposeContextQuery_TaskOnly(t *testing.T) {
	ctx := models.ContextSnapshot{
		Task: "development",
	}

	got := ComposeContextQuery(ctx)
	want := "development"

	if got != want {
		t.Errorf("ComposeContextQuery() = %q, want %q", got, want)
	}
}

func TestComposeContextQuery_FileAndLanguage(t *testing.T) {
	ctx := models.ContextSnapshot{
		FileLanguage: "go",
		FilePath:     "/home/user/project/sqlite.go",
	}

	got := ComposeContextQuery(ctx)
	want := "go editing sqlite.go"

	if got != want {
		t.Errorf("ComposeContextQuery() = %q, want %q", got, want)
	}
}

func TestComposeContextQuery_UnknownProjectType(t *testing.T) {
	ctx := models.ContextSnapshot{
		Task:        "testing",
		ProjectType: models.ProjectTypeUnknown,
	}

	got := ComposeContextQuery(ctx)
	want := "testing"

	if got != want {
		t.Errorf("ComposeContextQuery() = %q, want %q (ProjectTypeUnknown should be excluded)", got, want)
	}
}

func TestComposeContextQuery_EnvironmentOnly(t *testing.T) {
	ctx := models.ContextSnapshot{
		Environment: "production",
	}

	got := ComposeContextQuery(ctx)
	want := "production environment"

	if got != want {
		t.Errorf("ComposeContextQuery() = %q, want %q", got, want)
	}
}

func TestComposeContextQuery_ProjectTypeIncluded(t *testing.T) {
	ctx := models.ContextSnapshot{
		Task:        "refactoring",
		ProjectType: models.ProjectTypeNode,
	}

	got := ComposeContextQuery(ctx)
	want := "refactoring in a node project"

	if got != want {
		t.Errorf("ComposeContextQuery() = %q, want %q", got, want)
	}
}
