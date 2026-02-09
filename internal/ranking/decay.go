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

// DefaultDecayRate is the default temporal decay rate for edge weights.
// At 0.01, edges lose ~1% of effective weight per hour, ~21% per day.
const DefaultDecayRate = 0.01

// EdgeDecay calculates the effective weight of an edge based on temporal decay.
// Returns weight * e^(-rho * elapsed_hours) where rho is the decay rate.
// A rho of 0.01 means ~1% decay per hour, ~21% decay per day.
// If lastActivated is zero, the full weight is returned (no decay applied).
func EdgeDecay(weight float64, lastActivated time.Time, rho float64) float64 {
	if weight == 0 {
		return 0
	}
	if lastActivated.IsZero() {
		return weight
	}

	elapsed := time.Since(lastActivated).Hours()
	if elapsed <= 0 {
		return weight
	}

	return weight * math.Exp(-rho*elapsed)
}
