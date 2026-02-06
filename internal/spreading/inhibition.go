package spreading

import "sort"

// InhibitionConfig holds parameters for lateral inhibition.
// Lateral inhibition suppresses weakly activated nodes, focusing
// the activation pattern so that the strongest signals dominate.
type InhibitionConfig struct {
	// Strength (beta) controls how strongly winners suppress losers. Default: 0.15.
	// Higher values produce more aggressive suppression.
	Strength float64

	// Breadth (M) is the number of top nodes that compete in each round. Default: 7.
	// These top-M nodes suppress all nodes outside the top-M.
	Breadth int

	// Enabled controls whether inhibition is applied. Default: true.
	Enabled bool
}

// DefaultInhibitionConfig returns the default lateral inhibition config.
func DefaultInhibitionConfig() InhibitionConfig {
	return InhibitionConfig{
		Strength: 0.15,
		Breadth:  7,
		Enabled:  true,
	}
}

// ApplyInhibition performs lateral inhibition on the activation map.
// The top-M activated nodes suppress all other nodes based on their
// activation difference.
//
// Algorithm (from SYNAPSE):
//  1. Sort all nodes by activation descending
//  2. Select top-M nodes as "winners"
//  3. For each non-winner node:
//     suppression = beta * mean(winner_activation - node_activation)
//     node_activation = max(0, node_activation - suppression)
//
// This creates sharp contrast: winners stay bright, losers fade.
// ApplyInhibition is a pure function: it returns a new map without
// mutating the input.
func ApplyInhibition(activations map[string]float64, config InhibitionConfig) map[string]float64 {
	// Return a copy if inhibition is disabled or there is nothing to do.
	if !config.Enabled || len(activations) == 0 {
		return copyMap(activations)
	}

	// If there are fewer nodes than Breadth, every node is a winner
	// and no suppression occurs.
	if len(activations) <= config.Breadth {
		return copyMap(activations)
	}

	// Step 1: Collect and sort nodes by activation descending.
	type nodeAct struct {
		id  string
		act float64
	}
	nodes := make([]nodeAct, 0, len(activations))
	for id, act := range activations {
		nodes = append(nodes, nodeAct{id: id, act: act})
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].act > nodes[j].act
	})

	// Step 2: Identify winners (top-M).
	m := config.Breadth
	winners := nodes[:m]

	// Precompute the mean winner activation.
	var winnerSum float64
	for _, w := range winners {
		winnerSum += w.act
	}
	meanWinnerAct := winnerSum / float64(m)

	// Step 3: Build result map. Winners keep their activation;
	// losers are suppressed.
	result := make(map[string]float64, len(activations))
	for _, w := range winners {
		result[w.id] = w.act
	}

	losers := nodes[m:]
	for _, loser := range losers {
		// suppression = beta * mean(winner_act_i - loser_act) for all winners
		// = beta * (meanWinnerAct - loser_act)
		// This is always >= 0 because winners have higher activation.
		suppression := config.Strength * (meanWinnerAct - loser.act)
		suppressed := loser.act - suppression
		if suppressed < 0 {
			suppressed = 0
		}
		result[loser.id] = suppressed
	}

	return result
}

// copyMap returns a shallow copy of the activation map.
func copyMap(m map[string]float64) map[string]float64 {
	if m == nil {
		return nil
	}
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
