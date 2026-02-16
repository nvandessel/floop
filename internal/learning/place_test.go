package learning

import (
	"context"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

func TestNewGraphPlacer(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	placer := NewGraphPlacer(s)

	if placer == nil {
		t.Error("NewGraphPlacer() returned nil")
	}
}

func TestGraphPlacer_Place_EmptyStore(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	placer := NewGraphPlacer(s)
	ctx := context.Background()

	behavior := &models.Behavior{
		ID:   "behavior-1",
		Name: "test-behavior",
		Kind: models.BehaviorKindDirective,
		When: map[string]interface{}{
			"language": "go",
		},
		Content: models.BehaviorContent{
			Canonical: "Use table-driven tests",
		},
	}

	decision, err := placer.Place(ctx, behavior)
	if err != nil {
		t.Errorf("Place() error = %v", err)
		return
	}

	if decision.Action != "create" {
		t.Errorf("Place() Action = %v, want create", decision.Action)
	}
	if decision.Confidence < 0.8 {
		t.Errorf("Place() Confidence = %v, want >= 0.8 for empty store", decision.Confidence)
	}
	if len(decision.SimilarBehaviors) != 0 {
		t.Errorf("Place() SimilarBehaviors = %v, want empty", decision.SimilarBehaviors)
	}
}

func TestGraphPlacer_Place_HighSimilarity_SuggestsMerge(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	placer := NewGraphPlacer(s)
	ctx := context.Background()

	// Add an existing behavior
	existingNode := store.Node{
		ID:   "existing-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"kind": "directive",
			"name": "use-table-tests",
			"when": map[string]interface{}{
				"language": "go",
			},
			"content": map[string]interface{}{
				"canonical": "Use table-driven tests in Go",
			},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.8,
		},
	}
	s.AddNode(ctx, existingNode)

	// Try to add a very similar behavior
	newBehavior := &models.Behavior{
		ID:   "behavior-new",
		Name: "use-table-tests-new",
		Kind: models.BehaviorKindDirective,
		When: map[string]interface{}{
			"language": "go",
		},
		Content: models.BehaviorContent{
			Canonical: "Use table-driven tests in Go",
		},
	}

	decision, err := placer.Place(ctx, newBehavior)
	if err != nil {
		t.Errorf("Place() error = %v", err)
		return
	}

	if decision.Action != "merge" {
		t.Errorf("Place() Action = %v, want merge for high similarity", decision.Action)
	}
	if decision.TargetID != "existing-1" {
		t.Errorf("Place() TargetID = %v, want existing-1", decision.TargetID)
	}
	if len(decision.SimilarBehaviors) == 0 {
		t.Error("Place() SimilarBehaviors should not be empty")
	}
}

func TestGraphPlacer_Place_Specialize(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	placer := NewGraphPlacer(s)
	ctx := context.Background()

	// Add a general behavior
	generalNode := store.Node{
		ID:   "general-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"kind": "directive",
			"name": "use-fmt",
			"when": map[string]interface{}{
				"language": "go",
			},
			"content": map[string]interface{}{
				"canonical": "Format code with gofmt",
			},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.8,
		},
	}
	s.AddNode(ctx, generalNode)

	// Add a more specific behavior (same language + specific task)
	specificBehavior := &models.Behavior{
		ID:   "specific-1",
		Name: "use-fmt-tests",
		Kind: models.BehaviorKindDirective,
		When: map[string]interface{}{
			"language": "go",
			"task":     "testing",
		},
		Content: models.BehaviorContent{
			Canonical: "Format test code with gofmt",
		},
	}

	decision, err := placer.Place(ctx, specificBehavior)
	if err != nil {
		t.Errorf("Place() error = %v", err)
		return
	}

	// Should propose overrides edge since the new behavior is more specific
	hasOverridesEdge := false
	for _, edge := range decision.ProposedEdges {
		if edge.Kind == "overrides" && edge.From == "specific-1" && edge.To == "general-1" {
			hasOverridesEdge = true
			break
		}
	}
	if !hasOverridesEdge {
		t.Errorf("Place() should propose 'overrides' edge, got edges: %+v", decision.ProposedEdges)
	}
}

