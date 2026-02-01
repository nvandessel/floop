package models

// InjectionTier represents the level of detail for an injected behavior
type InjectionTier int

const (
	// TierFull includes the full canonical content
	TierFull InjectionTier = iota
	// TierSummary includes only a one-line summary
	TierSummary
	// TierOmitted means the behavior is referenced but not included
	TierOmitted
)

// String returns a string representation of the tier
func (t InjectionTier) String() string {
	switch t {
	case TierFull:
		return "full"
	case TierSummary:
		return "summary"
	case TierOmitted:
		return "omitted"
	default:
		return "unknown"
	}
}

// InjectedBehavior represents a behavior prepared for injection with tier info
type InjectedBehavior struct {
	// Behavior is the original behavior being injected
	Behavior *Behavior `json:"behavior"`

	// Tier indicates the level of detail included
	Tier InjectionTier `json:"tier"`

	// Content is the actual content to inject (varies by tier)
	Content string `json:"content"`

	// TokenCost is the estimated token cost for this injection
	TokenCost int `json:"token_cost"`

	// Score is the ranking score used for prioritization
	Score float64 `json:"score"`
}

// InjectionPlan represents a complete plan for injecting behaviors
type InjectionPlan struct {
	// FullBehaviors are behaviors included at full detail
	FullBehaviors []InjectedBehavior `json:"full_behaviors"`

	// SummarizedBehaviors are behaviors included as summaries only
	SummarizedBehaviors []InjectedBehavior `json:"summarized_behaviors"`

	// OmittedBehaviors are behaviors referenced but not included
	OmittedBehaviors []InjectedBehavior `json:"omitted_behaviors"`

	// TotalTokens is the total token cost of the plan
	TotalTokens int `json:"total_tokens"`

	// TokenBudget is the budget this plan was optimized for
	TokenBudget int `json:"token_budget"`
}

// AllBehaviors returns all behaviors in the plan, regardless of tier
func (p *InjectionPlan) AllBehaviors() []InjectedBehavior {
	all := make([]InjectedBehavior, 0, len(p.FullBehaviors)+len(p.SummarizedBehaviors)+len(p.OmittedBehaviors))
	all = append(all, p.FullBehaviors...)
	all = append(all, p.SummarizedBehaviors...)
	all = append(all, p.OmittedBehaviors...)
	return all
}

// IncludedBehaviors returns all behaviors that will be injected (full + summarized)
func (p *InjectionPlan) IncludedBehaviors() []InjectedBehavior {
	included := make([]InjectedBehavior, 0, len(p.FullBehaviors)+len(p.SummarizedBehaviors))
	included = append(included, p.FullBehaviors...)
	included = append(included, p.SummarizedBehaviors...)
	return included
}

// BehaviorCount returns the total number of behaviors in the plan
func (p *InjectionPlan) BehaviorCount() int {
	return len(p.FullBehaviors) + len(p.SummarizedBehaviors) + len(p.OmittedBehaviors)
}

// IncludedCount returns the number of behaviors that will be injected
func (p *InjectionPlan) IncludedCount() int {
	return len(p.FullBehaviors) + len(p.SummarizedBehaviors)
}
