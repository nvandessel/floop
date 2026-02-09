package ranking

import (
	"sort"

	"github.com/nvandessel/feedback-loop/internal/models"
)

// HybridScorerConfig holds weights for the three scoring signals.
type HybridScorerConfig struct {
	// ContextWeight (lambda1) for context relevance signal. Default: 0.5.
	ContextWeight float64

	// ActivationWeight (lambda2) for spreading activation signal. Default: 0.3.
	ActivationWeight float64

	// PageRankWeight (lambda3) for graph structural importance. Default: 0.2.
	PageRankWeight float64
}

// DefaultHybridScorerConfig returns the default hybrid scoring weights.
func DefaultHybridScorerConfig() HybridScorerConfig {
	return HybridScorerConfig{
		ContextWeight:    0.5,
		ActivationWeight: 0.3,
		PageRankWeight:   0.2,
	}
}

// HybridScorer combines context relevance, spreading activation, and PageRank.
type HybridScorer struct {
	config         HybridScorerConfig
	contextScorer  *RelevanceScorer
	pageRankScores map[string]float64
}

// NewHybridScorer creates a new hybrid scorer.
// pageRankScores should be pre-computed via ComputePageRank.
func NewHybridScorer(config HybridScorerConfig, contextScorer *RelevanceScorer, pageRankScores map[string]float64) *HybridScorer {
	if pageRankScores == nil {
		pageRankScores = make(map[string]float64)
	}
	return &HybridScorer{
		config:         config,
		contextScorer:  contextScorer,
		pageRankScores: pageRankScores,
	}
}

// HybridScore represents the combined score for a behavior.
type HybridScore struct {
	BehaviorID      string  `json:"behavior_id"`
	FinalScore      float64 `json:"final_score"`
	ContextScore    float64 `json:"context_score"`
	ActivationScore float64 `json:"activation_score"`
	PageRankScore   float64 `json:"pagerank_score"`
}

// Score computes the hybrid score for a behavior.
// activationLevel is the spreading activation value for this behavior (0.0-1.0).
func (h *HybridScorer) Score(
	behavior *models.Behavior,
	ctx *models.ContextSnapshot,
	activationLevel float64,
) HybridScore {
	if behavior == nil {
		return HybridScore{}
	}

	var contextScore float64
	if h.contextScorer != nil {
		contextScore = h.contextScorer.Score(behavior, ctx).Score
	}
	pageRank := h.pageRankScores[behavior.ID] // 0 if not found

	finalScore := h.config.ContextWeight*contextScore +
		h.config.ActivationWeight*activationLevel +
		h.config.PageRankWeight*pageRank

	return HybridScore{
		BehaviorID:      behavior.ID,
		FinalScore:      finalScore,
		ContextScore:    contextScore,
		ActivationScore: activationLevel,
		PageRankScore:   pageRank,
	}
}

// ScoreBatch scores multiple behaviors and returns results sorted by FinalScore descending.
func (h *HybridScorer) ScoreBatch(
	behaviors []models.Behavior,
	ctx *models.ContextSnapshot,
	activations map[string]float64,
) []HybridScore {
	if activations == nil {
		activations = make(map[string]float64)
	}

	results := make([]HybridScore, len(behaviors))
	for i := range behaviors {
		results[i] = h.Score(&behaviors[i], ctx, activations[behaviors[i].ID])
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].FinalScore > results[j].FinalScore
	})

	return results
}
