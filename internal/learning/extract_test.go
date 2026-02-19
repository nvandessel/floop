package learning

import (
	"strings"
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
)

func TestNewBehaviorExtractor(t *testing.T) {
	extractor := NewBehaviorExtractor()
	if extractor == nil {
		t.Fatal("NewBehaviorExtractor returned nil")
	}
}

func TestBehaviorExtractor_Extract(t *testing.T) {
	extractor := NewBehaviorExtractor()

	tests := []struct {
		name       string
		correction models.Correction
		wantKind   models.BehaviorKind
		wantName   string
		wantID     string
	}{
		{
			name: "basic directive extraction",
			correction: models.Correction{
				ID:              "corr-123",
				AgentAction:     "used pip install",
				CorrectedAction: "use uv instead",
				Context: models.ContextSnapshot{
					FileLanguage: "python",
					FilePath:     "requirements.txt",
				},
			},
			wantKind: models.BehaviorKindPreference, // contains "use" and "instead"
			wantName: "learned/use-uv-instead",
		},
		{
			name: "constraint with never keyword",
			correction: models.Correction{
				ID:              "corr-456",
				AgentAction:     "committed secrets",
				CorrectedAction: "never commit secrets to git",
				Context: models.ContextSnapshot{
					FilePath: "src/.env",
				},
			},
			wantKind: models.BehaviorKindConstraint,
			wantName: "learned/never-commit-secrets-to-git",
		},
		{
			name: "preference with prefer keyword",
			correction: models.Correction{
				ID:              "corr-789",
				AgentAction:     "used os.path",
				CorrectedAction: "prefer pathlib.Path over os.path",
				Context: models.ContextSnapshot{
					FileLanguage: "python",
				},
			},
			wantKind: models.BehaviorKindPreference,
			wantName: "learned/prefer-pathlib-path-over-os-path",
		},
		{
			name: "procedure with step keywords",
			correction: models.Correction{
				ID:              "corr-abc",
				AgentAction:     "deployed directly",
				CorrectedAction: "first run tests, then deploy to staging",
				Context: models.ContextSnapshot{
					Task:        "deploy",
					Environment: "prod",
				},
			},
			wantKind: models.BehaviorKindProcedure,
			wantName: "learned/first-run-tests-then-deploy-to-staging",
		},
		{
			name: "long name gets truncated",
			correction: models.Correction{
				ID:              "corr-long",
				AgentAction:     "short action",
				CorrectedAction: "this is a very long corrected action that should be truncated to fit within fifty characters maximum",
				Context:         models.ContextSnapshot{},
			},
			wantKind: models.BehaviorKindDirective,
			wantName: "learned/this-is-a-very-long-corrected-action-that-should-b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			behavior, err := extractor.Extract(tt.correction)
			if err != nil {
				t.Fatalf("Extract() error = %v", err)
			}

			if behavior == nil {
				t.Fatal("Extract() returned nil behavior")
			}

			// Check kind
			if behavior.Kind != tt.wantKind {
				t.Errorf("Kind = %v, want %v", behavior.Kind, tt.wantKind)
			}

			// Check name
			if behavior.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", behavior.Name, tt.wantName)
			}

			// Check ID is generated and has correct prefix
			if !strings.HasPrefix(behavior.ID, "behavior-") {
				t.Errorf("ID = %q, want prefix 'behavior-'", behavior.ID)
			}

			// Check provenance
			if behavior.Provenance.SourceType != models.SourceTypeLearned {
				t.Errorf("Provenance.SourceType = %v, want %v",
					behavior.Provenance.SourceType, models.SourceTypeLearned)
			}
			if behavior.Provenance.CorrectionID != tt.correction.ID {
				t.Errorf("Provenance.CorrectionID = %v, want %v",
					behavior.Provenance.CorrectionID, tt.correction.ID)
			}

			// Check content
			if behavior.Content.Canonical != tt.correction.CorrectedAction {
				t.Errorf("Content.Canonical = %q, want %q",
					behavior.Content.Canonical, tt.correction.CorrectedAction)
			}

			// Check confidence starts at 0.6
			if behavior.Confidence != 0.6 {
				t.Errorf("Confidence = %v, want 0.6", behavior.Confidence)
			}
		})
	}
}

