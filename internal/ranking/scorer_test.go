package ranking

import (
	"math"
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
)

func TestNewRelevanceScorer(t *testing.T) {
	config := DefaultScorerConfig()
	scorer := NewRelevanceScorer(config)

	if scorer == nil {
		t.Fatal("expected scorer to be non-nil")
	}

	// Check weights are normalized
	totalWeight := scorer.config.ContextWeight + scorer.config.UsageWeight +
		scorer.config.RecencyWeight + scorer.config.ConfidenceWeight +
		scorer.config.PriorityWeight

	if math.Abs(totalWeight-1.0) > 0.001 {
		t.Errorf("weights should sum to 1.0, got %f", totalWeight)
	}
}

func TestRelevanceScorer_Score_NilBehavior(t *testing.T) {
	scorer := NewRelevanceScorer(DefaultScorerConfig())
	result := scorer.Score(nil, nil)

	if result.Score != 0 {
		t.Errorf("nil behavior should have score 0, got %f", result.Score)
	}
}

func TestRelevanceScorer_Score_Basic(t *testing.T) {
	scorer := NewRelevanceScorer(DefaultScorerConfig())
	now := time.Now()

	behavior := &models.Behavior{
		ID:         "test",
		Kind:       models.BehaviorKindDirective,
		Confidence: 0.8,
		Priority:   5,
		Stats: models.BehaviorStats{
			TimesActivated: 10,
			TimesFollowed:  8,
			CreatedAt:      now.Add(-24 * time.Hour),
			UpdatedAt:      now.Add(-1 * time.Hour),
		},
	}

	result := scorer.Score(behavior, nil)

	if result.Score <= 0 {
		t.Error("expected positive score")
	}
	if result.Behavior != behavior {
		t.Error("expected behavior reference to be preserved")
	}
}

