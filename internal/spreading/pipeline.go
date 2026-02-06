package spreading

import (
	"context"
	"fmt"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// Pipeline orchestrates the full spreading activation flow:
// context -> seeds -> activation -> results.
type Pipeline struct {
	selector *SeedSelector
	engine   *Engine
	store    store.GraphStore
}

// NewPipeline creates a new spreading activation pipeline.
func NewPipeline(s store.GraphStore, config Config) *Pipeline {
	return &Pipeline{
		selector: NewSeedSelector(s),
		engine:   NewEngine(s, config),
		store:    s,
	}
}

// Run performs the full activation pipeline for the given context.
// Returns activated behaviors sorted by activation level.
func (p *Pipeline) Run(ctx context.Context, actCtx models.ContextSnapshot) ([]Result, error) {
	seeds, err := p.selector.SelectSeeds(ctx, actCtx)
	if err != nil {
		return nil, fmt.Errorf("seed selection: %w", err)
	}
	if len(seeds) == 0 {
		return nil, nil
	}
	return p.engine.Activate(ctx, seeds)
}
