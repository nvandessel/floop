package learning

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/logging"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

func TestNewLearningLoop(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	loop := NewLearningLoop(s, nil)
	if loop == nil {
		t.Error("NewLearningLoop returned nil")
	}
}

func TestNewLearningLoop_WithConfig(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	cfg := &LearningLoopConfig{AutoAcceptThreshold: 0.5}
	loop := NewLearningLoop(s, cfg)
	if loop == nil {
		t.Error("NewLearningLoop returned nil")
	}
}

func TestLearningLoop_ProcessCorrection(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	loop := NewLearningLoop(s, nil)
	ctx := context.Background()

	correction := models.Correction{
		ID:              "test-correction-1",
		Timestamp:       time.Now(),
		AgentAction:     "used pip install",
		CorrectedAction: "use uv instead of pip for package management",
		Context: models.ContextSnapshot{
			Timestamp:    time.Now(),
			FileLanguage: "python",
			FilePath:     "requirements.txt",
		},
	}

	result, err := loop.ProcessCorrection(ctx, correction)
	if err != nil {
		t.Fatalf("ProcessCorrection failed: %v", err)
	}

	if result == nil {
		t.Fatal("ProcessCorrection returned nil result")
	}

	// Check the correction is preserved
	if result.Correction.ID != correction.ID {
		t.Errorf("expected correction ID %s, got %s", correction.ID, result.Correction.ID)
	}

	// Check a behavior was extracted
	if result.CandidateBehavior.ID == "" {
		t.Error("expected non-empty behavior ID")
	}

	// Check placement decision was made
	if result.Placement.Action == "" {
		t.Error("expected non-empty placement action")
	}
}

func TestLearningLoop_ProcessCorrection_ConstraintRequiresReview(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	loop := NewLearningLoop(s, nil)
	ctx := context.Background()

	// A correction that will be detected as a constraint
	correction := models.Correction{
		ID:              "test-correction-constraint",
		Timestamp:       time.Now(),
		AgentAction:     "committed directly to main",
		CorrectedAction: "never commit directly to main branch",
		Context: models.ContextSnapshot{
			Timestamp: time.Now(),
		},
	}

	result, err := loop.ProcessCorrection(ctx, correction)
	if err != nil {
		t.Fatalf("ProcessCorrection failed: %v", err)
	}

	// Constraints should require review
	if !result.RequiresReview {
		t.Error("expected constraint to require review")
	}

	// Should not be auto-accepted
	if result.AutoAccepted {
		t.Error("expected constraint to not be auto-accepted")
	}

	// Check that "Constraints require human review" is in reasons
	found := false
	for _, reason := range result.ReviewReasons {
		if reason == "Constraints require human review" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected constraint review reason, got: %v", result.ReviewReasons)
	}
}

func TestLearningLoop_ProcessCorrection_AutoAccept(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	// Lower threshold to ensure auto-accept
	cfg := &LearningLoopConfig{AutoAcceptThreshold: 0.5}
	loop := NewLearningLoop(s, cfg)
	ctx := context.Background()

	// A simple directive that should auto-accept
	correction := models.Correction{
		ID:              "test-correction-autoaccept",
		Timestamp:       time.Now(),
		AgentAction:     "used fmt.Println",
		CorrectedAction: "use log.Printf for logging",
		Context: models.ContextSnapshot{
			Timestamp:    time.Now(),
			FileLanguage: "go",
		},
	}

	result, err := loop.ProcessCorrection(ctx, correction)
	if err != nil {
		t.Fatalf("ProcessCorrection failed: %v", err)
	}

	// Should be auto-accepted (high confidence placement, not a constraint)
	if !result.AutoAccepted {
		t.Errorf("expected auto-accept, got RequiresReview=%v, reasons=%v",
			result.RequiresReview, result.ReviewReasons)
	}

	// Verify it was stored
	node, err := s.GetNode(ctx, result.CandidateBehavior.ID)
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}
	if node == nil {
		t.Error("expected behavior to be stored after auto-accept")
	}
}

func TestLearningLoop_NeedsReview_LowConfidence(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	loop := NewLearningLoop(s, nil).(*learningLoop)

	candidate := &models.Behavior{
		ID:   "test-behavior",
		Kind: models.BehaviorKindDirective,
	}
	placement := &PlacementDecision{
		Action:     "create",
		Confidence: 0.4, // Low confidence
	}

	needsReview, reasons := loop.needsReview(candidate, placement)
	if !needsReview {
		t.Error("expected low confidence to require review")
	}

	found := false
	for _, r := range reasons {
		if r == "Low placement confidence: 0.40" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected low confidence reason, got: %v", reasons)
	}
}

func TestLearningLoop_NeedsReview_HighSimilarity(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	loop := NewLearningLoop(s, nil).(*learningLoop)

	candidate := &models.Behavior{
		ID:   "test-behavior",
		Kind: models.BehaviorKindDirective,
	}
	placement := &PlacementDecision{
		Action:     "create",
		Confidence: 0.9,
		SimilarBehaviors: []SimilarityMatch{
			{ID: "existing-1", Score: 0.90},
		},
	}

	needsReview, reasons := loop.needsReview(candidate, placement)
	if !needsReview {
		t.Error("expected high similarity to require review")
	}

	found := false
	for _, r := range reasons {
		if r == "Very similar to existing: existing-1 (0.90)" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected high similarity reason, got: %v", reasons)
	}
}

