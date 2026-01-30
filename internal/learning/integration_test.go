package learning

import (
	"context"
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/dedup"
	"github.com/nvandessel/feedback-loop/internal/llm"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// mockDeduplicator implements dedup.Deduplicator for testing purposes.
// It allows configuring duplicate detection results and merge behavior.
type mockDeduplicator struct {
	duplicates     []dedup.DuplicateMatch
	mergeResult    *models.Behavior
	findErr        error
	mergeErr       error
	findCallCount  int
	mergeCallCount int
}

func newMockDeduplicator() *mockDeduplicator {
	return &mockDeduplicator{
		duplicates: make([]dedup.DuplicateMatch, 0),
	}
}

func (m *mockDeduplicator) withDuplicates(matches []dedup.DuplicateMatch) *mockDeduplicator {
	m.duplicates = matches
	return m
}

func (m *mockDeduplicator) withMergeResult(merged *models.Behavior) *mockDeduplicator {
	m.mergeResult = merged
	return m
}

func (m *mockDeduplicator) withFindError(err error) *mockDeduplicator {
	m.findErr = err
	return m
}

func (m *mockDeduplicator) withMergeError(err error) *mockDeduplicator {
	m.mergeErr = err
	return m
}

func (m *mockDeduplicator) FindDuplicates(ctx context.Context, behavior *models.Behavior) ([]dedup.DuplicateMatch, error) {
	m.findCallCount++
	if m.findErr != nil {
		return nil, m.findErr
	}
	return m.duplicates, nil
}

func (m *mockDeduplicator) MergeDuplicates(ctx context.Context, matches []dedup.DuplicateMatch, primary *models.Behavior) (*models.Behavior, error) {
	m.mergeCallCount++
	if m.mergeErr != nil {
		return nil, m.mergeErr
	}
	if m.mergeResult != nil {
		return m.mergeResult, nil
	}
	// Default: return a merged behavior based on primary
	merged := *primary
	merged.ID = primary.ID + "-merged"
	merged.Name = primary.Name + " (merged)"
	return &merged, nil
}

func (m *mockDeduplicator) DeduplicateStore(ctx context.Context, s store.GraphStore) (*dedup.DeduplicationReport, error) {
	return &dedup.DeduplicationReport{}, nil
}

// TestIntegration_FullPipeline tests the complete learning loop pipeline:
// correction -> extraction -> duplicate detection -> auto-merge
func TestIntegration_FullPipeline(t *testing.T) {
	tests := []struct {
		name               string
		correction         models.Correction
		autoMerge          bool
		autoMergeThreshold float64
		duplicates         []dedup.DuplicateMatch
		mergeResult        *models.Behavior
		wantMerged         bool
		wantAutoAccepted   bool
		wantMergeID        string
		wantMergeSimilar   float64
	}{
		{
			name: "no duplicates creates new behavior",
			correction: models.Correction{
				ID:              "corr-1",
				Timestamp:       time.Now(),
				AgentAction:     "used pip install",
				CorrectedAction: "use uv for package management",
				Context: models.ContextSnapshot{
					Timestamp:    time.Now(),
					FileLanguage: "python",
				},
			},
			autoMerge:          true,
			autoMergeThreshold: 0.9,
			duplicates:         []dedup.DuplicateMatch{}, // No duplicates
			wantMerged:         false,
			wantAutoAccepted:   true,
		},
		{
			name: "high similarity duplicate triggers auto-merge",
			correction: models.Correction{
				ID:              "corr-2",
				Timestamp:       time.Now(),
				AgentAction:     "used pip",
				CorrectedAction: "prefer uv over pip",
				Context: models.ContextSnapshot{
					Timestamp:    time.Now(),
					FileLanguage: "python",
				},
			},
			autoMerge:          true,
			autoMergeThreshold: 0.9,
			duplicates: []dedup.DuplicateMatch{
				{
					Behavior: &models.Behavior{
						ID:   "existing-behavior-1",
						Name: "Use uv for Python",
						Kind: models.BehaviorKindDirective,
						Content: models.BehaviorContent{
							Canonical: "use uv instead of pip",
						},
						Confidence: 0.85,
					},
					Similarity:       0.95, // Above threshold
					SimilarityMethod: "llm",
					MergeRecommended: true,
				},
			},
			mergeResult: &models.Behavior{
				ID:   "existing-behavior-1-merged",
				Name: "Use uv for Python (merged)",
				Kind: models.BehaviorKindDirective,
				Content: models.BehaviorContent{
					Canonical: "use uv instead of pip for all Python package management",
				},
				Confidence: 0.9,
			},
			wantMerged:       true,
			wantAutoAccepted: true,
			wantMergeID:      "existing-behavior-1",
			wantMergeSimilar: 0.95,
		},
		{
			name: "below threshold similarity does not merge",
			correction: models.Correction{
				ID:              "corr-3",
				Timestamp:       time.Now(),
				AgentAction:     "used npm install",
				CorrectedAction: "use pnpm instead",
				Context: models.ContextSnapshot{
					Timestamp:    time.Now(),
					FileLanguage: "javascript",
				},
			},
			autoMerge:          true,
			autoMergeThreshold: 0.9,
			duplicates: []dedup.DuplicateMatch{
				{
					Behavior: &models.Behavior{
						ID:   "existing-behavior-2",
						Name: "Use yarn",
						Kind: models.BehaviorKindDirective,
						Content: models.BehaviorContent{
							Canonical: "use yarn instead of npm",
						},
					},
					Similarity:       0.7, // Below threshold
					SimilarityMethod: "jaccard",
					MergeRecommended: false,
				},
			},
			wantMerged:       false,
			wantAutoAccepted: true,
		},
		{
			name: "auto-merge disabled creates new behavior even with duplicates",
			correction: models.Correction{
				ID:              "corr-4",
				Timestamp:       time.Now(),
				AgentAction:     "used print",
				CorrectedAction: "use logging module",
				Context: models.ContextSnapshot{
					Timestamp:    time.Now(),
					FileLanguage: "python",
				},
			},
			autoMerge:          false, // Disabled
			autoMergeThreshold: 0.9,
			duplicates: []dedup.DuplicateMatch{
				{
					Behavior: &models.Behavior{
						ID:   "existing-behavior-3",
						Name: "Use logging",
						Kind: models.BehaviorKindDirective,
					},
					Similarity:       0.95, // Would merge if enabled
					SimilarityMethod: "llm",
					MergeRecommended: true,
				},
			},
			wantMerged:       false,
			wantAutoAccepted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			s := store.NewInMemoryGraphStore()

			// Create mock deduplicator
			mockDedup := newMockDeduplicator().
				withDuplicates(tt.duplicates)
			if tt.mergeResult != nil {
				mockDedup.withMergeResult(tt.mergeResult)
			}

			// Create learning loop with auto-merge configuration
			cfg := &LearningLoopConfig{
				AutoAcceptThreshold: 0.5,
				AutoMerge:           tt.autoMerge,
				AutoMergeThreshold:  tt.autoMergeThreshold,
				Deduplicator:        mockDedup,
			}
			loop := NewLearningLoop(s, cfg)

			// Process the correction
			result, err := loop.ProcessCorrection(ctx, tt.correction)
			if err != nil {
				t.Fatalf("ProcessCorrection failed: %v", err)
			}

			// Verify merge result
			if result.MergedIntoExisting != tt.wantMerged {
				t.Errorf("MergedIntoExisting = %v, want %v", result.MergedIntoExisting, tt.wantMerged)
			}

			// Verify auto-accept
			if result.AutoAccepted != tt.wantAutoAccepted {
				t.Errorf("AutoAccepted = %v, want %v", result.AutoAccepted, tt.wantAutoAccepted)
			}

			// Verify merge details if merge occurred
			if tt.wantMerged {
				if result.MergedBehaviorID != tt.wantMergeID {
					t.Errorf("MergedBehaviorID = %q, want %q", result.MergedBehaviorID, tt.wantMergeID)
				}
				if result.MergeSimilarity != tt.wantMergeSimilar {
					t.Errorf("MergeSimilarity = %v, want %v", result.MergeSimilarity, tt.wantMergeSimilar)
				}
			}
		})
	}
}

