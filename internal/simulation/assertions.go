package simulation

import (
	"math"
	"testing"
)

// AssertWeightConverges asserts that a specific edge weight settles within
// [min, max] after a given session index.
func AssertWeightConverges(t *testing.T, result SimulationResult, src, tgt, kind string, min, max float64, afterSession int) {
	t.Helper()
	key := EdgeKey(src, tgt, kind)
	for i := afterSession; i < len(result.Sessions); i++ {
		w, ok := result.Sessions[i].EdgeWeights[key]
		if !ok {
			t.Errorf("AssertWeightConverges: session %d: edge %s not found", i, key)
			continue
		}
		if w < min || w > max {
			t.Errorf("AssertWeightConverges: session %d: edge %s weight %.6f not in [%.4f, %.4f]", i, key, w, min, max)
		}
	}
}

// AssertNoWeightExplosion asserts that no edge weight exceeds maxWeight
// in any session.
func AssertNoWeightExplosion(t *testing.T, result SimulationResult, maxWeight float64) {
	t.Helper()
	for _, sr := range result.Sessions {
		for key, w := range sr.EdgeWeights {
			if w > maxWeight {
				t.Errorf("AssertNoWeightExplosion: session %d: edge %s weight %.6f > max %.4f", sr.Index, key, w, maxWeight)
			}
		}
	}
}

// AssertNoActivationCollapse asserts that at least minActive behaviors
// have activation above threshold in each session after afterSession.
func AssertNoActivationCollapse(t *testing.T, result SimulationResult, threshold float64, minActive int, afterSession int) {
	t.Helper()
	for i := afterSession; i < len(result.Sessions); i++ {
		sr := result.Sessions[i]
		active := 0
		for _, r := range sr.Results {
			if r.Activation >= threshold {
				active++
			}
		}
		if active < minActive {
			t.Errorf("AssertNoActivationCollapse: session %d: only %d behaviors above %.4f (need %d)", sr.Index, active, threshold, minActive)
		}
	}
}

// AssertBehaviorSurfaces asserts that a specific behavior appears in results
// with activation above threshold in at least one session.
func AssertBehaviorSurfaces(t *testing.T, result SimulationResult, behaviorID string, threshold float64) {
	t.Helper()
	for _, sr := range result.Sessions {
		for _, r := range sr.Results {
			if r.BehaviorID == behaviorID && r.Activation >= threshold {
				return // Found it.
			}
		}
	}
	t.Errorf("AssertBehaviorSurfaces: behavior %s never appeared above %.4f in any session", behaviorID, threshold)
}

// AssertBehaviorSurfacesInFraction asserts that a behavior appears in at least
// the given fraction of sessions (e.g., 0.3 = 30%).
func AssertBehaviorSurfacesInFraction(t *testing.T, result SimulationResult, behaviorID string, threshold float64, minFraction float64) {
	t.Helper()
	count := 0
	for _, sr := range result.Sessions {
		for _, r := range sr.Results {
			if r.BehaviorID == behaviorID && r.Activation >= threshold {
				count++
				break
			}
		}
	}
	fraction := float64(count) / float64(len(result.Sessions))
	if fraction < minFraction {
		t.Errorf("AssertBehaviorSurfacesInFraction: behavior %s appeared in %.1f%% sessions (need %.1f%%)", behaviorID, fraction*100, minFraction*100)
	}
}

// AssertDiverseCoActivation asserts that co-activation pairs span at least
// minUnique distinct behaviors across all sessions.
func AssertDiverseCoActivation(t *testing.T, result SimulationResult, minUnique int) {
	t.Helper()
	unique := make(map[string]bool)
	for _, sr := range result.Sessions {
		for _, pair := range sr.Pairs {
			unique[pair.BehaviorA] = true
			unique[pair.BehaviorB] = true
		}
	}
	if len(unique) < minUnique {
		t.Errorf("AssertDiverseCoActivation: only %d unique behaviors in co-activation pairs (need %d)", len(unique), minUnique)
	}
}

// AssertEdgeCreated asserts that an edge exists between two behaviors in the
// final session's edge weight snapshot.
func AssertEdgeCreated(t *testing.T, result SimulationResult, behaviorA, behaviorB string) {
	t.Helper()
	if len(result.Sessions) == 0 {
		t.Fatal("AssertEdgeCreated: no sessions")
	}
	last := result.Sessions[len(result.Sessions)-1]
	// Check both directions since edges are directed.
	keyAB := EdgeKey(behaviorA, behaviorB, "co-activated")
	keyBA := EdgeKey(behaviorB, behaviorA, "co-activated")
	_, okAB := last.EdgeWeights[keyAB]
	_, okBA := last.EdgeWeights[keyBA]
	if !okAB && !okBA {
		t.Errorf("AssertEdgeCreated: no co-activated edge between %s and %s in final session", behaviorA, behaviorB)
	}
}

// AssertEdgeNotCreated asserts that no co-activated edge exists between two
// behaviors in the final session.
func AssertEdgeNotCreated(t *testing.T, result SimulationResult, behaviorA, behaviorB string) {
	t.Helper()
	if len(result.Sessions) == 0 {
		t.Fatal("AssertEdgeNotCreated: no sessions")
	}
	last := result.Sessions[len(result.Sessions)-1]
	keyAB := EdgeKey(behaviorA, behaviorB, "co-activated")
	keyBA := EdgeKey(behaviorB, behaviorA, "co-activated")
	_, okAB := last.EdgeWeights[keyAB]
	_, okBA := last.EdgeWeights[keyBA]
	if okAB || okBA {
		t.Errorf("AssertEdgeNotCreated: unexpected co-activated edge between %s and %s", behaviorA, behaviorB)
	}
}