func TestGraphPlacer_Place_NoOverlapDifferentLanguage(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	placer := NewGraphPlacer(s)
	ctx := context.Background()

	// Add a Python behavior
	pythonNode := store.Node{
		ID:   "python-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"kind": "directive",
			"name": "use-black",
			"when": map[string]interface{}{
				"language": "python",
			},
			"content": map[string]interface{}{
				"canonical": "Format Python code with black",
			},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.8,
		},
	}
	s.AddNode(ctx, pythonNode)

	// Add a Go behavior (different language, no overlap)
	goBehavior := &models.Behavior{
		ID:   "go-1",
		Name: "use-gofmt",
		Kind: models.BehaviorKindDirective,
		When: map[string]interface{}{
			"language": "go",
		},
		Content: models.BehaviorContent{
			Canonical: "Format Go code with gofmt",
		},
	}

	decision, err := placer.Place(ctx, goBehavior)
	if err != nil {
		t.Errorf("Place() error = %v", err)
		return
	}

	if decision.Action != "create" {
		t.Errorf("Place() Action = %v, want create for non-overlapping behaviors", decision.Action)
	}
	if len(decision.SimilarBehaviors) != 0 {
		t.Errorf("Place() SimilarBehaviors = %v, want empty for different languages", decision.SimilarBehaviors)
	}
}

