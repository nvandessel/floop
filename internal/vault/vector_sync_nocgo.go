//go:build !cgo

package vault

import (
	"context"
	"errors"

	"github.com/lancedb/lancedb-go/pkg/contracts"
)

var errNoCGO = errors.New("vector sync requires CGO (lancedb)")

// VectorSyncer syncs LanceDB vector tables between local and remote storage.
// This is the non-CGO stub.
type VectorSyncer struct{}

// VectorSyncResult contains the results of a vector sync operation.
type VectorSyncResult struct {
	RowsPushed  int
	RowsPulled  int
	RowsSkipped int
}

// NewVectorSyncer returns a stub VectorSyncer when CGO is disabled.
func NewVectorSyncer(localDir, remoteURI string, opts *contracts.ConnectionOptions, dims int) *VectorSyncer {
	return &VectorSyncer{}
}

// Push is a stub that returns an error when CGO is disabled.
func (v *VectorSyncer) Push(ctx context.Context) (*VectorSyncResult, error) {
	return nil, errNoCGO
}

// Pull is a stub that returns an error when CGO is disabled.
func (v *VectorSyncer) Pull(ctx context.Context) (*VectorSyncResult, error) {
	return nil, errNoCGO
}

// LocalRowCount is a stub that returns an error when CGO is disabled.
func (v *VectorSyncer) LocalRowCount(ctx context.Context) (int, error) {
	return 0, errNoCGO
}

// RemoteRowCount is a stub that returns an error when CGO is disabled.
func (v *VectorSyncer) RemoteRowCount(ctx context.Context) (int, error) {
	return 0, errNoCGO
}
