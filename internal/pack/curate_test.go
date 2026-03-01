package pack

import (
	"context"
	"strings"
	"testing"

	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

func TestAddToPack(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*store.InMemoryGraphStore)
		behavior  string
		packID    string
		force     bool
		wantErr   bool
		wantPack  string // expected provenance.package after call
		errSubstr string
	}{
		{
			name: "new assignment",
			setup: func(s *store.InMemoryGraphStore) {
				s.AddNode(context.Background(), store.Node{
					ID:       "b-1",
					Kind:     "behavior",
					Content:  map[string]interface{}{"name": "test"},
					Metadata: map[string]interface{}{},
				})
			},
			behavior: "b-1",
			packID:   "my-org/my-pack",
			wantPack: "my-org/my-pack",
		},
		{
			name: "already member no-op",
			setup: func(s *store.InMemoryGraphStore) {
				s.AddNode(context.Background(), store.Node{
					ID:      "b-2",
					Kind:    "behavior",
					Content: map[string]interface{}{"name": "test"},
					Metadata: map[string]interface{}{
						"provenance": map[string]interface{}{
							"package": "my-org/my-pack",
						},
					},
				})
			},
			behavior: "b-2",
			packID:   "my-org/my-pack",
			wantPack: "my-org/my-pack",
		},
		{
			name: "cross-pack without force errors",
			setup: func(s *store.InMemoryGraphStore) {
				s.AddNode(context.Background(), store.Node{
					ID:      "b-3",
					Kind:    "behavior",
					Content: map[string]interface{}{"name": "test"},
					Metadata: map[string]interface{}{
						"provenance": map[string]interface{}{
							"package": "other-org/other-pack",
						},
					},
				})
			},
			behavior:  "b-3",
			packID:    "my-org/my-pack",
			wantErr:   true,
			errSubstr: "already belongs to pack",
		},
		{
			name: "cross-pack with force succeeds",
			setup: func(s *store.InMemoryGraphStore) {
				s.AddNode(context.Background(), store.Node{
					ID:      "b-4",
					Kind:    "behavior",
					Content: map[string]interface{}{"name": "test"},
					Metadata: map[string]interface{}{
						"provenance": map[string]interface{}{
							"package": "other-org/other-pack",
						},
					},
				})
			},
			behavior: "b-4",
			packID:   "my-org/my-pack",
			force:    true,
			wantPack: "my-org/my-pack",
		},
		{
			name: "forgotten behavior rejected",
			setup: func(s *store.InMemoryGraphStore) {
				s.AddNode(context.Background(), store.Node{
					ID:       "b-5",
					Kind:     store.NodeKindForgotten,
					Content:  map[string]interface{}{"name": "forgotten"},
					Metadata: map[string]interface{}{},
				})
			},
			behavior:  "b-5",
			packID:    "my-org/my-pack",
			wantErr:   true,
			errSubstr: "forgotten",
		},
		{
			name:      "nonexistent behavior errors",
			setup:     func(s *store.InMemoryGraphStore) {},
			behavior:  "b-nonexistent",
			packID:    "my-org/my-pack",
			wantErr:   true,
			errSubstr: "not found",
		},
		{
			name:      "invalid pack ID errors",
			setup:     func(s *store.InMemoryGraphStore) {},
			behavior:  "b-1",
			packID:    "invalid",
			wantErr:   true,
			errSubstr: "invalid pack ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := store.NewInMemoryGraphStore()
			tt.setup(s)
			ctx := context.Background()

			err := AddToPack(ctx, s, tt.behavior, tt.packID, tt.force)
			if (err != nil) != tt.wantErr {
				t.Fatalf("AddToPack() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if tt.errSubstr != "" && err != nil {
					if got := err.Error(); !strings.Contains(got, tt.errSubstr) {
						t.Errorf("error = %q, want substring %q", got, tt.errSubstr)
					}
				}
				return
			}

			// Verify provenance was stamped
			node, _ := s.GetNode(ctx, tt.behavior)
			if node == nil {
				t.Fatal("expected node to exist after AddToPack")
			}
			gotPack := models.ExtractPackageName(node.Metadata)
			if gotPack != tt.wantPack {
				t.Errorf("provenance.package = %q, want %q", gotPack, tt.wantPack)
			}
		})
	}
}

func TestRemoveFromPack(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*store.InMemoryGraphStore)
		behavior  string
		mode      RemoveMode
		wantErr   bool
		errSubstr string
		checkNode func(t *testing.T, node *store.Node)
	}{
		{
			name: "unassign mode clears pack",
			setup: func(s *store.InMemoryGraphStore) {
				s.AddNode(context.Background(), store.Node{
					ID:      "b-1",
					Kind:    "behavior",
					Content: map[string]interface{}{"name": "test"},
					Metadata: map[string]interface{}{
						"provenance": map[string]interface{}{
							"package":         "my-org/my-pack",
							"package_version": "1.0.0",
						},
					},
				})
			},
			behavior: "b-1",
			mode:     RemoveModeUnassign,
			checkNode: func(t *testing.T, node *store.Node) {
				if node.Kind != "behavior" {
					t.Errorf("Kind = %q, want %q", node.Kind, "behavior")
				}
				if pkg := models.ExtractPackageName(node.Metadata); pkg != "" {
					t.Errorf("provenance.package = %q, want empty", pkg)
				}
				if ver := models.ExtractPackageVersion(node.Metadata); ver != "" {
					t.Errorf("provenance.package_version = %q, want empty", ver)
				}
			},
		},
		{
			name: "forget mode marks forgotten",
			setup: func(s *store.InMemoryGraphStore) {
				s.AddNode(context.Background(), store.Node{
					ID:      "b-2",
					Kind:    "behavior",
					Content: map[string]interface{}{"name": "test"},
					Metadata: map[string]interface{}{
						"provenance": map[string]interface{}{
							"package": "my-org/my-pack",
						},
					},
				})
			},
			behavior: "b-2",
			mode:     RemoveModeForgotten,
			checkNode: func(t *testing.T, node *store.Node) {
				if node.Kind != store.NodeKindForgotten {
					t.Errorf("Kind = %q, want %q", node.Kind, store.NodeKindForgotten)
				}
			},
		},
		{
			name: "no pack membership errors",
			setup: func(s *store.InMemoryGraphStore) {
				s.AddNode(context.Background(), store.Node{
					ID:       "b-3",
					Kind:     "behavior",
					Content:  map[string]interface{}{"name": "test"},
					Metadata: map[string]interface{}{},
				})
			},
			behavior:  "b-3",
			mode:      RemoveModeUnassign,
			wantErr:   true,
			errSubstr: "does not belong to any pack",
		},
		{
			name:      "nonexistent behavior errors",
			setup:     func(s *store.InMemoryGraphStore) {},
			behavior:  "b-nonexistent",
			mode:      RemoveModeUnassign,
			wantErr:   true,
			errSubstr: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := store.NewInMemoryGraphStore()
			tt.setup(s)
			ctx := context.Background()

			err := RemoveFromPack(ctx, s, tt.behavior, tt.mode)
			if (err != nil) != tt.wantErr {
				t.Fatalf("RemoveFromPack() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if tt.errSubstr != "" && err != nil {
					if got := err.Error(); !strings.Contains(got, tt.errSubstr) {
						t.Errorf("error = %q, want substring %q", got, tt.errSubstr)
					}
				}
				return
			}

			node, _ := s.GetNode(ctx, tt.behavior)
			if node == nil {
				t.Fatal("expected node to exist after RemoveFromPack")
			}
			if tt.checkNode != nil {
				tt.checkNode(t, node)
			}
		})
	}
}