func TestBehaviorExtractor_GenerateID(t *testing.T) {
	extractor := NewBehaviorExtractor().(*behaviorExtractor)

	tests := []struct {
		name        string
		correction1 models.Correction
		correction2 models.Correction
		wantSameID  bool
	}{
		{
			name: "same content generates same ID",
			correction1: models.Correction{
				AgentAction:     "action A",
				CorrectedAction: "action B",
			},
			correction2: models.Correction{
				AgentAction:     "action A",
				CorrectedAction: "action B",
			},
			wantSameID: true,
		},
		{
			name: "different content generates different ID",
			correction1: models.Correction{
				AgentAction:     "action A",
				CorrectedAction: "action B",
			},
			correction2: models.Correction{
				AgentAction:     "action C",
				CorrectedAction: "action D",
			},
			wantSameID: false,
		},
		{
			name: "context does not affect ID",
			correction1: models.Correction{
				AgentAction:     "action A",
				CorrectedAction: "action B",
				Context: models.ContextSnapshot{
					FileLanguage: "go",
				},
			},
			correction2: models.Correction{
				AgentAction:     "action A",
				CorrectedAction: "action B",
				Context: models.ContextSnapshot{
					FileLanguage: "python",
				},
			},
			wantSameID: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id1 := extractor.generateID(tt.correction1)
			id2 := extractor.generateID(tt.correction2)

			if tt.wantSameID && id1 != id2 {
				t.Errorf("expected same ID, got %q and %q", id1, id2)
			}
			if !tt.wantSameID && id1 == id2 {
				t.Errorf("expected different IDs, got same: %q", id1)
			}
		})
	}
}

func TestBehaviorExtractor_InferWhen(t *testing.T) {
	extractor := NewBehaviorExtractor().(*behaviorExtractor)

	tests := []struct {
		name    string
		context models.ContextSnapshot
		want    map[string]interface{}
	}{
		{
			name: "language only",
			context: models.ContextSnapshot{
				FileLanguage: "go",
			},
			want: map[string]interface{}{
				"language": "go",
			},
		},
		{
			name: "multiple fields with known task",
			context: models.ContextSnapshot{
				FileLanguage: "python",
				Task:         "testing",
				Environment:  "dev",
			},
			want: map[string]interface{}{
				"language": "python",
				"task":     "testing",
			},
		},
		{
			name: "environment is excluded",
			context: models.ContextSnapshot{
				FileLanguage: "go",
				Environment:  "production",
			},
			want: map[string]interface{}{
				"language": "go",
			},
		},
		{
			name: "file path generalization",
			context: models.ContextSnapshot{
				FilePath: "db/migrations/001.sql",
			},
			want: map[string]interface{}{
				"file_path": "db/*",
			},
		},
		{
			name: "skip common dirs like src",
			context: models.ContextSnapshot{
				FilePath: "src/db/models/user.go",
			},
			want: map[string]interface{}{
				"file_path": "db/*",
			},
		},
		{
			name: "unknown task excluded - development",
			context: models.ContextSnapshot{
				FileLanguage: "go",
				Task:         "development",
			},
			want: map[string]interface{}{
				"language": "go",
				// task "development" is not in knownTasks
			},
		},
		{
			name: "unknown task excluded - coding",
			context: models.ContextSnapshot{
				Task: "coding",
			},
			want: map[string]interface{}{},
		},
		{
			name: "unknown task excluded - configuration",
			context: models.ContextSnapshot{
				Task: "configuration",
			},
			want: map[string]interface{}{},
		},
		{
			name: "fine-grained task included - testing",
			context: models.ContextSnapshot{
				Task: "testing",
			},
			want: map[string]interface{}{
				"task": "testing",
			},
		},
		{
			name: "fine-grained task included - committing",
			context: models.ContextSnapshot{
				Task: "committing",
			},
			want: map[string]interface{}{
				"task": "committing",
			},
		},
		{
			name:    "empty context",
			context: models.ContextSnapshot{},
			want:    map[string]interface{}{},
		},
		{
			name: "single file without path",
			context: models.ContextSnapshot{
				FilePath:     "main.go",
				FileLanguage: "go",
			},
			want: map[string]interface{}{
				"language": "go",
				// No file_path because single file without directory
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractor.inferWhen(tt.context)

			// Check length
			if len(got) != len(tt.want) {
				t.Errorf("inferWhen() returned %d fields, want %d", len(got), len(tt.want))
			}

			// Check each expected field
			for key, wantVal := range tt.want {
				gotVal, ok := got[key]
				if !ok {
					t.Errorf("missing key %q in result", key)
					continue
				}
				if gotVal != wantVal {
					t.Errorf("got[%q] = %v, want %v", key, gotVal, wantVal)
				}
			}
		})
	}
}