// TestIntegration_SimilarCorrections_HighSimilarity tests that similar corrections
// with high semantic similarity yield merged behaviors.
func TestIntegration_SimilarCorrections_HighSimilarity(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Create LLM mock configured for high similarity comparison
	mockLLM := llm.NewMockClient().WithComparisonResult(&llm.ComparisonResult{
		SemanticSimilarity: 0.95,
		IntentMatch:        true,
		MergeCandidate:     true,
		Reasoning:          "Both behaviors express the same intent about using a specific tool",
	}).WithMergeResult(&llm.MergeResult{
		Merged: &models.Behavior{
			ID:   "merged-behavior",
			Name: "Unified package management behavior",
			Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{
				Canonical: "use uv for all Python package management tasks",
				Expanded:  "Always prefer uv over pip for installing, updating, and managing Python packages",
			},
			Confidence: 0.9,
		},
		SourceIDs: []string{"behavior-1", "behavior-2"},
		Reasoning: "Combined similar behaviors about Python package management",
	})

	// Create existing behavior in the store
	existingBehavior := &models.Behavior{
		ID:   "existing-pip-behavior",
		Name: "Prefer uv",
		Kind: models.BehaviorKindDirective,
		Content: models.BehaviorContent{
			Canonical: "use uv instead of pip",
		},
		Confidence: 0.8,
	}

	// Create mock deduplicator that returns the existing behavior as a high-similarity duplicate
	mockDedup := newMockDeduplicator().withDuplicates([]dedup.DuplicateMatch{
		{
			Behavior:         existingBehavior,
			Similarity:       0.95,
			SimilarityMethod: "llm",
			MergeRecommended: true,
		},
	}).withMergeResult(&models.Behavior{
		ID:   "existing-pip-behavior-merged",
		Name: "Prefer uv (merged)",
		Kind: models.BehaviorKindDirective,
		Content: models.BehaviorContent{
			Canonical: "use uv for all Python package operations",
		},
		Confidence: 0.9,
	})

	// Configure loop with auto-merge enabled and LLM client
	cfg := &LearningLoopConfig{
		AutoAcceptThreshold: 0.5,
		AutoMerge:           true,
		AutoMergeThreshold:  0.9,
		LLMClient:           mockLLM,
		Deduplicator:        mockDedup,
	}
	loop := NewLearningLoop(s, cfg)

	// Process a correction similar to the existing behavior
	correction := models.Correction{
		ID:              "similar-correction-1",
		Timestamp:       time.Now(),
		AgentAction:     "ran pip install package",
		CorrectedAction: "always use uv instead of pip for installing packages",
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

	// Verify behavior was merged
	if !result.MergedIntoExisting {
		t.Error("expected behavior to be merged into existing")
	}

	// Verify merge similarity
	if result.MergeSimilarity < 0.9 {
		t.Errorf("expected merge similarity >= 0.9, got %v", result.MergeSimilarity)
	}

	// Verify merged behavior ID
	if result.MergedBehaviorID != "existing-pip-behavior" {
		t.Errorf("expected MergedBehaviorID = %q, got %q", "existing-pip-behavior", result.MergedBehaviorID)
	}

	// Verify deduplicator was called
	if mockDedup.findCallCount != 1 {
		t.Errorf("expected FindDuplicates to be called once, got %d", mockDedup.findCallCount)
	}
	if mockDedup.mergeCallCount != 1 {
		t.Errorf("expected MergeDuplicates to be called once, got %d", mockDedup.mergeCallCount)
	}
}

// TestIntegration_DissimilarCorrections_SeparateBehaviors tests that dissimilar
// corrections create separate behaviors rather than merging.
func TestIntegration_DissimilarCorrections_SeparateBehaviors(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Create LLM mock configured for low similarity
	mockLLM := llm.NewMockClient().WithComparisonResult(&llm.ComparisonResult{
		SemanticSimilarity: 0.3,
		IntentMatch:        false,
		MergeCandidate:     false,
		Reasoning:          "Behaviors express different intents about different tools",
	})

	// Create mock deduplicator that returns low-similarity matches
	mockDedup := newMockDeduplicator().withDuplicates([]dedup.DuplicateMatch{
		{
			Behavior: &models.Behavior{
				ID:   "existing-go-behavior",
				Name: "Format Go code",
				Kind: models.BehaviorKindDirective,
				Content: models.BehaviorContent{
					Canonical: "always run go fmt before committing",
				},
			},
			Similarity:       0.3, // Low similarity
			SimilarityMethod: "jaccard",
			MergeRecommended: false,
		},
	})

	// Configure loop with auto-merge enabled but using threshold that won't be met
	cfg := &LearningLoopConfig{
		AutoAcceptThreshold: 0.5,
		AutoMerge:           true,
		AutoMergeThreshold:  0.9, // Much higher than the 0.3 similarity
		LLMClient:           mockLLM,
		Deduplicator:        mockDedup,
	}
	loop := NewLearningLoop(s, cfg)

	// Process a correction about Python (dissimilar to the Go behavior)
	correction := models.Correction{
		ID:              "dissimilar-correction-1",
		Timestamp:       time.Now(),
		AgentAction:     "used raw SQL queries",
		CorrectedAction: "use parameterized queries to prevent SQL injection",
		Context: models.ContextSnapshot{
			Timestamp:    time.Now(),
			FileLanguage: "python",
			FilePath:     "database.py",
		},
	}

	result, err := loop.ProcessCorrection(ctx, correction)
	if err != nil {
		t.Fatalf("ProcessCorrection failed: %v", err)
	}

	// Verify behavior was NOT merged
	if result.MergedIntoExisting {
		t.Error("expected behavior to NOT be merged into existing")
	}

	// Verify a new behavior was created
	if result.CandidateBehavior.ID == "" {
		t.Error("expected a new behavior to be created")
	}

	// Verify the behavior was stored
	node, err := s.GetNode(ctx, result.CandidateBehavior.ID)
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}
	if node == nil {
		t.Error("expected behavior to be stored in the graph")
	}

	// Verify deduplicator was called but merge was not
	if mockDedup.findCallCount != 1 {
		t.Errorf("expected FindDuplicates to be called once, got %d", mockDedup.findCallCount)
	}
	if mockDedup.mergeCallCount != 0 {
		t.Errorf("expected MergeDuplicates to NOT be called, got %d", mockDedup.mergeCallCount)
	}
}

