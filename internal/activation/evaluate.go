package activation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nvandessel/feedback-loop/internal/models"
)

// ActivationResult represents a behavior that matched the current context
type ActivationResult struct {
	Behavior models.Behavior

	// MatchedConditions shows which 'when' conditions were confirmed
	MatchedConditions map[string]interface{}

	// Specificity indicates how specific the match is (number of confirmed conditions)
	Specificity int

	// MatchScore is the ratio of confirmed conditions to total conditions (0.0-1.0).
	// A score of 0.0 means all conditions were absent; 1.0 means all confirmed.
	MatchScore float64
}

// MatchResult captures the outcome of evaluating a behavior's when-conditions.
type MatchResult struct {
	Matched      bool                   // true if no contradictions
	Score        float64                // confirmed / total (0.0-1.0), 0 if no conditions
	Confirmed    map[string]interface{} // conditions that matched
	Absent       []string               // conditions where context had no value
	Contradicted []string               // conditions where context value differed
}

// Evaluator determines which behaviors are active for a given context
type Evaluator struct{}

// NewEvaluator creates a new evaluator
func NewEvaluator() *Evaluator {
	return &Evaluator{}
}

// Evaluate checks which behaviors match the given context.
// A behavior matches if none of its conditions are contradicted.
// Absent conditions (context has no value for the key) are neutral.
// Returns behaviors that match, sorted by specificity (most specific first).
func (e *Evaluator) Evaluate(ctx models.ContextSnapshot, behaviors []models.Behavior) []ActivationResult {
	var results []ActivationResult

	for _, b := range behaviors {
		mr := e.evaluateMatch(ctx, b)
		if mr.Matched {
			results = append(results, ActivationResult{
				Behavior:          b,
				MatchedConditions: mr.Confirmed,
				Specificity:       len(mr.Confirmed),
				MatchScore:        mr.Score,
			})
		}
	}

	// Sort by specificity (higher first), then by priority
	sortBySpecificityAndPriority(results)

	return results
}

// evaluateMatch checks a behavior's when-conditions against the context using
// partial matching semantics:
//   - Confirmed: context has the key and values match
//   - Contradicted: context has the key but values differ (excludes behavior)
//   - Absent: context doesn't have the key (neutral)
func (e *Evaluator) evaluateMatch(ctx models.ContextSnapshot, b models.Behavior) MatchResult {
	if len(b.When) == 0 {
		return MatchResult{Matched: true, Score: 0.0, Confirmed: nil}
	}

	confirmed := make(map[string]interface{})
	var absent []string
	var contradicted []string

	for key, required := range b.When {
		matched, hasValue := ctx.MatchField(key, required)
		if hasValue && !matched {
			contradicted = append(contradicted, key)
		} else if hasValue && matched {
			confirmed[key] = required
		} else {
			absent = append(absent, key)
		}
	}

	if len(contradicted) > 0 {
		return MatchResult{
			Matched:      false,
			Score:        0.0,
			Confirmed:    confirmed,
			Absent:       absent,
			Contradicted: contradicted,
		}
	}

	score := float64(len(confirmed)) / float64(len(b.When))
	return MatchResult{
		Matched:   true,
		Score:     score,
		Confirmed: confirmed,
		Absent:    absent,
	}
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

// IsActive is a convenience method to check if a specific behavior is active.
// A behavior is active if none of its conditions are contradicted by the context.
func (e *Evaluator) IsActive(ctx models.ContextSnapshot, b models.Behavior) bool {
	mr := e.evaluateMatch(ctx, b)
	return mr.Matched
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

	// Reuse evaluateMatch for the core classification logic
	mr := e.evaluateMatch(ctx, b)

	// Build condition details from the match result
	for key, required := range b.When {
		conditionResult := ConditionResult{
			Field:    key,
			Required: required,
			Actual:   ctx.GetField(key),
		}

		if _, ok := mr.Confirmed[key]; ok {
			conditionResult.Status = "confirmed"
			conditionResult.Matched = true
		} else if sliceContains(mr.Contradicted, key) {
			conditionResult.Status = "contradicted"
			conditionResult.Matched = false
		} else {
			conditionResult.Status = "absent"
			conditionResult.Matched = false
		}

		explanation.Conditions = append(explanation.Conditions, conditionResult)
	}

	// Behavior is active if no conditions are contradicted
	explanation.IsActive = mr.Matched
	if len(mr.Contradicted) > 0 {
		sort.Strings(mr.Contradicted)
		explanation.Reason = fmt.Sprintf("Contradicted on: %s", strings.Join(mr.Contradicted, ", "))
	} else if len(mr.Absent) == 0 {
		explanation.Reason = "All conditions confirmed"
	} else {
		explanation.Reason = fmt.Sprintf("Partially matched (%d/%d confirmed, %d absent)",
			len(mr.Confirmed), len(b.When), len(mr.Absent))
	}

	return explanation
}

// sliceContains checks if a string slice contains a value.
func sliceContains(s []string, v string) bool {
	for _, item := range s {
		if item == v {
			return true
		}
	}
	return false
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
	Status   string      `json:"status"` // "confirmed", "contradicted", "absent"
}