func TestBehaviorExtractor_GeneralizeFilePath(t *testing.T) {
	extractor := NewBehaviorExtractor().(*behaviorExtractor)

	tests := []struct {
		filePath string
		want     string
	}{
		{"db/migrations/001.sql", "db/*"},
		{"src/db/models/user.go", "db/*"},
		{"internal/store/memory.go", "store/*"},
		{"pkg/types/behavior.go", "types/*"},
		{"main.go", ""},                                 // Single file
		{"lib/utils/helper.py", "utils/*"},              // Skip lib
		{"app/controllers/user.rb", "controllers/*"},    // Skip app
		{"tests/unit/test_user.py", "tests/*"},          // tests is significant
		{"src/internal/models/behavior.go", "models/*"}, // Skip both src and internal
	}

	for _, tt := range tests {
		t.Run(tt.filePath, func(t *testing.T) {
			got := extractor.generalizeFilePath(tt.filePath)
			if got != tt.want {
				t.Errorf("generalizeFilePath(%q) = %q, want %q", tt.filePath, got, tt.want)
			}
		})
	}
}

func TestBehaviorExtractor_InferKind(t *testing.T) {
	extractor := NewBehaviorExtractor().(*behaviorExtractor)

	tests := []struct {
		name       string
		correction models.Correction
		want       models.BehaviorKind
	}{
		{
			name: "constraint - never",
			correction: models.Correction{
				CorrectedAction: "Never push directly to main",
			},
			want: models.BehaviorKindConstraint,
		},
		{
			name: "constraint - don't",
			correction: models.Correction{
				CorrectedAction: "Don't use global variables",
			},
			want: models.BehaviorKindConstraint,
		},
		{
			name: "constraint - must not",
			correction: models.Correction{
				CorrectedAction: "You must not skip tests",
			},
			want: models.BehaviorKindConstraint,
		},
		{
			name: "constraint - forbidden",
			correction: models.Correction{
				CorrectedAction: "Hardcoded passwords are forbidden",
			},
			want: models.BehaviorKindConstraint,
		},
		{
			name: "constraint - avoid",
			correction: models.Correction{
				CorrectedAction: "Avoid using eval()",
			},
			want: models.BehaviorKindConstraint,
		},
		{
			name: "preference - prefer",
			correction: models.Correction{
				CorrectedAction: "Prefer composition over inheritance",
			},
			want: models.BehaviorKindPreference,
		},
		{
			name: "preference - instead of",
			correction: models.Correction{
				CorrectedAction: "Use uv instead of pip",
			},
			want: models.BehaviorKindPreference,
		},
		{
			name: "preference - rather than",
			correction: models.Correction{
				CorrectedAction: "Use context rather than globals",
			},
			want: models.BehaviorKindPreference,
		},
		{
			name: "procedure - first then",
			correction: models.Correction{
				CorrectedAction: "First write tests, then implement",
			},
			want: models.BehaviorKindProcedure,
		},
		{
			name: "procedure - step 1",
			correction: models.Correction{
				CorrectedAction: "Step 1: read the spec",
			},
			want: models.BehaviorKindProcedure,
		},
		{
			name: "procedure - workflow",
			correction: models.Correction{
				CorrectedAction: "Follow the deployment workflow",
			},
			want: models.BehaviorKindProcedure,
		},
		{
			name: "directive - simple action",
			correction: models.Correction{
				CorrectedAction: "Run go fmt before committing",
			},
			want: models.BehaviorKindDirective,
		},
		{
			name: "preference - implicit from correction pair with use",
			correction: models.Correction{
				AgentAction:     "wrote manual SQL",
				CorrectedAction: "use the ORM instead",
			},
			want: models.BehaviorKindPreference,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractor.inferKind(tt.correction)
			if got != tt.want {
				t.Errorf("inferKind() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBehaviorExtractor_GenerateName(t *testing.T) {
	extractor := NewBehaviorExtractor().(*behaviorExtractor)

	tests := []struct {
		name       string
		correction models.Correction
		want       string
	}{
		{
			name: "simple name",
			correction: models.Correction{
				CorrectedAction: "Use uv",
			},
			want: "learned/use-uv",
		},
		{
			name: "name with special characters",
			correction: models.Correction{
				CorrectedAction: "Use pathlib.Path (not os.path)",
			},
			want: "learned/use-pathlib-path-not-os-path",
		},
		{
			name: "name with newlines",
			correction: models.Correction{
				CorrectedAction: "Line one\nLine two",
			},
			want: "learned/line-one-line-two",
		},
		{
			name: "long name truncation",
			correction: models.Correction{
				CorrectedAction: "This is a very long action description that exceeds fifty characters",
			},
			want: "learned/this-is-a-very-long-action-description-that-exceed",
		},
		{
			name: "name with multiple spaces",
			correction: models.Correction{
				CorrectedAction: "Use    spaces   correctly",
			},
			want: "learned/use-spaces-correctly",
		},
		{
			name: "name with quotes",
			correction: models.Correction{
				CorrectedAction: `Use "proper" quotes`,
			},
			want: "learned/use-proper-quotes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractor.generateName(tt.correction)
			if got != tt.want {
				t.Errorf("generateName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBehaviorExtractor_BuildContent(t *testing.T) {
	extractor := NewBehaviorExtractor().(*behaviorExtractor)

	tests := []struct {
		name       string
		correction models.Correction
		wantAvoid  string
		wantPrefer string
	}{
		{
			name: "with both actions",
			correction: models.Correction{
				AgentAction:     "used pip",
				CorrectedAction: "use uv instead",
			},
			wantAvoid:  "used pip",
			wantPrefer: "use uv instead",
		},
		{
			name: "without agent action",
			correction: models.Correction{
				AgentAction:     "",
				CorrectedAction: "always run tests",
			},
			wantAvoid:  "",
			wantPrefer: "always run tests",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := extractor.buildContent(tt.correction)

			// Check canonical
			if content.Canonical != tt.correction.CorrectedAction {
				t.Errorf("Canonical = %q, want %q", content.Canonical, tt.correction.CorrectedAction)
			}

			// Check structured.prefer
			if prefer, ok := content.Structured["prefer"]; ok {
				if prefer != tt.wantPrefer {
					t.Errorf("Structured[prefer] = %q, want %q", prefer, tt.wantPrefer)
				}
			} else {
				t.Error("missing Structured[prefer]")
			}

			// Check structured.avoid
			if tt.wantAvoid != "" {
				if avoid, ok := content.Structured["avoid"]; ok {
					if avoid != tt.wantAvoid {
						t.Errorf("Structured[avoid] = %q, want %q", avoid, tt.wantAvoid)
					}
				} else {
					t.Error("missing Structured[avoid]")
				}
			} else {
				if _, ok := content.Structured["avoid"]; ok {
					t.Error("Structured[avoid] should not be set when AgentAction is empty")
				}
			}

			// Check expanded contains the correction info
			if tt.wantAvoid != "" {
				if !strings.Contains(content.Expanded, tt.wantAvoid) {
					t.Errorf("Expanded should contain avoid text: %q", tt.wantAvoid)
				}
			}
			if !strings.Contains(content.Expanded, tt.wantPrefer) {
				t.Errorf("Expanded should contain prefer text: %q", tt.wantPrefer)
			}
		})
	}
}

func TestBehaviorExtractor_BuildContent_Tags(t *testing.T) {
	extractor := NewBehaviorExtractor().(*behaviorExtractor)

	tests := []struct {
		name       string
		correction models.Correction
		wantTags   []string
	}{
		{
			name: "git-related correction gets git tag",
			correction: models.Correction{
				CorrectedAction: "Always use git -C for worktree operations",
			},
			wantTags: []string{"git", "worktree"},
		},
		{
			name: "testing correction gets testing tag",
			correction: models.Correction{
				CorrectedAction: "Follow TDD when writing Go tests",
			},
			wantTags: []string{"go", "tdd", "testing"},
		},
		{
			name: "no keywords produces nil tags",
			correction: models.Correction{
				CorrectedAction: "be more careful next time",
			},
			wantTags: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := extractor.buildContent(tt.correction)
			if len(content.Tags) != len(tt.wantTags) {
				t.Fatalf("Tags = %v, want %v", content.Tags, tt.wantTags)
			}
			for i, tag := range content.Tags {
				if tag != tt.wantTags[i] {
					t.Errorf("Tags[%d] = %q, want %q", i, tag, tt.wantTags[i])
				}
			}
		})
	}
}

func TestBehaviorExtractor_Extract_Provenance(t *testing.T) {
	extractor := NewBehaviorExtractor()

	correction := models.Correction{
		ID:              "test-correction-123",
		Timestamp:       time.Now(),
		AgentAction:     "did wrong thing",
		CorrectedAction: "do right thing",
	}

	behavior, err := extractor.Extract(correction)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	// Check provenance is properly set
	if behavior.Provenance.SourceType != models.SourceTypeLearned {
		t.Errorf("Provenance.SourceType = %v, want %v",
			behavior.Provenance.SourceType, models.SourceTypeLearned)
	}

	if behavior.Provenance.CorrectionID != correction.ID {
		t.Errorf("Provenance.CorrectionID = %v, want %v",
			behavior.Provenance.CorrectionID, correction.ID)
	}

	if behavior.Provenance.CreatedAt.IsZero() {
		t.Error("Provenance.CreatedAt should not be zero")
	}

}

func TestBehaviorExtractor_Extract_Stats(t *testing.T) {
	extractor := NewBehaviorExtractor()

	correction := models.Correction{
		ID:              "test-correction",
		AgentAction:     "action",
		CorrectedAction: "corrected",
	}

	beforeExtract := time.Now()
	behavior, err := extractor.Extract(correction)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	afterExtract := time.Now()

	// Check stats are initialized
	if behavior.Stats.CreatedAt.Before(beforeExtract) || behavior.Stats.CreatedAt.After(afterExtract) {
		t.Error("Stats.CreatedAt should be set to approximately now")
	}

	if behavior.Stats.UpdatedAt.Before(beforeExtract) || behavior.Stats.UpdatedAt.After(afterExtract) {
		t.Error("Stats.UpdatedAt should be set to approximately now")
	}

	if behavior.Stats.TimesActivated != 0 {
		t.Errorf("Stats.TimesActivated = %d, want 0", behavior.Stats.TimesActivated)
	}

	if behavior.Stats.TimesFollowed != 0 {
		t.Errorf("Stats.TimesFollowed = %d, want 0", behavior.Stats.TimesFollowed)
	}

	if behavior.Stats.TimesOverridden != 0 {
		t.Errorf("Stats.TimesOverridden = %d, want 0", behavior.Stats.TimesOverridden)
	}
}
