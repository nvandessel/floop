package activation

import (
	"sort"

	"github.com/nvandessel/feedback-loop/internal/models"
)

// ActivationResult represents a behavior that matched the current context
type ActivationResult struct {
	Behavior models.Behavior

	// MatchedConditions shows which 'when' conditions matched
	MatchedConditions map[string]interface{}

	// Specificity indicates how specific the match is (more conditions = higher)
	Specificity int
}

// Evaluator determines which behaviors are active for a given context
type Evaluator struct{}

// NewEvaluator creates a new evaluator
func NewEvaluator() *Evaluator {
	return &Evaluator{}
}

// Evaluate checks which behaviors match the given context
// Returns behaviors that match, sorted by specificity (most specific first)
func (e *Evaluator) Evaluate(ctx models.ContextSnapshot, behaviors []models.Behavior) []ActivationResult {
	var results []ActivationResult

	for _, b := range behaviors {
		if matches, matchedConditions := e.matchesBehavior(ctx, b); matches {
			results = append(results, ActivationResult{
				Behavior:          b,
				MatchedConditions: matchedConditions,
				Specificity:       len(matchedConditions),
			})
		}
	}

	// Sort by specificity (higher first), then by priority
	sortBySpecificityAndPriority(results)

	return results
}

// matchesBehavior checks if a context matches a behavior's 'when' conditions
func (e *Evaluator) matchesBehavior(ctx models.ContextSnapshot, b models.Behavior) (bool, map[string]interface{}) {
	// Behaviors with no 'when' conditions always match
	if len(b.When) == 0 {
		return true, nil
	}

	// Use the existing ContextSnapshot.Matches() method
	if ctx.Matches(b.When) {
		return true, b.When
	}

	return false, nil
}

// sortBySpecificityAndPriority sorts results by specificity desc, then priority desc
func sortBySpecificityAndPriority(results []ActivationResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Specificity != results[j].Specificity {
			return results[i].Specificity > results[j].Specificity
		}
		return results[i].Behavior.Priority > results[j].Behavior.Priority
	})
}

// IsActive is a convenience method to check if a specific behavior is active
func (e *Evaluator) IsActive(ctx models.ContextSnapshot, b models.Behavior) bool {
	matches, _ := e.matchesBehavior(ctx, b)
	return matches
}

// WhyActive explains why a behavior is or isn't active for a context
func (e *Evaluator) WhyActive(ctx models.ContextSnapshot, b models.Behavior) ActivationExplanation {
	explanation := ActivationExplanation{
		BehaviorID: b.ID,
		IsActive:   false,
	}

	if len(b.When) == 0 {
		explanation.IsActive = true
		explanation.Reason = "No activation conditions - always active"
		return explanation
	}

	// Check each condition
	for key, required := range b.When {
		conditionResult := ConditionResult{
			Field:    key,
			Required: required,
		}

		// Get actual value from context
		conditionResult.Actual = ctx.GetField(key)
		conditionResult.Matched = ctx.Matches(map[string]interface{}{key: required})

		explanation.Conditions = append(explanation.Conditions, conditionResult)
	}

	// Behavior is active if all conditions matched
	allMatched := true
	for _, c := range explanation.Conditions {
		if !c.Matched {
			allMatched = false
			break
		}
	}

	explanation.IsActive = allMatched
	if allMatched {
		explanation.Reason = "All conditions matched"
	} else {
		explanation.Reason = "One or more conditions did not match"
	}

	return explanation
}

// ActivationExplanation provides detailed info about why a behavior is/isn't active
type ActivationExplanation struct {
	BehaviorID string            `json:"behavior_id"`
	IsActive   bool              `json:"is_active"`
	Reason     string            `json:"reason"`
	Conditions []ConditionResult `json:"conditions,omitempty"`
}

// ConditionResult shows the result of evaluating one 'when' condition
type ConditionResult struct {
	Field    string      `json:"field"`
	Required interface{} `json:"required"`
	Actual   interface{} `json:"actual"`
	Matched  bool        `json:"matched"`
}