func TestLearningLoop_NeedsReview_MergeAction(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	loop := NewLearningLoop(s, nil).(*learningLoop)

	candidate := &models.Behavior{
		ID:   "test-behavior",
		Kind: models.BehaviorKindDirective,
	}
	placement := &PlacementDecision{
		Action:     "merge",
		TargetID:   "existing-behavior",
		Confidence: 0.9,
	}

	needsReview, reasons := loop.needsReview(candidate, placement)
	if !needsReview {
		t.Error("expected merge action to require review")
	}

	found := false
	for _, r := range reasons {
		if r == "Would merge into existing behavior: existing-behavior" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected merge reason, got: %v", reasons)
	}
}

func TestLearningLoop_ProcessCorrection_LogsAutoAccept(t *testing.T) {
	dir := t.TempDir()
	dl := logging.NewDecisionLogger(dir, "debug")
	defer dl.Close()

	s := store.NewInMemoryGraphStore()
	cfg := &LearningLoopConfig{
		AutoAcceptThreshold: 0.5,
		Logger:              logging.NewLogger("debug", os.Stderr),
		DecisionLogger:      dl,
	}
	loop := NewLearningLoop(s, cfg)
	ctx := context.Background()

	correction := models.Correction{
		ID:              "test-correction-log",
		Timestamp:       time.Now(),
		AgentAction:     "used fmt.Println",
		CorrectedAction: "use log.Printf for logging",
		Context: models.ContextSnapshot{
			Timestamp:    time.Now(),
			FileLanguage: "go",
		},
	}

	result, err := loop.ProcessCorrection(ctx, correction)
	if err != nil {
		t.Fatalf("ProcessCorrection failed: %v", err)
	}

	if !result.AutoAccepted {
		t.Fatalf("expected auto-accept with threshold 0.5, got RequiresReview=%v, reasons=%v",
			result.RequiresReview, result.ReviewReasons)
	}

	// Read decisions.jsonl and verify auto_accept event
	data, err := os.ReadFile(filepath.Join(dir, "decisions.jsonl"))
	if err != nil {
		t.Fatalf("failed to read decisions.jsonl: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	found := false
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event["event"] == "auto_accept" {
			found = true
			if event["accepted"] != true {
				t.Errorf("expected accepted=true, got %v", event["accepted"])
			}
			if _, ok := event["confidence"]; !ok {
				t.Error("expected confidence field")
			}
			if _, ok := event["threshold"]; !ok {
				t.Error("expected threshold field")
			}
		}
	}
	if !found {
		t.Errorf("expected auto_accept event, got:\n%s", string(data))
	}
}

func TestLearningLoop_ProcessCorrection_LogsReviewRequired(t *testing.T) {
	dir := t.TempDir()
	dl := logging.NewDecisionLogger(dir, "debug")
	defer dl.Close()

	s := store.NewInMemoryGraphStore()
	cfg := &LearningLoopConfig{
		Logger:         logging.NewLogger("debug", os.Stderr),
		DecisionLogger: dl,
	}
	loop := NewLearningLoop(s, cfg)
	ctx := context.Background()

	// Constraints always require review
	correction := models.Correction{
		ID:              "test-correction-review",
		Timestamp:       time.Now(),
		AgentAction:     "committed directly to main",
		CorrectedAction: "never commit directly to main branch",
		Context: models.ContextSnapshot{
			Timestamp: time.Now(),
		},
	}

	result, err := loop.ProcessCorrection(ctx, correction)
	if err != nil {
		t.Fatalf("ProcessCorrection failed: %v", err)
	}

	if !result.RequiresReview {
		t.Fatal("expected constraint correction to require review")
	}

	data, err := os.ReadFile(filepath.Join(dir, "decisions.jsonl"))
	if err != nil {
		t.Fatalf("failed to read decisions.jsonl: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	found := false
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event["event"] == "review_required" {
			found = true
			if _, ok := event["reasons"]; !ok {
				t.Error("expected reasons field")
			}
			if _, ok := event["behavior_id"]; !ok {
				t.Error("expected behavior_id field")
			}
		}
	}
	if !found {
		t.Errorf("expected review_required event, got:\n%s", string(data))
	}
}

func TestLearningLoop_NilDecisionLogger_DoesNotPanic(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	// No logger or decision logger
	loop := NewLearningLoop(s, nil)
	ctx := context.Background()

	correction := models.Correction{
		ID:              "test-nil-logger",
		Timestamp:       time.Now(),
		AgentAction:     "used pip install",
		CorrectedAction: "use uv instead of pip",
		Context: models.ContextSnapshot{
			Timestamp:    time.Now(),
			FileLanguage: "python",
		},
	}

	// Should not panic with nil loggers
	_, err := loop.ProcessCorrection(ctx, correction)
	if err != nil {
		t.Fatalf("ProcessCorrection failed: %v", err)
	}
}
