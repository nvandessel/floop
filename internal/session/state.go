// Package session tracks injection state for a single MCP server session.
// It prevents re-injection of already-seen behaviors, manages token budgets,
// and supports strategic reinforcement with exponential backoff.
//
// All public methods are safe for concurrent use.
package session

import (
	"sync"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/spreading"
)

// InjectionRecord tracks a single behavior injection event.
type InjectionRecord struct {
	BehaviorID string               `json:"behavior_id"`
	Tier       models.InjectionTier `json:"tier"`
	Activation float64              `json:"activation"`
	TokenCost  int                  `json:"token_cost"`
	InjectedAt time.Time            `json:"injected_at"`
	Count      int                  `json:"count"`       // how many times injected this session
	LastPrompt int                  `json:"last_prompt"` // prompt count when last injected
}

// Config holds session state configuration.
type Config struct {
	// MaxTokenBudget is the total token budget for the entire session.
	// Once exceeded, no more injections occur. Default: 3000.
	MaxTokenBudget int

	// MaxPerInjection is the max tokens for a single injection event. Default: 500.
	MaxPerInjection int

	// BackoffMultiplier controls exponential backoff for re-injection.
	// After Nth injection, wait N*BackoffMultiplier prompts before next.
	// Default: 5 (1st: immediate, 2nd: after 5 prompts, 3rd: after 10).
	BackoffMultiplier int
}

// DefaultConfig returns the default session configuration.
func DefaultConfig() Config {
	return Config{
		MaxTokenBudget:    3000,
		MaxPerInjection:   500,
		BackoffMultiplier: 5,
	}
}

// State tracks injection history for a single session.
// Thread-safe for concurrent hook invocations.
type State struct {
	mu              sync.RWMutex
	config          Config
	injections      map[string]*InjectionRecord
	totalTokensUsed int
	promptCount     int
}

// NewState creates a new session state tracker.
func NewState(config Config) *State {
	return &State{
		config:     config,
		injections: make(map[string]*InjectionRecord),
	}
}

// RecordInjection records that a behavior was injected.
func (s *State) RecordInjection(behaviorID string, tier models.InjectionTier, activation float64, tokenCost int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, exists := s.injections[behaviorID]
	if !exists {
		s.injections[behaviorID] = &InjectionRecord{
			BehaviorID: behaviorID,
			Tier:       tier,
			Activation: activation,
			TokenCost:  tokenCost,
			InjectedAt: time.Now(),
			Count:      1,
			LastPrompt: s.promptCount,
		}
		s.totalTokensUsed += tokenCost
		return
	}

	rec.Count++
	rec.Tier = tier
	rec.Activation = activation
	rec.TokenCost = tokenCost
	rec.InjectedAt = time.Now()
	rec.LastPrompt = s.promptCount
	s.totalTokensUsed += tokenCost
}

// GetInjection returns the injection record for a behavior, or nil if never injected.
func (s *State) GetInjection(behaviorID string) *InjectionRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, exists := s.injections[behaviorID]
	if !exists {
		return nil
	}

	// Return a copy to avoid data races on the returned value.
	cp := *rec
	return &cp
}

// IncrementPromptCount should be called on each user prompt to track prompt cadence.
func (s *State) IncrementPromptCount() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.promptCount++
}

// PromptCount returns the current prompt count.
func (s *State) PromptCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.promptCount
}

// RemainingBudget returns how many tokens are still available.
func (s *State) RemainingBudget() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	remaining := s.config.MaxTokenBudget - s.totalTokensUsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// TotalTokensUsed returns the total tokens consumed by injections.
func (s *State) TotalTokensUsed() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.totalTokensUsed
}

// ShouldInject determines whether a behavior should be injected given its current state.
// Returns the recommended tier and whether to inject at all.
//
// Decision logic:
//   - Never injected: inject at requested tier
//   - Injected at lower tier + now higher activation: upgrade (inject at higher tier)
//   - Injected at same/higher tier + within backoff window: skip
//   - Injected at same/higher tier + past backoff window: reinforce (inject again)
//   - Budget exhausted: skip
//   - Single injection exceeds MaxPerInjection: skip
func (s *State) ShouldInject(behaviorID string, requestedTier models.InjectionTier, activation float64, tokenCost int) (models.InjectionTier, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check per-injection budget limit.
	if tokenCost > s.config.MaxPerInjection {
		return 0, false
	}

	// Check session-wide budget.
	if s.totalTokensUsed+tokenCost > s.config.MaxTokenBudget {
		return 0, false
	}

	rec, exists := s.injections[behaviorID]
	if !exists {
		// Never injected: inject at requested tier.
		return requestedTier, true
	}

	// Upgrade check: a lower tier number means more detail (TierFull < TierSummary < TierNameOnly).
	// If the requested tier is more detailed than what we previously injected, upgrade.
	if requestedTier < rec.Tier {
		return requestedTier, true
	}

	// Backoff check: within backoff window, skip.
	// Backoff window = Count * BackoffMultiplier prompts since last injection.
	backoffWindow := rec.Count * s.config.BackoffMultiplier
	promptsSinceInjection := s.promptCount - rec.LastPrompt
	if promptsSinceInjection < backoffWindow {
		return 0, false
	}

	// Past backoff window: reinforce.
	return requestedTier, true
}

// FilteredResult combines activation result with injection decision.
type FilteredResult struct {
	BehaviorID  string               `json:"behavior_id"`
	Activation  float64              `json:"activation"`
	Tier        models.InjectionTier `json:"tier"`
	IsUpgrade   bool                 `json:"is_upgrade"`
	IsReinforce bool                 `json:"is_reinforce"`
}

// FilterResults takes spreading activation results and returns only those
// that should be injected, respecting session state and budget.
//
// The tierMapper converts an activation level to an injection tier.
// The tokenEstimator estimates token cost for a behavior at a given tier.
func (s *State) FilterResults(
	results []spreading.Result,
	tierMapper func(activation float64) models.InjectionTier,
	tokenEstimator func(behaviorID string, tier models.InjectionTier) int,
) []FilteredResult {
	filtered := make([]FilteredResult, 0, len(results))

	for _, r := range results {
		tier := tierMapper(r.Activation)
		tokenCost := tokenEstimator(r.BehaviorID, tier)

		recommendedTier, ok := s.ShouldInject(r.BehaviorID, tier, r.Activation, tokenCost)
		if !ok {
			continue
		}

		fr := FilteredResult{
			BehaviorID: r.BehaviorID,
			Activation: r.Activation,
			Tier:       recommendedTier,
		}

		// Determine if this is an upgrade or reinforcement.
		rec := s.GetInjection(r.BehaviorID)
		if rec != nil {
			if recommendedTier < rec.Tier {
				fr.IsUpgrade = true
			} else {
				fr.IsReinforce = true
			}
		}

		filtered = append(filtered, fr)
	}

	return filtered
}

// Reset clears all session state (for testing or session restart).
func (s *State) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.injections = make(map[string]*InjectionRecord)
	s.totalTokensUsed = 0
	s.promptCount = 0
}
