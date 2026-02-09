package ranking

import (
	"math"
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
)

func TestHybridScorer_WeightedCombination(t *testing.T) {
	tests := []struct {
		name            string
		contextScore    float64
		activationScore float64
		pageRankScore   float64
		wantScore       float64
	}{
		{"all max", 1.0, 1.0, 1.0, 1.0},
		{"all zero", 0.0, 0.0, 0.0, 0.0},
		{"context only", 1.0, 0.0, 0.0, 0.5},
		{"activation only", 0.0, 1.0, 0.0, 0.3},
		{"pagerank only", 0.0, 0.0, 1.0, 0.2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a context scorer that returns a predictable score.
			// We use a behavior whose scorer.Score().Score matches tt.contextScore.
			// To control the context score precisely, we create a scorer with
			// specific weights and a behavior tailored to produce the desired score.
			//
			// For simplicity, we construct the HybridScorer directly and verify
			// the formula using a known contextScore from the RelevanceScorer.
			pageRankScores := map[string]float64{
				"test-behavior": tt.pageRankScore,
			}

			contextScorer := NewRelevanceScorer(DefaultScorerConfig())
			scorer := NewHybridScorer(DefaultHybridScorerConfig(), contextScorer, pageRankScores)

			// Create a behavior that will produce a known context score.
			// We need to compute what the RelevanceScorer actually returns
			// and then verify the hybrid formula applies correctly.
			now := time.Now()
			behavior := &models.Behavior{
				ID:         "test-behavior",
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

			result := scorer.Score(behavior, nil, tt.activationScore)

			// Verify that the formula is correctly applied.
			actualContextScore := contextScorer.Score(behavior, nil).Score
			expectedFinal := DefaultHybridScorerConfig().ContextWeight*actualContextScore +
				DefaultHybridScorerConfig().ActivationWeight*tt.activationScore +
				DefaultHybridScorerConfig().PageRankWeight*tt.pageRankScore

			if math.Abs(result.FinalScore-expectedFinal) > 0.001 {
				t.Errorf("FinalScore = %f, want %f (formula: 0.5*%f + 0.3*%f + 0.2*%f)",
					result.FinalScore, expectedFinal,
					actualContextScore, tt.activationScore, tt.pageRankScore)
			}

			// Verify component scores are stored correctly.
			if math.Abs(result.ActivationScore-tt.activationScore) > 0.001 {
				t.Errorf("ActivationScore = %f, want %f", result.ActivationScore, tt.activationScore)
			}
			if math.Abs(result.PageRankScore-tt.pageRankScore) > 0.001 {
				t.Errorf("PageRankScore = %f, want %f", result.PageRankScore, tt.pageRankScore)
			}
		})
	}
}

func TestHybridScorer_FormulaVerification(t *testing.T) {
	// This test directly verifies the weighted combination formula
	// with controlled inputs by checking exact arithmetic.
	tests := []struct {
		name       string
		config     HybridScorerConfig
		activation float64
		pageRank   float64
	}{
		{
			name:       "default weights",
			config:     DefaultHybridScorerConfig(),
			activation: 0.7,
			pageRank:   0.4,
		},
		{
			name: "custom weights",
			config: HybridScorerConfig{
				ContextWeight:    0.3,
				ActivationWeight: 0.5,
				PageRankWeight:   0.2,
			},
			activation: 0.9,
			pageRank:   0.6,
		},
		{
			name: "zero activation weight",
			config: HybridScorerConfig{
				ContextWeight:    0.8,
				ActivationWeight: 0.0,
				PageRankWeight:   0.2,
			},
			activation: 1.0,
			pageRank:   0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contextScorer := NewRelevanceScorer(DefaultScorerConfig())
			pageRankScores := map[string]float64{"b1": tt.pageRank}
			scorer := NewHybridScorer(tt.config, contextScorer, pageRankScores)

			now := time.Now()
			behavior := &models.Behavior{
				ID:         "b1",
				Kind:       models.BehaviorKindPreference,
				Confidence: 0.5,
				Priority:   3,
				Stats: models.BehaviorStats{
					CreatedAt: now,
					UpdatedAt: now,
				},
			}

			result := scorer.Score(behavior, nil, tt.activation)
			ctxScore := contextScorer.Score(behavior, nil).Score

			expected := tt.config.ContextWeight*ctxScore +
				tt.config.ActivationWeight*tt.activation +
				tt.config.PageRankWeight*tt.pageRank

			if math.Abs(result.FinalScore-expected) > 0.0001 {
				t.Errorf("FinalScore = %f, want %f", result.FinalScore, expected)
			}
		})
	}
}

func TestHybridScorer_NilBehavior(t *testing.T) {
	contextScorer := NewRelevanceScorer(DefaultScorerConfig())
	scorer := NewHybridScorer(DefaultHybridScorerConfig(), contextScorer, nil)

	result := scorer.Score(nil, nil, 0.5)
	if result.FinalScore != 0 {
		t.Errorf("nil behavior should have FinalScore 0, got %f", result.FinalScore)
	}
	if result.BehaviorID != "" {
		t.Errorf("nil behavior should have empty BehaviorID, got %q", result.BehaviorID)
	}
}

