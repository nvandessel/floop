//go:build !cgo

package vectorindex

import (
	"context"
	"errors"
)

// LanceDBIndex is a stub for non-CGO builds.
// All methods return errors; use BruteForceIndex instead.
type LanceDBIndex struct{}

// NewLanceDBIndex requires CGO for the LanceDB Rust bindings.
// Build with CGO_ENABLED=1 to use LanceDB, or use BruteForceIndex as a fallback.
func NewLanceDBIndex(_ LanceDBConfig) (*LanceDBIndex, error) {
	return nil, errors.New("LanceDB requires CGO; build with CGO_ENABLED=1")
}

func (l *LanceDBIndex) Add(_ context.Context, _ string, _ []float32) error {
	return errors.New("LanceDB requires CGO")
}

func (l *LanceDBIndex) Remove(_ context.Context, _ string) error {
	return errors.New("LanceDB requires CGO")
}

func (l *LanceDBIndex) Search(_ context.Context, _ []float32, _ int) ([]SearchResult, error) {
	return nil, errors.New("LanceDB requires CGO")
}

func (l *LanceDBIndex) Len() int { return 0 }

func (l *LanceDBIndex) Save(_ context.Context) error {
	return errors.New("LanceDB requires CGO")
}

func (l *LanceDBIndex) Close() error { return nil }

var _ VectorIndex = (*LanceDBIndex)(nil)
