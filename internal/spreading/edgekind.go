package spreading

import "github.com/nvandessel/floop/internal/store"

// Sproink edge kind constants matching sproink/src/graph.rs EdgeKind enum.
const (
	sproinkEdgePositive               uint8 = 0
	sproinkEdgeConflicts              uint8 = 1
	sproinkEdgeDirectionalSuppressive uint8 = 2
	sproinkEdgeFeatureAffinity        uint8 = 4
)

// edgeKindToU8 maps a floop EdgeKind to the corresponding sproink uint8 value.
func edgeKindToU8(kind store.EdgeKind) uint8 {
	switch kind {
	case store.EdgeKindConflicts:
		return sproinkEdgeConflicts
	case store.EdgeKindOverrides, store.EdgeKindDeprecatedTo, store.EdgeKindMergedInto:
		return sproinkEdgeDirectionalSuppressive
	case store.EdgeKind(edgeKindFeatureAffinity):
		return sproinkEdgeFeatureAffinity
	default:
		return sproinkEdgePositive
	}
}
