package ranking

import (
	"math"
	"time"
)

// ExponentialDecay calculates a decay score based on time elapsed
// Returns a value between 0 and 1, where 1 means the time is now
// and 0 approaches as time goes to infinity.
// The halfLife parameter determines how quickly the score decays.
func ExponentialDecay(t time.Time, halfLife time.Duration) float64 {
	if t.IsZero() {
		return 0.0
	}

	elapsed := time.Since(t)
	if elapsed <= 0 {
		return 1.0
	}

	// Using natural decay: score = e^(-lambda * t)
	// where lambda = ln(2) / halfLife
	lambda := math.Ln2 / float64(halfLife)
	score := math.Exp(-lambda * float64(elapsed))

	return score
}

// LinearDecay calculates a linear decay score based on time elapsed
// Returns a value between 0 and 1, where 1 means the time is now
// and 0 means the maxAge has been reached or exceeded.
func LinearDecay(t time.Time, maxAge time.Duration) float64 {
	if t.IsZero() {
		return 0.0
	}

	elapsed := time.Since(t)
	if elapsed <= 0 {
		return 1.0
	}

	if elapsed >= maxAge {
		return 0.0
	}

	return 1.0 - float64(elapsed)/float64(maxAge)
}

// StepDecay returns discrete decay levels based on time thresholds
// Returns 1.0 for recent, 0.5 for medium, 0.25 for old, 0.0 for very old
func StepDecay(t time.Time, recent, medium, old time.Duration) float64 {
	if t.IsZero() {
		return 0.0
	}

	elapsed := time.Since(t)
	if elapsed <= 0 {
		return 1.0
	}

	switch {
	case elapsed < recent:
		return 1.0
	case elapsed < medium:
		return 0.75
	case elapsed < old:
		return 0.5
	default:
		return 0.25
	}
}

// BoostedDecay combines exponential decay with a minimum floor
// Ensures behaviors never decay below minScore, preserving long-term memory
func BoostedDecay(t time.Time, halfLife time.Duration, minScore float64) float64 {
	base := ExponentialDecay(t, halfLife)

	// Apply floor
	if base < minScore {
		return minScore
	}
	return base
}
