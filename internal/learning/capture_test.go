package learning

import (
	"strings"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/models"
)

func TestNewCorrectionCapture(t *testing.T) {
	cc := NewCorrectionCapture()
	if cc == nil {
		t.Fatal("NewCorrectionCapture returned nil")
	}
}

func TestCorrectionCapture_MightBeCorrection(t *testing.T) {
	cc := NewCorrectionCapture()

	tests := []struct {
		name string
		text string
		want bool
	}{
		// Positive cases - should detect corrections
		{"starts with no", "No, that's not what I meant", true},
		{"contains don't", "Please don't do that", true},
		{"contains instead", "Use this instead of that", true},
		{"contains actually", "Actually, the correct way is...", true},
		{"contains not like that", "Not like that, do it this way", true},
		{"contains that's wrong", "That's wrong, here's the fix", true},
		{"contains that's not right", "That's not right", true},
		{"contains shouldn't", "You shouldn't use that approach", true},
		{"contains prefer", "I prefer using interfaces", true},
		{"contains better to", "It's better to use constants", true},
		{"contains rather than", "Use Go rather than Python", true},
		{"contains use this instead", "Use this instead", true},
		{"contains that's incorrect", "That's incorrect", true},
		{"contains please use", "Please use the new API", true},
		{"contains you should", "You should follow the guidelines", true},

		// Case insensitivity
		{"uppercase NO", "NO, stop doing that", true},
		{"mixed case Actually", "ACTUALLY, here's the thing", true},

		// New patterns that should be detected
		{"contains stop", "Stop doing that", true},
		{"contains is wrong", "That is wrong", true},
		{"contains went wrong", "Something went wrong with your approach", true},
		{"contains never", "Never do that again", true},
		{"contains always", "Always use interfaces here", true},
		{"contains fix this", "Fix this, it's broken", true},
		{"contains broke", "That broke the build", true},
		{"contains remember", "Remember to run tests first", true},
		{"contains make sure", "Make sure to validate inputs", true},
		{"contains i told you", "I told you to use snake_case", true},
		{"contains i said", "I said not to commit to main", true},
		{"contains why did you", "Why did you delete that file?", true},
		{"contains that's not", "That's not how it works", true},
		{"contains do not", "Do not use global state", true},
		{"contains quit", "Quit adding unnecessary abstractions", true},
		{"contains not what i", "That's not what I asked for", true},
		// Pre-filter accepts some false positives — LLM extraction handles precision
		{"question about stopping matches", "How do I stop the server?", true},

		// Negative cases - should not detect corrections
		{"neutral statement", "The weather is nice today", false},
		{"question", "How do I use this function?", false},
		{"code snippet", "func main() { fmt.Println(x) }", false},
		{"empty string", "", false},
		{"partial match inside word", "nothing special here", false},
		{"partial match donate", "donate to charity", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cc.MightBeCorrection(tt.text)
			if got != tt.want {
				t.Errorf("MightBeCorrection(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestCorrectionCapture_CaptureFromCLI(t *testing.T) {
	cc := NewCorrectionCapture()

	tests := []struct {
		name     string
		wrong    string
		right    string
		ctx      models.ContextSnapshot
		wantErr  bool
		validate func(t *testing.T, c *models.Correction)
	}{
		{
			name:  "basic correction",
			wrong: "used pip install",
			right: "use uv instead of pip",
			ctx: models.ContextSnapshot{
				Timestamp: time.Now(),
				Repo:      "my-project",
				FilePath:  "requirements.txt",
				User:      "developer",
			},
			wantErr: false,
			validate: func(t *testing.T, c *models.Correction) {
				if c.AgentAction != "used pip install" {
					t.Errorf("AgentAction = %q, want %q", c.AgentAction, "used pip install")
				}
				if c.CorrectedAction != "use uv instead of pip" {
					t.Errorf("CorrectedAction = %q, want %q", c.CorrectedAction, "use uv instead of pip")
				}
				if c.Corrector != "developer" {
					t.Errorf("Corrector = %q, want %q", c.Corrector, "developer")
				}
				if c.Processed {
					t.Error("Processed should be false for new corrections")
				}
			},
		},
		{
			name:    "empty context",
			wrong:   "did X",
			right:   "do Y",
			ctx:     models.ContextSnapshot{},
			wantErr: false,
			validate: func(t *testing.T, c *models.Correction) {
				if c.Corrector != "" {
					t.Errorf("Corrector should be empty, got %q", c.Corrector)
				}
			},
		},
		{
			name:  "preserves context fields",
			wrong: "wrong action",
			right: "correct action",
			ctx: models.ContextSnapshot{
				Repo:         "test-repo",
				Branch:       "main",
				FilePath:     "/path/to/file.go",
				FileLanguage: "go",
				Task:         "implementing tests",
				User:         "testuser",
				Environment:  "dev",
			},
			wantErr: false,
			validate: func(t *testing.T, c *models.Correction) {
				if c.Context.Repo != "test-repo" {
					t.Errorf("Context.Repo = %q, want %q", c.Context.Repo, "test-repo")
				}
				if c.Context.Branch != "main" {
					t.Errorf("Context.Branch = %q, want %q", c.Context.Branch, "main")
				}
				if c.Context.FilePath != "/path/to/file.go" {
					t.Errorf("Context.FilePath = %q, want %q", c.Context.FilePath, "/path/to/file.go")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cc.CaptureFromCLI(tt.wrong, tt.right, tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("CaptureFromCLI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got == nil {
				t.Fatal("CaptureFromCLI() returned nil without error")
			}
			if tt.validate != nil && got != nil {
				tt.validate(t, got)
			}
		})
	}
}

func TestCorrectionCapture_IDGeneration(t *testing.T) {
	cc := NewCorrectionCapture()

	t.Run("ID has correct prefix", func(t *testing.T) {
		c, err := cc.CaptureFromCLI("wrong", "right", models.ContextSnapshot{})
		if err != nil {
			t.Fatalf("CaptureFromCLI() error = %v", err)
		}
		if !strings.HasPrefix(c.ID, "correction-") {
			t.Errorf("ID = %q, want prefix 'correction-'", c.ID)
		}
	})

	t.Run("same inputs produce same ID", func(t *testing.T) {
		c1, _ := cc.CaptureFromCLI("wrong", "right", models.ContextSnapshot{})
		c2, _ := cc.CaptureFromCLI("wrong", "right", models.ContextSnapshot{})
		if c1.ID != c2.ID {
			t.Errorf("Same inputs should produce same ID: %q != %q", c1.ID, c2.ID)
		}
	})

	t.Run("different inputs produce different IDs", func(t *testing.T) {
		c1, _ := cc.CaptureFromCLI("wrong1", "right1", models.ContextSnapshot{})
		c2, _ := cc.CaptureFromCLI("wrong2", "right2", models.ContextSnapshot{})
		if c1.ID == c2.ID {
			t.Errorf("Different inputs should produce different IDs: both %q", c1.ID)
		}
	})

	t.Run("ID is deterministic regardless of context", func(t *testing.T) {
		ctx1 := models.ContextSnapshot{User: "user1", Repo: "repo1"}
		ctx2 := models.ContextSnapshot{User: "user2", Repo: "repo2"}
		c1, _ := cc.CaptureFromCLI("wrong", "right", ctx1)
		c2, _ := cc.CaptureFromCLI("wrong", "right", ctx2)
		if c1.ID != c2.ID {
			t.Errorf("ID should only depend on wrong/right, not context: %q != %q", c1.ID, c2.ID)
		}
	})

	t.Run("long inputs are truncated for ID", func(t *testing.T) {
		longWrong := strings.Repeat("a", 200)
		longRight := strings.Repeat("b", 200)
		c, err := cc.CaptureFromCLI(longWrong, longRight, models.ContextSnapshot{})
		if err != nil {
			t.Fatalf("CaptureFromCLI() error = %v", err)
		}
		// ID should still be generated without panic
		if !strings.HasPrefix(c.ID, "correction-") {
			t.Errorf("ID = %q, want prefix 'correction-'", c.ID)
		}
		// ID length should be consistent (prefix + 12 hex chars)
		expectedLen := len("correction-") + 12
		if len(c.ID) != expectedLen {
			t.Errorf("ID length = %d, want %d", len(c.ID), expectedLen)
		}
	})
}

func TestCorrectionCapture_Timestamp(t *testing.T) {
	cc := NewCorrectionCapture()

	before := time.Now()
	c, err := cc.CaptureFromCLI("wrong", "right", models.ContextSnapshot{})
	after := time.Now()

	if err != nil {
		t.Fatalf("CaptureFromCLI() error = %v", err)
	}

	if c.Timestamp.Before(before) || c.Timestamp.After(after) {
		t.Errorf("Timestamp %v should be between %v and %v", c.Timestamp, before, after)
	}
}