// TestIntegration_MultipleCorrections_BatchProcessing tests processing
// multiple corrections in sequence with deduplication.
func TestIntegration_MultipleCorrections_BatchProcessing(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Track behaviors that have been "added" for dynamic duplicate detection
	var addedBehaviors []*models.Behavior

	// Create a dynamic mock deduplicator that returns previously added behaviors
	mockDedup := &mockDeduplicator{
		duplicates: make([]dedup.DuplicateMatch, 0),
	}

	// Override FindDuplicates to return previously added behaviors
	originalFind := mockDedup.FindDuplicates
	dynamicFind := func(ctx context.Context, behavior *models.Behavior) ([]dedup.DuplicateMatch, error) {
		mockDedup.findCallCount++
		if mockDedup.findErr != nil {
			return nil, mockDedup.findErr
		}

		// Return previously added behaviors as potential duplicates with low similarity
		// so they don't trigger merge
		matches := make([]dedup.DuplicateMatch, 0)
		for _, b := range addedBehaviors {
			matches = append(matches, dedup.DuplicateMatch{
				Behavior:         b,
				Similarity:       0.5, // Below merge threshold
				SimilarityMethod: "jaccard",
				MergeRecommended: false,
			})
		}
		return matches, nil
	}
	_ = originalFind // Avoid unused warning

	// Use a wrapper deduplicator
	wrapperDedup := &dynamicMockDeduplicator{
		findFunc: dynamicFind,
		mergeFunc: func(ctx context.Context, matches []dedup.DuplicateMatch, primary *models.Behavior) (*models.Behavior, error) {
			merged := *primary
			merged.ID = primary.ID + "-merged"
			return &merged, nil
		},
	}

	cfg := &LearningLoopConfig{
		AutoAcceptThreshold: 0.5,
		AutoMerge:           true,
		AutoMergeThreshold:  0.9,
		Deduplicator:        wrapperDedup,
	}
	loop := NewLearningLoop(s, cfg)

	// Process multiple different corrections
	corrections := []models.Correction{
		{
			ID:              "batch-corr-1",
			Timestamp:       time.Now(),
			AgentAction:     "used print statements",
			CorrectedAction: "use logging module instead of print",
			Context: models.ContextSnapshot{
				Timestamp:    time.Now(),
				FileLanguage: "python",
			},
		},
		{
			ID:              "batch-corr-2",
			Timestamp:       time.Now(),
			AgentAction:     "used var keyword",
			CorrectedAction: "use let or const instead of var",
			Context: models.ContextSnapshot{
				Timestamp:    time.Now(),
				FileLanguage: "javascript",
			},
		},
		{
			ID:              "batch-corr-3",
			Timestamp:       time.Now(),
			AgentAction:     "hardcoded credentials",
			CorrectedAction: "use environment variables for secrets",
			Context: models.ContextSnapshot{
				Timestamp:    time.Now(),
				FileLanguage: "go",
			},
		},
	}

	var results []*LearningResult
	for _, corr := range corrections {
		result, err := loop.ProcessCorrection(ctx, corr)
		if err != nil {
			t.Fatalf("ProcessCorrection failed for %s: %v", corr.ID, err)
		}
		results = append(results, result)

		// Simulate tracking the added behavior for future duplicate checks
		addedBehaviors = append(addedBehaviors, &result.CandidateBehavior)
	}

	// Verify all corrections were processed
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	// Verify each created a separate behavior (no merges since similarity is low)
	for i, result := range results {
		if result.MergedIntoExisting {
			t.Errorf("correction %d: expected no merge, got merged", i)
		}
		if result.CandidateBehavior.ID == "" {
			t.Errorf("correction %d: expected behavior ID to be set", i)
		}
	}

	// Verify behaviors are stored in graph
	nodes, err := s.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		t.Fatalf("QueryNodes failed: %v", err)
	}
	if len(nodes) != 3 {
		t.Errorf("expected 3 behaviors in store, got %d", len(nodes))
	}
}

