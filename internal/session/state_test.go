package session

import (
	"sync"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/spreading"
)

func TestState_NewSession(t *testing.T) {
	s := NewState(DefaultConfig())

	if got := s.TotalTokensUsed(); got != 0 {
		t.Errorf("TotalTokensUsed() = %d, want 0", got)
	}
	if got := s.RemainingBudget(); got != 3000 {
		t.Errorf("RemainingBudget() = %d, want 3000", got)
	}
	if got := s.PromptCount(); got != 0 {
		t.Errorf("PromptCount() = %d, want 0", got)
	}
	if rec := s.GetInjection("nonexistent"); rec != nil {
		t.Errorf("GetInjection(nonexistent) = %v, want nil", rec)
	}
}

func TestState_RecordInjection(t *testing.T) {
	s := NewState(DefaultConfig())

	// First injection.
	s.RecordInjection("b1", models.TierFull, 0.9, 100)

	rec := s.GetInjection("b1")
	if rec == nil {
		t.Fatal("GetInjection(b1) = nil, want non-nil")
	}
	if rec.Count != 1 {
		t.Errorf("Count = %d, want 1", rec.Count)
	}
	if rec.Tier != models.TierFull {
		t.Errorf("Tier = %v, want TierFull", rec.Tier)
	}
	if rec.TokenCost != 100 {
		t.Errorf("TokenCost = %d, want 100", rec.TokenCost)
	}
	if s.TotalTokensUsed() != 100 {
		t.Errorf("TotalTokensUsed() = %d, want 100", s.TotalTokensUsed())
	}

	// Re-inject same behavior: count should increment.
	s.RecordInjection("b1", models.TierSummary, 0.5, 50)

	rec = s.GetInjection("b1")
	if rec == nil {
		t.Fatal("GetInjection(b1) = nil after re-inject")
	}
	if rec.Count != 2 {
		t.Errorf("Count = %d, want 2", rec.Count)
	}
	if rec.Tier != models.TierSummary {
		t.Errorf("Tier = %v, want TierSummary", rec.Tier)
	}
	if s.TotalTokensUsed() != 150 {
		t.Errorf("TotalTokensUsed() = %d, want 150", s.TotalTokensUsed())
	}
}

func TestState_ShouldInject_NeverInjected(t *testing.T) {
	s := NewState(DefaultConfig())

	tests := []struct {
		name          string
		requestedTier models.InjectionTier
		tokenCost     int
		wantTier      models.InjectionTier
		wantInject    bool
	}{
		{"full tier", models.TierFull, 100, models.TierFull, true},
		{"summary tier", models.TierSummary, 50, models.TierSummary, true},
		{"name-only tier", models.TierNameOnly, 10, models.TierNameOnly, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tier, ok := s.ShouldInject("new-behavior", tt.requestedTier, 0.8, tt.tokenCost)
			if ok != tt.wantInject {
				t.Errorf("ShouldInject() inject = %v, want %v", ok, tt.wantInject)
			}
			if tier != tt.wantTier {
				t.Errorf("ShouldInject() tier = %v, want %v", tier, tt.wantTier)
			}
		})
	}
}

func TestState_ShouldInject_UpgradeTier(t *testing.T) {
	s := NewState(DefaultConfig())

	// Inject at TierSummary.
	s.RecordInjection("b1", models.TierSummary, 0.4, 50)

	// Request TierFull (higher detail = lower tier number). Should upgrade.
	tier, ok := s.ShouldInject("b1", models.TierFull, 0.9, 100)
	if !ok {
		t.Fatal("ShouldInject() = false, want true (upgrade)")
	}
	if tier != models.TierFull {
		t.Errorf("ShouldInject() tier = %v, want TierFull", tier)
	}

	// Inject at TierNameOnly.
	s.RecordInjection("b2", models.TierNameOnly, 0.2, 10)

	// Request TierSummary. Should upgrade.
	tier, ok = s.ShouldInject("b2", models.TierSummary, 0.5, 50)
	if !ok {
		t.Fatal("ShouldInject(b2) = false, want true (upgrade)")
	}
	if tier != models.TierSummary {
		t.Errorf("ShouldInject(b2) tier = %v, want TierSummary", tier)
	}

	// Request same or lower tier should NOT upgrade (backoff applies).
	_, ok = s.ShouldInject("b1", models.TierSummary, 0.5, 50)
	if ok {
		t.Error("ShouldInject() = true for same tier within backoff, want false")
	}
}