func TestGraphPlacer_computeSimilarity(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	gp := NewGraphPlacer(s).(*graphPlacer)

	tests := []struct {
		name    string
		a       *models.Behavior
		b       *models.Behavior
		wantMin float64
		wantMax float64
	}{
		{
			name: "identical behaviors",
			a: &models.Behavior{
				When: map[string]interface{}{"language": "go"},
				Content: models.BehaviorContent{
					Canonical: "Use table-driven tests",
				},
			},
			b: &models.Behavior{
				When: map[string]interface{}{"language": "go"},
				Content: models.BehaviorContent{
					Canonical: "Use table-driven tests",
				},
			},
			wantMin: 0.9,
			wantMax: 1.0,
		},
		{
			name: "similar content different when",
			a: &models.Behavior{
				When: map[string]interface{}{"language": "go"},
				Content: models.BehaviorContent{
					Canonical: "Use table-driven tests",
				},
			},
			b: &models.Behavior{
				When: map[string]interface{}{"language": "python"},
				Content: models.BehaviorContent{
					Canonical: "Use table-driven tests",
				},
			},
			wantMin: 0.5,
			wantMax: 0.7,
		},
		{
			name: "completely different",
			a: &models.Behavior{
				When: map[string]interface{}{"language": "go"},
				Content: models.BehaviorContent{
					Canonical: "Use gofmt for formatting",
				},
			},
			b: &models.Behavior{
				When: map[string]interface{}{"language": "python"},
				Content: models.BehaviorContent{
					Canonical: "Use black for code style",
				},
			},
			wantMin: 0.0,
			wantMax: 0.3,
		},
		{
			name: "empty when conditions",
			a: &models.Behavior{
				When: map[string]interface{}{},
				Content: models.BehaviorContent{
					Canonical: "Always write tests",
				},
			},
			b: &models.Behavior{
				When: map[string]interface{}{},
				Content: models.BehaviorContent{
					Canonical: "Always write tests",
				},
			},
			wantMin: 0.9,
			wantMax: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gp.computeSimilarity(context.Background(), tt.a, tt.b)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("computeSimilarity() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestGraphPlacer_isMoreSpecific(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	gp := NewGraphPlacer(s).(*graphPlacer)

	tests := []struct {
		name string
		a    map[string]interface{}
		b    map[string]interface{}
		want bool
	}{
		{
			name: "a is more specific",
			a: map[string]interface{}{
				"language": "go",
				"task":     "testing",
			},
			b: map[string]interface{}{
				"language": "go",
			},
			want: true,
		},
		{
			name: "b is more specific",
			a: map[string]interface{}{
				"language": "go",
			},
			b: map[string]interface{}{
				"language": "go",
				"task":     "testing",
			},
			want: false,
		},
		{
			name: "same specificity",
			a: map[string]interface{}{
				"language": "go",
			},
			b: map[string]interface{}{
				"language": "go",
			},
			want: false,
		},
		{
			name: "different keys",
			a: map[string]interface{}{
				"language": "go",
				"env":      "prod",
			},
			b: map[string]interface{}{
				"task": "testing",
			},
			want: false,
		},
		{
			name: "a has all of b plus more",
			a: map[string]interface{}{
				"language": "go",
				"task":     "testing",
				"file":     "*.go",
			},
			b: map[string]interface{}{
				"language": "go",
				"task":     "testing",
			},
			want: true,
		},
		{
			name: "empty b (unscoped, not less specific)",
			a: map[string]interface{}{
				"language": "go",
			},
			b:    map[string]interface{}{},
			want: false,
		},
		{
			name: "both empty",
			a:    map[string]interface{}{},
			b:    map[string]interface{}{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gp.isMoreSpecific(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("isMoreSpecific() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsMoreSpecific_EmptyWhen(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	placer := NewGraphPlacer(s).(*graphPlacer)

	// A scoped behavior should NOT be considered "more specific" than
	// an unscoped behavior. Empty when means "unscoped", not "less specific".
	got := placer.isMoreSpecific(
		map[string]interface{}{"task": "dev"},
		map[string]interface{}{},
	)
	if got {
		t.Errorf("isMoreSpecific({task:dev}, {}) = true, want false; empty when is unscoped, not less specific")
	}
}

func TestGraphPlacer_hasOverlappingConditions(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	gp := NewGraphPlacer(s).(*graphPlacer)

	tests := []struct {
		name    string
		when    map[string]interface{}
		content map[string]interface{}
		want    bool
	}{
		{
			name: "matching language",
			when: map[string]interface{}{
				"language": "go",
			},
			content: map[string]interface{}{
				"when": map[string]interface{}{
					"language": "go",
				},
			},
			want: true,
		},
		{
			name: "different language",
			when: map[string]interface{}{
				"language": "go",
			},
			content: map[string]interface{}{
				"when": map[string]interface{}{
					"language": "python",
				},
			},
			want: false,
		},
		{
			name: "no when in content",
			when: map[string]interface{}{
				"language": "go",
			},
			content: map[string]interface{}{
				"name": "test",
			},
			want: false,
		},
		{
			name: "both empty when",
			when: map[string]interface{}{},
			content: map[string]interface{}{
				"when": map[string]interface{}{},
			},
			want: false,
		},
		{
			name: "partial overlap",
			when: map[string]interface{}{
				"language": "go",
				"task":     "testing",
			},
			content: map[string]interface{}{
				"when": map[string]interface{}{
					"language": "go",
					"task":     "refactor",
				},
			},
			want: true, // language matches
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gp.hasOverlappingConditions(tt.when, tt.content)
			if got != tt.want {
				t.Errorf("hasOverlappingConditions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNodeToBehavior(t *testing.T) {
	node := store.Node{
		ID:   "test-id",
		Kind: "behavior",
		Content: map[string]interface{}{
			"kind": "directive",
			"name": "test-name",
			"when": map[string]interface{}{
				"language": "go",
			},
			"content": map[string]interface{}{
				"canonical": "Test canonical content",
				"expanded":  "Test expanded content",
			},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.85,
			"priority":   5,
		},
	}

	b := models.NodeToBehavior(node)

	if b.ID != "test-id" {
		t.Errorf("nodeToBehavior() ID = %v, want test-id", b.ID)
	}
	if b.Kind != models.BehaviorKindDirective {
		t.Errorf("nodeToBehavior() Kind = %v, want directive", b.Kind)
	}
	if b.Name != "test-name" {
		t.Errorf("nodeToBehavior() Name = %v, want test-name", b.Name)
	}
	if b.When["language"] != "go" {
		t.Errorf("nodeToBehavior() When[language] = %v, want go", b.When["language"])
	}
	if b.Content.Canonical != "Test canonical content" {
		t.Errorf("nodeToBehavior() Content.Canonical = %v, want Test canonical content", b.Content.Canonical)
	}
	if b.Confidence != 0.85 {
		t.Errorf("nodeToBehavior() Confidence = %v, want 0.85", b.Confidence)
	}
}

func TestNodeToBehavior_StringCreatedAt(t *testing.T) {
	now := "2026-02-06T10:30:00Z"
	node := store.Node{
		ID:   "str-ts-test",
		Kind: "behavior",
		Content: map[string]interface{}{
			"kind": "directive",
			"name": "timestamp-test",
			"content": map[string]interface{}{
				"canonical": "Test string timestamp parsing",
			},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.7,
			"provenance": map[string]interface{}{
				"source_type": "correction",
				"created_at":  now, // string, not time.Time
			},
		},
	}

	b := models.NodeToBehavior(node)

	if b.Provenance.CreatedAt.IsZero() {
		t.Error("NodeToBehavior() CreatedAt is zero, want parsed time from string")
	}
	if b.Provenance.CreatedAt.Year() != 2026 {
		t.Errorf("NodeToBehavior() CreatedAt year = %d, want 2026", b.Provenance.CreatedAt.Year())
	}
}

func TestNodeToBehavior_Tags(t *testing.T) {
	node := store.Node{
		ID:   "tag-test",
		Kind: "behavior",
		Content: map[string]interface{}{
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "use git worktree",
				"tags":      []interface{}{"git", "worktree"},
			},
		},
		Metadata: map[string]interface{}{},
	}

	b := models.NodeToBehavior(node)

	if len(b.Content.Tags) != 2 {
		t.Fatalf("NodeToBehavior() Tags len = %d, want 2", len(b.Content.Tags))
	}
	if b.Content.Tags[0] != "git" {
		t.Errorf("Tags[0] = %q, want %q", b.Content.Tags[0], "git")
	}
	if b.Content.Tags[1] != "worktree" {
		t.Errorf("Tags[1] = %q, want %q", b.Content.Tags[1], "worktree")
	}
}

func TestNodeToBehavior_NoTags(t *testing.T) {
	node := store.Node{
		ID:   "no-tag-test",
		Kind: "behavior",
		Content: map[string]interface{}{
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "do something",
			},
		},
		Metadata: map[string]interface{}{},
	}

	b := models.NodeToBehavior(node)

	if len(b.Content.Tags) != 0 {
		t.Errorf("NodeToBehavior() Tags = %v, want empty", b.Content.Tags)
	}
}

func TestNodeToBehavior_StatsTimeFields(t *testing.T) {
	createdAt := "2026-01-15T08:00:00Z"
	updatedAt := "2026-02-10T14:30:00Z"
	lastActivated := "2026-02-10T14:00:00Z"
	lastConfirmed := "2026-02-09T12:00:00Z"

	node := store.Node{
		ID:   "stats-time-test",
		Kind: "behavior",
		Content: map[string]interface{}{
			"kind": "directive",
			"name": "time-test",
			"content": map[string]interface{}{
				"canonical": "Test stats time extraction",
			},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.8,
			"stats": map[string]interface{}{
				"times_activated":  10,
				"times_followed":   3,
				"times_confirmed":  2,
				"times_overridden": 1,
				"created_at":       createdAt,
				"updated_at":       updatedAt,
				"last_activated":   lastActivated,
				"last_confirmed":   lastConfirmed,
			},
		},
	}

	b := models.NodeToBehavior(node)

	// Counter fields
	if b.Stats.TimesActivated != 10 {
		t.Errorf("TimesActivated = %d, want 10", b.Stats.TimesActivated)
	}
	if b.Stats.TimesFollowed != 3 {
		t.Errorf("TimesFollowed = %d, want 3", b.Stats.TimesFollowed)
	}
	if b.Stats.TimesConfirmed != 2 {
		t.Errorf("TimesConfirmed = %d, want 2", b.Stats.TimesConfirmed)
	}
	if b.Stats.TimesOverridden != 1 {
		t.Errorf("TimesOverridden = %d, want 1", b.Stats.TimesOverridden)
	}

	// Time fields
	if b.Stats.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero, want parsed time")
	}
	if b.Stats.CreatedAt.Year() != 2026 || b.Stats.CreatedAt.Month() != 1 {
		t.Errorf("CreatedAt = %v, want 2026-01-15", b.Stats.CreatedAt)
	}

	if b.Stats.UpdatedAt.IsZero() {
		t.Error("UpdatedAt is zero, want parsed time")
	}
	if b.Stats.UpdatedAt.Month() != 2 || b.Stats.UpdatedAt.Day() != 10 {
		t.Errorf("UpdatedAt = %v, want 2026-02-10", b.Stats.UpdatedAt)
	}

	if b.Stats.LastActivated == nil {
		t.Fatal("LastActivated is nil, want parsed time")
	}
	if b.Stats.LastActivated.Hour() != 14 {
		t.Errorf("LastActivated hour = %d, want 14", b.Stats.LastActivated.Hour())
	}

	if b.Stats.LastConfirmed == nil {
		t.Fatal("LastConfirmed is nil, want parsed time")
	}
	if b.Stats.LastConfirmed.Day() != 9 {
		t.Errorf("LastConfirmed day = %d, want 9", b.Stats.LastConfirmed.Day())
	}
}

func TestNodeToBehavior_StatsNoTimeFields(t *testing.T) {
	node := store.Node{
		ID:   "stats-no-time",
		Kind: "behavior",
		Content: map[string]interface{}{
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "No time fields in stats",
			},
		},
		Metadata: map[string]interface{}{
			"stats": map[string]interface{}{
				"times_activated": 5,
			},
		},
	}

	b := models.NodeToBehavior(node)

	if b.Stats.TimesActivated != 5 {
		t.Errorf("TimesActivated = %d, want 5", b.Stats.TimesActivated)
	}
	// Time fields should be zero-valued, not cause a panic
	if !b.Stats.CreatedAt.IsZero() {
		t.Errorf("CreatedAt should be zero, got %v", b.Stats.CreatedAt)
	}
	if b.Stats.LastActivated != nil {
		t.Errorf("LastActivated should be nil, got %v", b.Stats.LastActivated)
	}
}

func TestGraphPlacer_determineEdges(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	gp := NewGraphPlacer(s).(*graphPlacer)

	tests := []struct {
		name          string
		behavior      *models.Behavior
		existing      []models.Behavior
		wantEdgeKinds []string
	}{
		{
			name: "new behavior overrides general",
			behavior: &models.Behavior{
				ID: "specific-1",
				When: map[string]interface{}{
					"language": "go",
					"task":     "testing",
				},
				Content: models.BehaviorContent{
					Canonical: "Use table tests for Go testing",
				},
			},
			existing: []models.Behavior{
				{
					ID: "general-1",
					When: map[string]interface{}{
						"language": "go",
					},
					Content: models.BehaviorContent{
						Canonical: "Use table tests for Go",
					},
				},
			},
			wantEdgeKinds: []string{"overrides", "similar-to"},
		},
		{
			name: "no edges for unrelated behaviors",
			behavior: &models.Behavior{
				ID: "go-1",
				When: map[string]interface{}{
					"language": "go",
				},
				Content: models.BehaviorContent{
					Canonical: "Use gofmt",
				},
			},
			existing: []models.Behavior{
				{
					ID: "python-1",
					When: map[string]interface{}{
						"language": "python",
					},
					Content: models.BehaviorContent{
						Canonical: "Use black",
					},
				},
			},
			wantEdgeKinds: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges := gp.determineEdges(context.Background(), tt.behavior, tt.existing)

			gotKinds := make(map[string]bool)
			for _, e := range edges {
				gotKinds[e.Kind] = true
			}

			for _, wantKind := range tt.wantEdgeKinds {
				if !gotKinds[wantKind] {
					t.Errorf("determineEdges() missing edge kind %s, got kinds: %v", wantKind, gotKinds)
				}
			}
		})
	}
}

func TestGraphPlacer_Place_MultipleSimilarBehaviors(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	placer := NewGraphPlacer(s)
	ctx := context.Background()

	// Add multiple existing behaviors with varying similarity
	nodes := []store.Node{
		{
			ID:   "behavior-1",
			Kind: "behavior",
			Content: map[string]interface{}{
				"kind": "directive",
				"when": map[string]interface{}{
					"language": "go",
				},
				"content": map[string]interface{}{
					"canonical": "Use interfaces for abstraction",
				},
			},
			Metadata: map[string]interface{}{},
		},
		{
			ID:   "behavior-2",
			Kind: "behavior",
			Content: map[string]interface{}{
				"kind": "directive",
				"when": map[string]interface{}{
					"language": "go",
				},
				"content": map[string]interface{}{
					"canonical": "Prefer composition over inheritance",
				},
			},
			Metadata: map[string]interface{}{},
		},
		{
			ID:   "behavior-3",
			Kind: "behavior",
			Content: map[string]interface{}{
				"kind": "directive",
				"when": map[string]interface{}{
					"language": "go",
				},
				"content": map[string]interface{}{
					"canonical": "Use interfaces for abstraction in Go code",
				},
			},
			Metadata: map[string]interface{}{},
		},
	}

	for _, n := range nodes {
		s.AddNode(ctx, n)
	}

	// New behavior similar to behavior-1 and behavior-3
	newBehavior := &models.Behavior{
		ID:   "new-behavior",
		Kind: models.BehaviorKindDirective,
		When: map[string]interface{}{
			"language": "go",
		},
		Content: models.BehaviorContent{
			Canonical: "Use interfaces for abstraction",
		},
	}

	decision, err := placer.Place(ctx, newBehavior)
	if err != nil {
		t.Errorf("Place() error = %v", err)
		return
	}

	// Should find similar behaviors
	if len(decision.SimilarBehaviors) == 0 {
		t.Error("Place() should find similar behaviors")
	}

	// Should suggest merge due to high similarity with behavior-1
	if decision.Action != "merge" {
		t.Errorf("Place() Action = %v, want merge", decision.Action)
	}
}