func TestRelevanceScorer_Score_ConstraintBoost(t *testing.T) {
	scorer := NewRelevanceScorer(DefaultScorerConfig())
	now := time.Now()

	constraint := &models.Behavior{
		ID:         "constraint",
		Kind:       models.BehaviorKindConstraint,
		Confidence: 0.8,
		Priority:   5,
		Stats: models.BehaviorStats{
			TimesActivated: 10,
			TimesFollowed:  8,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}

	directive := &models.Behavior{
		ID:         "directive",
		Kind:       models.BehaviorKindDirective,
		Confidence: 0.8,
		Priority:   5,
		Stats: models.BehaviorStats{
			TimesActivated: 10,
			TimesFollowed:  8,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}

	constraintScore := scorer.Score(constraint, nil)
	directiveScore := scorer.Score(directive, nil)

	// Constraints should score higher due to kind boost
	if constraintScore.Score <= directiveScore.Score {
		t.Errorf("constraint score (%f) should be > directive score (%f)",
			constraintScore.Score, directiveScore.Score)
	}

	// Verify kind boosts are applied correctly
	if constraintScore.KindBoost != 2.0 {
		t.Errorf("constraint kind boost should be 2.0, got %f", constraintScore.KindBoost)
	}
	if directiveScore.KindBoost != 1.5 {
		t.Errorf("directive kind boost should be 1.5, got %f", directiveScore.KindBoost)
	}
}

func TestRelevanceScorer_Score_UsageSignals(t *testing.T) {
	scorer := NewRelevanceScorer(DefaultScorerConfig())
	now := time.Now()

	wellUsed := &models.Behavior{
		ID:         "well-used",
		Kind:       models.BehaviorKindDirective,
		Confidence: 0.8,
		Priority:   5,
		Stats: models.BehaviorStats{
			TimesActivated: 100,
			TimesFollowed:  90,
			TimesConfirmed: 5,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}

	poorlyUsed := &models.Behavior{
		ID:         "poorly-used",
		Kind:       models.BehaviorKindDirective,
		Confidence: 0.8,
		Priority:   5,
		Stats: models.BehaviorStats{
			TimesActivated:  100,
			TimesFollowed:   10,
			TimesOverridden: 80,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
	}

	wellUsedScore := scorer.Score(wellUsed, nil)
	poorlyUsedScore := scorer.Score(poorlyUsed, nil)

	if wellUsedScore.UsageScore <= poorlyUsedScore.UsageScore {
		t.Errorf("well-used score (%f) should be > poorly-used score (%f)",
			wellUsedScore.UsageScore, poorlyUsedScore.UsageScore)
	}
}

func TestRelevanceScorer_Score_Recency(t *testing.T) {
	scorer := NewRelevanceScorer(DefaultScorerConfig())
	now := time.Now()

	recent := &models.Behavior{
		ID:         "recent",
		Kind:       models.BehaviorKindDirective,
		Confidence: 0.8,
		Priority:   5,
		Stats: models.BehaviorStats{
			CreatedAt: now.Add(-1 * time.Hour),
			UpdatedAt: now.Add(-1 * time.Hour),
		},
	}

	old := &models.Behavior{
		ID:         "old",
		Kind:       models.BehaviorKindDirective,
		Confidence: 0.8,
		Priority:   5,
		Stats: models.BehaviorStats{
			CreatedAt: now.Add(-30 * 24 * time.Hour),
			UpdatedAt: now.Add(-30 * 24 * time.Hour),
		},
	}

	recentScore := scorer.Score(recent, nil)
	oldScore := scorer.Score(old, nil)

	if recentScore.RecencyScore <= oldScore.RecencyScore {
		t.Errorf("recent score (%f) should be > old score (%f)",
			recentScore.RecencyScore, oldScore.RecencyScore)
	}
}

func TestRelevanceScorer_Score_ContextSpecificity(t *testing.T) {
	scorer := NewRelevanceScorer(DefaultScorerConfig())
	now := time.Now()

	ctx := &models.ContextSnapshot{
		FilePath:     "main.go",
		FileLanguage: "go",
		Task:         "development",
	}

	global := &models.Behavior{
		ID:         "global",
		Kind:       models.BehaviorKindDirective,
		Confidence: 0.8,
		Priority:   5,
		When:       nil, // No predicate = global
		Stats: models.BehaviorStats{
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	specific := &models.Behavior{
		ID:         "specific",
		Kind:       models.BehaviorKindDirective,
		Confidence: 0.8,
		Priority:   5,
		When: map[string]interface{}{
			"language": "go",
			"task":     "development",
		},
		Stats: models.BehaviorStats{
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	globalScore := scorer.Score(global, ctx)
	specificScore := scorer.Score(specific, ctx)

	if specificScore.ContextScore <= globalScore.ContextScore {
		t.Errorf("specific context score (%f) should be > global context score (%f)",
			specificScore.ContextScore, globalScore.ContextScore)
	}
}

func TestRelevanceScorer_ScoreBatch(t *testing.T) {
	scorer := NewRelevanceScorer(DefaultScorerConfig())
	now := time.Now()

	behaviors := []models.Behavior{
		{ID: "b1", Kind: models.BehaviorKindDirective, Confidence: 0.8, Stats: models.BehaviorStats{CreatedAt: now, UpdatedAt: now}},
		{ID: "b2", Kind: models.BehaviorKindConstraint, Confidence: 0.9, Stats: models.BehaviorStats{CreatedAt: now, UpdatedAt: now}},
		{ID: "b3", Kind: models.BehaviorKindPreference, Confidence: 0.7, Stats: models.BehaviorStats{CreatedAt: now, UpdatedAt: now}},
	}

	results := scorer.ScoreBatch(behaviors, nil)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for i, result := range results {
		if result.Behavior == nil {
			t.Errorf("result %d has nil behavior", i)
		}
		if result.Score <= 0 {
			t.Errorf("result %d has non-positive score: %f", i, result.Score)
		}
	}
}

func TestExponentialDecay(t *testing.T) {
	halfLife := 7 * 24 * time.Hour

	tests := []struct {
		name    string
		time    time.Time
		wantMin float64
		wantMax float64
	}{
		{
			name:    "zero time",
			time:    time.Time{},
			wantMin: 0,
			wantMax: 0.001,
		},
		{
			name:    "now",
			time:    time.Now(),
			wantMin: 0.99,
			wantMax: 1.0,
		},
		{
			name:    "one half-life ago",
			time:    time.Now().Add(-halfLife),
			wantMin: 0.45,
			wantMax: 0.55,
		},
		{
			name:    "two half-lives ago",
			time:    time.Now().Add(-2 * halfLife),
			wantMin: 0.20,
			wantMax: 0.30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ExponentialDecay(tt.time, halfLife)
			if score < tt.wantMin || score > tt.wantMax {
				t.Errorf("ExponentialDecay() = %f, want in [%f, %f]", score, tt.wantMin, tt.wantMax)
			}
		})
	}
}