// dynamicMockDeduplicator allows dynamic duplicate detection for batch tests.
type dynamicMockDeduplicator struct {
	findFunc  func(ctx context.Context, behavior *models.Behavior) ([]dedup.DuplicateMatch, error)
	mergeFunc func(ctx context.Context, matches []dedup.DuplicateMatch, primary *models.Behavior) (*models.Behavior, error)
}

func (d *dynamicMockDeduplicator) FindDuplicates(ctx context.Context, behavior *models.Behavior) ([]dedup.DuplicateMatch, error) {
	if d.findFunc != nil {
		return d.findFunc(ctx, behavior)
	}
	return nil, nil
}

func (d *dynamicMockDeduplicator) MergeDuplicates(ctx context.Context, matches []dedup.DuplicateMatch, primary *models.Behavior) (*models.Behavior, error) {
	if d.mergeFunc != nil {
		return d.mergeFunc(ctx, matches, primary)
	}
	return primary, nil
}

func (d *dynamicMockDeduplicator) DeduplicateStore(ctx context.Context, s store.GraphStore) (*dedup.DeduplicationReport, error) {
	return &dedup.DeduplicationReport{}, nil
}

// TestIntegration_MockClient_ComparisonResults tests that MockClient properly
// controls similarity scores for integration testing.
func TestIntegration_MockClient_ComparisonResults(t *testing.T) {
	tests := []struct {
		name               string
		similarityScore    float64
		intentMatch        bool
		mergeCandidate     bool
		wantIntentMatch    bool
		wantMergeCandidate bool
	}{
		{
			name:               "high similarity with intent match",
			similarityScore:    0.95,
			intentMatch:        true,
			mergeCandidate:     true,
			wantIntentMatch:    true,
			wantMergeCandidate: true,
		},
		{
			name:               "medium similarity no intent match",
			similarityScore:    0.6,
			intentMatch:        false,
			mergeCandidate:     false,
			wantIntentMatch:    false,
			wantMergeCandidate: false,
		},
		{
			name:               "low similarity",
			similarityScore:    0.2,
			intentMatch:        false,
			mergeCandidate:     false,
			wantIntentMatch:    false,
			wantMergeCandidate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			mockClient := llm.NewMockClient().WithComparisonResult(&llm.ComparisonResult{
				SemanticSimilarity: tt.similarityScore,
				IntentMatch:        tt.intentMatch,
				MergeCandidate:     tt.mergeCandidate,
			})

			behaviorA := &models.Behavior{
				ID:   "behavior-a",
				Name: "Test Behavior A",
				Kind: models.BehaviorKindDirective,
			}
			behaviorB := &models.Behavior{
				ID:   "behavior-b",
				Name: "Test Behavior B",
				Kind: models.BehaviorKindDirective,
			}

			result, err := mockClient.CompareBehaviors(ctx, behaviorA, behaviorB)
			if err != nil {
				t.Fatalf("CompareBehaviors failed: %v", err)
			}

			if result.SemanticSimilarity != tt.similarityScore {
				t.Errorf("SemanticSimilarity = %v, want %v", result.SemanticSimilarity, tt.similarityScore)
			}
			if result.IntentMatch != tt.wantIntentMatch {
				t.Errorf("IntentMatch = %v, want %v", result.IntentMatch, tt.wantIntentMatch)
			}
			if result.MergeCandidate != tt.wantMergeCandidate {
				t.Errorf("MergeCandidate = %v, want %v", result.MergeCandidate, tt.wantMergeCandidate)
			}

			// Verify call was tracked
			if mockClient.CompareCallCount() != 1 {
				t.Errorf("expected 1 compare call, got %d", mockClient.CompareCallCount())
			}
		})
	}
}