// AssertWeightBounded asserts that all edge weights in all sessions fall
// within [min, max].
func AssertWeightBounded(t *testing.T, result SimulationResult, min, max float64) {
	t.Helper()
	for _, sr := range result.Sessions {
		for key, w := range sr.EdgeWeights {
			if w < min || w > max {
				t.Errorf("AssertWeightBounded: session %d: edge %s weight %.6f not in [%.4f, %.4f]", sr.Index, key, w, min, max)
			}
		}
	}
}

// AssertWeightStable asserts that the variance of a specific edge weight
// over the last N sessions is below maxVariance.
func AssertWeightStable(t *testing.T, result SimulationResult, src, tgt, kind string, maxVariance float64, lastNSessions int) {
	t.Helper()
	key := EdgeKey(src, tgt, kind)

	start := len(result.Sessions) - lastNSessions
	if start < 0 {
		start = 0
	}

	var weights []float64
	for i := start; i < len(result.Sessions); i++ {
		if w, ok := result.Sessions[i].EdgeWeights[key]; ok {
			weights = append(weights, w)
		}
	}

	if len(weights) < 2 {
		t.Errorf("AssertWeightStable: edge %s found in only %d of last %d sessions", key, len(weights), lastNSessions)
		return
	}

	v := variance(weights)
	if v > maxVariance {
		t.Errorf("AssertWeightStable: edge %s variance %.6f > max %.6f over last %d sessions (weights: %v)", key, v, maxVariance, lastNSessions, weights)
	}
}

// AssertWeightIncreased asserts that a specific edge weight is higher in a
// later session than in an earlier session.
func AssertWeightIncreased(t *testing.T, result SimulationResult, src, tgt, kind string, fromSession, toSession int) {
	t.Helper()
	key := EdgeKey(src, tgt, kind)
	wFrom, okFrom := result.Sessions[fromSession].EdgeWeights[key]
	wTo, okTo := result.Sessions[toSession].EdgeWeights[key]
	if !okFrom {
		t.Errorf("AssertWeightIncreased: edge %s not found in session %d", key, fromSession)
		return
	}
	if !okTo {
		t.Errorf("AssertWeightIncreased: edge %s not found in session %d", key, toSession)
		return
	}
	if wTo <= wFrom {
		t.Errorf("AssertWeightIncreased: edge %s weight did not increase: session %d=%.6f, session %d=%.6f", key, fromSession, wFrom, toSession, wTo)
	}
}

// AssertWeightDecreased asserts that a specific edge weight is lower in a
// later session than in an earlier session.
func AssertWeightDecreased(t *testing.T, result SimulationResult, src, tgt, kind string, fromSession, toSession int) {
	t.Helper()
	key := EdgeKey(src, tgt, kind)
	wFrom, okFrom := result.Sessions[fromSession].EdgeWeights[key]
	wTo, okTo := result.Sessions[toSession].EdgeWeights[key]
	if !okFrom {
		t.Errorf("AssertWeightDecreased: edge %s not found in session %d", key, fromSession)
		return
	}
	if !okTo {
		t.Errorf("AssertWeightDecreased: edge %s not found in session %d", key, toSession)
		return
	}
	if wTo >= wFrom {
		t.Errorf("AssertWeightDecreased: edge %s weight did not decrease: session %d=%.6f, session %d=%.6f", key, fromSession, wFrom, toSession, wTo)
	}
}

// AssertResultsNotEmpty asserts that every session produced at least one result.
func AssertResultsNotEmpty(t *testing.T, result SimulationResult) {
	t.Helper()
	for _, sr := range result.Sessions {
		if len(sr.Results) == 0 {
			t.Errorf("AssertResultsNotEmpty: session %d produced no results", sr.Index)
		}
	}
}

// variance computes the population variance of a float64 slice.
func variance(vals []float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	mean := 0.0
	for _, v := range vals {
		mean += v
	}
	mean /= float64(len(vals))

	sum := 0.0
	for _, v := range vals {
		d := v - mean
		sum += d * d
	}
	return sum / float64(len(vals))
}

// CountSessionsWithBehavior counts how many sessions include the given
// behavior with activation above threshold.
func CountSessionsWithBehavior(result SimulationResult, behaviorID string, threshold float64) int {
	count := 0
	for _, sr := range result.Sessions {
		for _, r := range sr.Results {
			if r.BehaviorID == behaviorID && r.Activation >= threshold {
				count++
				break
			}
		}
	}
	return count
}

// CountUniqueEdges counts distinct edge keys across all sessions.
func CountUniqueEdges(result SimulationResult) int {
	unique := make(map[string]bool)
	for _, sr := range result.Sessions {
		for key := range sr.EdgeWeights {
			unique[key] = true
		}
	}
	return len(unique)
}

// MaxWeight returns the maximum weight for a specific edge across all sessions.
func MaxWeight(result SimulationResult, src, tgt, kind string) float64 {
	key := EdgeKey(src, tgt, kind)
	max := math.Inf(-1)
	for _, sr := range result.Sessions {
		if w, ok := sr.EdgeWeights[key]; ok && w > max {
			max = w
		}
	}
	return max
}