func TestState_ShouldInject_BackoffWindow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BackoffMultiplier = 3
	s := NewState(cfg)

	// Inject once. Count=1, backoff window = 1*3 = 3 prompts.
	s.RecordInjection("b1", models.TierFull, 0.9, 100)

	// Still at prompt 0 (injection prompt). Should not reinject.
	_, ok := s.ShouldInject("b1", models.TierFull, 0.9, 100)
	if ok {
		t.Error("ShouldInject() = true within backoff window, want false")
	}

	// Advance 2 prompts. Still within window (0+2 < 3).
	s.IncrementPromptCount()
	s.IncrementPromptCount()
	_, ok = s.ShouldInject("b1", models.TierFull, 0.9, 100)
	if ok {
		t.Error("ShouldInject() = true at prompt 2 (window=3), want false")
	}

	// Advance past the backoff window (prompt 3).
	s.IncrementPromptCount()
	tier, ok := s.ShouldInject("b1", models.TierFull, 0.9, 100)
	if !ok {
		t.Fatal("ShouldInject() = false past backoff window, want true")
	}
	if tier != models.TierFull {
		t.Errorf("ShouldInject() tier = %v, want TierFull", tier)
	}

	// Re-inject at prompt 3. Count=2, new backoff = 2*3 = 6 prompts.
	s.RecordInjection("b1", models.TierFull, 0.9, 100)

	// Advance 5 prompts (to prompt 8). Should still be in window (8 - 3 = 5 < 6).
	for i := 0; i < 5; i++ {
		s.IncrementPromptCount()
	}
	_, ok = s.ShouldInject("b1", models.TierFull, 0.9, 100)
	if ok {
		t.Error("ShouldInject() = true within 2nd backoff window, want false")
	}

	// Advance 1 more (prompt 9). Now 9 - 3 = 6 >= 6, past backoff.
	s.IncrementPromptCount()
	_, ok = s.ShouldInject("b1", models.TierFull, 0.9, 100)
	if !ok {
		t.Error("ShouldInject() = false past 2nd backoff window, want true")
	}
}

func TestState_ShouldInject_BudgetExhausted(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxTokenBudget = 200
	cfg.MaxPerInjection = 150
	s := NewState(cfg)

	// Fill most of the budget.
	s.RecordInjection("b1", models.TierFull, 0.9, 150)

	// Remaining = 50. Requesting 100 tokens should fail.
	_, ok := s.ShouldInject("b-new", models.TierFull, 0.8, 100)
	if ok {
		t.Error("ShouldInject() = true with budget exhausted, want false")
	}

	// Requesting 50 tokens should succeed (fits remaining).
	tier, ok := s.ShouldInject("b-new", models.TierSummary, 0.5, 50)
	if !ok {
		t.Fatal("ShouldInject() = false with budget available, want true")
	}
	if tier != models.TierSummary {
		t.Errorf("ShouldInject() tier = %v, want TierSummary", tier)
	}

	// Verify MaxPerInjection limit.
	_, ok = s.ShouldInject("b-big", models.TierFull, 0.9, 200)
	if ok {
		t.Error("ShouldInject() = true exceeding MaxPerInjection, want false")
	}
}

func TestState_RemainingBudget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxTokenBudget = 500
	s := NewState(cfg)

	tests := []struct {
		name       string
		inject     bool
		behaviorID string
		tokenCost  int
		wantBudget int
	}{
		{"initial", false, "", 0, 500},
		{"after 100", true, "b1", 100, 400},
		{"after 200", true, "b2", 200, 200},
		{"after 150", true, "b3", 150, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.inject {
				s.RecordInjection(tt.behaviorID, models.TierFull, 0.9, tt.tokenCost)
			}
			if got := s.RemainingBudget(); got != tt.wantBudget {
				t.Errorf("RemainingBudget() = %d, want %d", got, tt.wantBudget)
			}
		})
	}

	// Budget should never go negative.
	s.RecordInjection("b4", models.TierFull, 0.9, 100)
	if got := s.RemainingBudget(); got != 0 {
		t.Errorf("RemainingBudget() = %d, want 0 (should not go negative)", got)
	}
}