func TestHybridScorer_MissingPageRank(t *testing.T) {
	contextScorer := NewRelevanceScorer(DefaultScorerConfig())

	// PageRank map does not include our behavior.
	pageRankScores := map[string]float64{
		"other-behavior": 0.9,
	}
	scorer := NewHybridScorer(DefaultHybridScorerConfig(), contextScorer, pageRankScores)

	now := time.Now()
	behavior := &models.Behavior{
		ID:         "missing-pr",
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

	result := scorer.Score(behavior, nil, 0.6)

	// PageRank should default to 0.
	if result.PageRankScore != 0 {
		t.Errorf("missing PageRank should default to 0, got %f", result.PageRankScore)
	}

	// Score should still be computed from context + activation.
	if result.FinalScore <= 0 {
		t.Errorf("FinalScore should be positive even without PageRank, got %f", result.FinalScore)
	}

	// Verify the formula holds with pageRank=0.
	ctxScore := contextScorer.Score(behavior, nil).Score
	expected := 0.5*ctxScore + 0.3*0.6 + 0.2*0.0
	if math.Abs(result.FinalScore-expected) > 0.001 {
		t.Errorf("FinalScore = %f, want %f", result.FinalScore, expected)
	}
}

func TestHybridScorer_ScoreBatch_Sorted(t *testing.T) {
	contextScorer := NewRelevanceScorer(DefaultScorerConfig())
	pageRankScores := map[string]float64{
		"high": 0.9,
		"mid":  0.5,
		"low":  0.1,
	}
	scorer := NewHybridScorer(DefaultHybridScorerConfig(), contextScorer, pageRankScores)

	now := time.Now()
	behaviors := []models.Behavior{
		{
			ID: "low", Kind: models.BehaviorKindPreference, Confidence: 0.5, Priority: 3,
			Stats: models.BehaviorStats{CreatedAt: now, UpdatedAt: now},
		},
		{
			ID: "high", Kind: models.BehaviorKindConstraint, Confidence: 0.9, Priority: 8,
			Stats: models.BehaviorStats{
				TimesActivated: 50, TimesFollowed: 45,
				CreatedAt: now, UpdatedAt: now,
			},
		},
		{
			ID: "mid", Kind: models.BehaviorKindDirective, Confidence: 0.7, Priority: 5,
			Stats: models.BehaviorStats{
				TimesActivated: 20, TimesFollowed: 15,
				CreatedAt: now, UpdatedAt: now,
			},
		},
	}

	activations := map[string]float64{
		"high": 0.9,
		"mid":  0.5,
		"low":  0.1,
	}

	results := scorer.ScoreBatch(behaviors, nil, activations)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Verify sorted descending by FinalScore.
	for i := 1; i < len(results); i++ {
		if results[i].FinalScore > results[i-1].FinalScore {
			t.Errorf("results not sorted descending: index %d (%f) > index %d (%f)",
				i, results[i].FinalScore, i-1, results[i-1].FinalScore)
		}
	}

	// The "high" behavior should be first (highest PageRank, activation, and context score).
	if results[0].BehaviorID != "high" {
		t.Errorf("expected 'high' to be first, got %q", results[0].BehaviorID)
	}
}

func TestHybridScorer_ScoreBatch_NilActivations(t *testing.T) {
	contextScorer := NewRelevanceScorer(DefaultScorerConfig())
	scorer := NewHybridScorer(DefaultHybridScorerConfig(), contextScorer, nil)

	now := time.Now()
	behaviors := []models.Behavior{
		{
			ID: "b1", Kind: models.BehaviorKindDirective, Confidence: 0.8, Priority: 5,
			Stats: models.BehaviorStats{CreatedAt: now, UpdatedAt: now},
		},
	}

	// Passing nil activations should not panic.
	results := scorer.ScoreBatch(behaviors, nil, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Activation and PageRank should both be 0; only context contributes.
	if results[0].ActivationScore != 0 {
		t.Errorf("ActivationScore should be 0 with nil activations, got %f", results[0].ActivationScore)
	}
	if results[0].PageRankScore != 0 {
		t.Errorf("PageRankScore should be 0 with nil pageRankScores, got %f", results[0].PageRankScore)
	}
}

func TestHybridScorer_NilContextScorer(t *testing.T) {
	scorer := NewHybridScorer(DefaultHybridScorerConfig(), nil, nil)
	// Should not panic, should return zero context score
	result := scorer.Score(&models.Behavior{ID: "test"}, nil, 0.5)
	if result.ContextScore != 0 {
		t.Errorf("expected 0 context score with nil scorer, got %f", result.ContextScore)
	}
}

func TestDefaultHybridScorerConfig(t *testing.T) {
	config := DefaultHybridScorerConfig()

	// Weights should sum to 1.0.
	total := config.ContextWeight + config.ActivationWeight + config.PageRankWeight
	if math.Abs(total-1.0) > 0.001 {
		t.Errorf("weights should sum to 1.0, got %f", total)
	}

	// Verify default values.
	if config.ContextWeight != 0.5 {
		t.Errorf("ContextWeight = %f, want 0.5", config.ContextWeight)
	}
	if config.ActivationWeight != 0.3 {
		t.Errorf("ActivationWeight = %f, want 0.3", config.ActivationWeight)
	}
	if config.PageRankWeight != 0.2 {
		t.Errorf("PageRankWeight = %f, want 0.2", config.PageRankWeight)
	}
}
