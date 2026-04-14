//go:build !cgo

package spreading

import (
	"context"
	"errors"

	"github.com/nvandessel/floop/internal/store"
)

// NativeEngine is a stub for non-CGO builds.
// All methods return errors; build with CGO_ENABLED=1 to use sproink.
type NativeEngine struct{}

// NewNativeEngine requires CGO for the sproink Rust bindings.
func NewNativeEngine(_ store.ExtendedGraphStore, _ Config) (*NativeEngine, error) {
	return nil, errors.New("sproink requires CGO; build with CGO_ENABLED=1")
}

func (e *NativeEngine) Activate(_ context.Context, _ []Seed) ([]Result, error) {
	return nil, errors.New("sproink requires CGO")
}

func (e *NativeEngine) Rebuild(_ context.Context) error {
	return errors.New("sproink requires CGO")
}

func (e *NativeEngine) Close() {}

var _ Activator = (*NativeEngine)(nil)
