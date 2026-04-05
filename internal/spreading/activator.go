package spreading

import "context"

// Activator performs spreading activation over a behavior graph.
type Activator interface {
	Activate(ctx context.Context, seeds []Seed) ([]Result, error)
}