func TestState_FilterResults(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxTokenBudget = 1000
	cfg.BackoffMultiplier = 5
	s := NewState(cfg)

	// Pre-inject b1 at TierSummary so it can be upgraded.
	s.RecordInjection("b1", models.TierSummary, 0.4, 50)

	// Advance prompts past backoff for b1 (1 * 5 = 5 prompts).
	for i := 0; i < 6; i++ {
		s.IncrementPromptCount()
	}

	// Pre-inject b3 recently so it should be in backoff.
	s.RecordInjection("b3", models.TierFull, 0.8, 100)

	results := []spreading.Result{
		{BehaviorID: "b1", Activation: 0.9}, // Previously injected at summary, now high → upgrade
		{BehaviorID: "b2", Activation: 0.5}, // Never injected → new
		{BehaviorID: "b3", Activation: 0.7}, // Recently injected, in backoff → skip
		{BehaviorID: "b4", Activation: 0.3}, // Never injected → new
	}

	tierMapper := func(activation float64) models.InjectionTier {
		if activation >= 0.7 {
			return models.TierFull
		}
		if activation >= 0.3 {
			return models.TierSummary
		}
		return models.TierNameOnly
	}

	tokenEstimator := func(_ string, tier models.InjectionTier) int {
		switch tier {
		case models.TierFull:
			return 100
		case models.TierSummary:
			return 50
		default:
			return 10
		}
	}

	filtered := s.FilterResults(results, tierMapper, tokenEstimator)

	// Expect: b1 (upgrade), b2 (new), b4 (new). b3 should be skipped (backoff).
	if len(filtered) != 3 {
		t.Fatalf("FilterResults() returned %d results, want 3", len(filtered))
	}

	// Check b1 is an upgrade.
	found := false
	for _, fr := range filtered {
		if fr.BehaviorID == "b1" {
			found = true
			if !fr.IsUpgrade {
				t.Error("b1 should be IsUpgrade=true")
			}
			if fr.Tier != models.TierFull {
				t.Errorf("b1 tier = %v, want TierFull", fr.Tier)
			}
		}
	}
	if !found {
		t.Error("b1 not found in filtered results")
	}

	// Check b2 is new (neither upgrade nor reinforce).
	for _, fr := range filtered {
		if fr.BehaviorID == "b2" {
			if fr.IsUpgrade || fr.IsReinforce {
				t.Error("b2 should not be upgrade or reinforce")
			}
			if fr.Tier != models.TierSummary {
				t.Errorf("b2 tier = %v, want TierSummary", fr.Tier)
			}
		}
	}

	// Check b3 is not present.
	for _, fr := range filtered {
		if fr.BehaviorID == "b3" {
			t.Error("b3 should be filtered out (backoff)")
		}
	}
}

func TestState_ThreadSafety(t *testing.T) {
	s := NewState(DefaultConfig())

	var wg sync.WaitGroup
	const goroutines = 50
	const opsPerGoroutine = 100

	// Concurrent writers.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				behaviorID := "b" + string(rune('A'+id%26))
				s.RecordInjection(behaviorID, models.TierFull, 0.9, 10)
				s.IncrementPromptCount()
			}
		}(i)
	}

	// Concurrent readers.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				behaviorID := "b" + string(rune('A'+id%26))
				s.GetInjection(behaviorID)
				s.ShouldInject(behaviorID, models.TierSummary, 0.5, 50)
				s.RemainingBudget()
				s.TotalTokensUsed()
				s.PromptCount()
			}
		}(i)
	}

	wg.Wait()

	// If we got here without -race detector panicking, we pass.
	// Just sanity-check that some state was tracked.
	if s.TotalTokensUsed() == 0 {
		t.Error("TotalTokensUsed() = 0 after concurrent injections, want > 0")
	}
	if s.PromptCount() == 0 {
		t.Error("PromptCount() = 0 after concurrent increments, want > 0")
	}
}

func TestState_Reset(t *testing.T) {
	s := NewState(DefaultConfig())

	// Add some state.
	s.RecordInjection("b1", models.TierFull, 0.9, 100)
	s.RecordInjection("b2", models.TierSummary, 0.5, 50)
	s.IncrementPromptCount()
	s.IncrementPromptCount()

	// Verify state exists.
	if s.TotalTokensUsed() == 0 {
		t.Fatal("TotalTokensUsed() = 0 before reset, want > 0")
	}

	// Reset.
	s.Reset()

	// Verify clean state.
	if got := s.TotalTokensUsed(); got != 0 {
		t.Errorf("TotalTokensUsed() after reset = %d, want 0", got)
	}
	if got := s.RemainingBudget(); got != 3000 {
		t.Errorf("RemainingBudget() after reset = %d, want 3000", got)
	}
	if got := s.PromptCount(); got != 0 {
		t.Errorf("PromptCount() after reset = %d, want 0", got)
	}
	if rec := s.GetInjection("b1"); rec != nil {
		t.Errorf("GetInjection(b1) after reset = %v, want nil", rec)
	}
	if rec := s.GetInjection("b2"); rec != nil {
		t.Errorf("GetInjection(b2) after reset = %v, want nil", rec)
	}
}
