package spreading

import (
	"testing"

	"github.com/nvandessel/floop/internal/store"
)

func TestEdgeKindToU8(t *testing.T) {
	tests := []struct {
		name string
		kind store.EdgeKind
		want uint8
	}{
		// Positive (default) — everything else maps to 0
		{"requires", store.EdgeKindRequires, sproinkEdgePositive},
		{"similar-to", store.EdgeKindSimilarTo, sproinkEdgePositive},
		{"learned-from", store.EdgeKindLearnedFrom, sproinkEdgePositive},
		{"co-activated", store.EdgeKindCoActivated, sproinkEdgePositive},

		// Conflicts → 1
		{"conflicts", store.EdgeKindConflicts, sproinkEdgeConflicts},

		// Directional suppressive → 2
		{"overrides", store.EdgeKindOverrides, sproinkEdgeDirectionalSuppressive},
		{"deprecated-to", store.EdgeKindDeprecatedTo, sproinkEdgeDirectionalSuppressive},
		{"merged-into", store.EdgeKindMergedInto, sproinkEdgeDirectionalSuppressive},

		// Feature affinity → 4 (string constant, not store.EdgeKind)
		{"feature-affinity", store.EdgeKind(edgeKindFeatureAffinity), sproinkEdgeFeatureAffinity},

		// Unknown kind → 0 (positive default)
		{"unknown", store.EdgeKind("something-unknown"), sproinkEdgePositive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := edgeKindToU8(tt.kind)
			if got != tt.want {
				t.Errorf("edgeKindToU8(%q) = %d, want %d", tt.kind, got, tt.want)
			}
		})
	}
}