// TestIntegration_AutoMergeThreshold_EdgeCases tests edge cases around the
// auto-merge threshold boundary.
func TestIntegration_AutoMergeThreshold_EdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		threshold  float64
		similarity float64
		wantMerge  bool
	}{
		{
			name:       "similarity equals threshold",
			threshold:  0.9,
			similarity: 0.9,
			wantMerge:  true,
		},
		{
			name:       "similarity just below threshold",
			threshold:  0.9,
			similarity: 0.89,
			wantMerge:  false,
		},
		{
			name:       "similarity just above threshold",
			threshold:  0.9,
			similarity: 0.91,
			wantMerge:  true,
		},
		{
			name:       "zero threshold always merges with duplicates",
			threshold:  0.0,
			similarity: 0.1,
			wantMerge:  true,
		},
		{
			name:       "threshold at 1.0 requires perfect match",
			threshold:  1.0,
			similarity: 0.99,
			wantMerge:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			s := store.NewInMemoryGraphStore()

			mockDedup := newMockDeduplicator().withDuplicates([]dedup.DuplicateMatch{
				{
					Behavior: &models.Behavior{
						ID:   "existing-behavior",
						Name: "Existing",
						Kind: models.BehaviorKindDirective,
					},
					Similarity:       tt.similarity,
					SimilarityMethod: "llm",
					MergeRecommended: tt.similarity >= tt.threshold,
				},
			})

			cfg := &LearningLoopConfig{
				AutoAcceptThreshold: 0.5,
				AutoMerge:           true,
				AutoMergeThreshold:  tt.threshold,
				Deduplicator:        mockDedup,
			}
			loop := NewLearningLoop(s, cfg)

			correction := models.Correction{
				ID:              "edge-case-correction",
				Timestamp:       time.Now(),
				AgentAction:     "did something",
				CorrectedAction: "do something else",
				Context: models.ContextSnapshot{
					Timestamp: time.Now(),
				},
			}

			result, err := loop.ProcessCorrection(ctx, correction)
			if err != nil {
				t.Fatalf("ProcessCorrection failed: %v", err)
			}

			if result.MergedIntoExisting != tt.wantMerge {
				t.Errorf("MergedIntoExisting = %v, want %v (threshold=%v, similarity=%v)",
					result.MergedIntoExisting, tt.wantMerge, tt.threshold, tt.similarity)
			}
		})
	}
}
