//go:build cgo

package vault

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/apache/arrow/go/v17/arrow"
	"github.com/apache/arrow/go/v17/arrow/array"
	"github.com/apache/arrow/go/v17/arrow/memory"
	"github.com/lancedb/lancedb-go/pkg/contracts"
	"github.com/lancedb/lancedb-go/pkg/lancedb"

	"github.com/nvandessel/floop/internal/vectorindex"
)

// extractVector converts vector values from LanceDB result maps to []float32.
func extractVector(v interface{}) []float32 {
	switch val := v.(type) {
	case []interface{}:
		out := make([]float32, len(val))
		for i, x := range val {
			switch f := x.(type) {
			case float64:
				out[i] = float32(f)
			case float32:
				out[i] = f
			default:
				return nil
			}
		}
		return out
	case []float32:
		return val
	case []float64:
		out := make([]float32, len(val))
		for i, f := range val {
			out[i] = float32(f)
		}
		return out
	}
	return nil
}

const behaviorTableName = "behaviors"

// VectorSyncer syncs LanceDB vector tables between local and remote storage.
type VectorSyncer struct {
	localDir   string
	remoteURI  string
	remoteOpts *contracts.ConnectionOptions
	dims       int
	mu         sync.Mutex
}

// VectorSyncResult contains the results of a vector sync operation.
type VectorSyncResult struct {
	RowsPushed  int
	RowsPulled  int
	RowsSkipped int
}

// NewVectorSyncer creates a VectorSyncer for the given local directory and remote URI.
func NewVectorSyncer(localDir, remoteURI string, opts *contracts.ConnectionOptions, dims int) *VectorSyncer {
	return &VectorSyncer{
		localDir:   localDir,
		remoteURI:  remoteURI,
		remoteOpts: opts,
		dims:       dims,
	}
}

// Push reads all rows from the local behaviors table and upserts them into the remote.
func (v *VectorSyncer) Push(ctx context.Context) (*VectorSyncResult, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	localDB, err := lancedb.Connect(ctx, v.localDir, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to local LanceDB: %w", err)
	}
	defer localDB.Close()

	localTable, err := openTable(ctx, localDB, behaviorTableName)
	if err != nil {
		return nil, fmt.Errorf("opening local table: %w", err)
	}
	defer localTable.Close()

	remoteDB, err := lancedb.Connect(ctx, v.remoteURI, v.remoteOpts)
	if err != nil {
		return nil, fmt.Errorf("connecting to remote LanceDB: %w", err)
	}
	defer remoteDB.Close()

	remoteTable, err := openOrCreateTable(ctx, remoteDB, behaviorTableName, v.dims)
	if err != nil {
		return nil, fmt.Errorf("opening remote table: %w", err)
	}
	defer remoteTable.Close()

	return syncRows(ctx, localTable, remoteTable, v.dims)
}

// Pull reads all rows from the remote behaviors table and upserts them into the local.
func (v *VectorSyncer) Pull(ctx context.Context) (*VectorSyncResult, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	localDB, err := lancedb.Connect(ctx, v.localDir, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to local LanceDB: %w", err)
	}
	defer localDB.Close()

	localTable, err := openOrCreateTable(ctx, localDB, behaviorTableName, v.dims)
	if err != nil {
		return nil, fmt.Errorf("opening local table: %w", err)
	}
	defer localTable.Close()

	remoteDB, err := lancedb.Connect(ctx, v.remoteURI, v.remoteOpts)
	if err != nil {
		return nil, fmt.Errorf("connecting to remote LanceDB: %w", err)
	}
	defer remoteDB.Close()

	remoteTable, err := openTable(ctx, remoteDB, behaviorTableName)
	if err != nil {
		return nil, fmt.Errorf("opening remote table: %w", err)
	}
	defer remoteTable.Close()

	return syncRows(ctx, remoteTable, localTable, v.dims)
}

// LocalRowCount returns the number of rows in the local behaviors table.
func (v *VectorSyncer) LocalRowCount(ctx context.Context) (int, error) {
	db, err := lancedb.Connect(ctx, v.localDir, nil)
	if err != nil {
		return 0, fmt.Errorf("connecting to local LanceDB: %w", err)
	}
	defer db.Close()

	table, err := openTable(ctx, db, behaviorTableName)
	if err != nil {
		return 0, nil // table doesn't exist → 0 rows
	}
	defer table.Close()

	count, err := table.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("counting local rows: %w", err)
	}
	return int(count), nil
}

// RemoteRowCount returns the number of rows in the remote behaviors table.
func (v *VectorSyncer) RemoteRowCount(ctx context.Context) (int, error) {
	db, err := lancedb.Connect(ctx, v.remoteURI, v.remoteOpts)
	if err != nil {
		return 0, fmt.Errorf("connecting to remote LanceDB: %w", err)
	}
	defer db.Close()

	table, err := openTable(ctx, db, behaviorTableName)
	if err != nil {
		return 0, nil // table doesn't exist → 0 rows
	}
	defer table.Close()

	count, err := table.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("counting remote rows: %w", err)
	}
	return int(count), nil
}

// syncRows reads all rows from src and upserts them into dst.
func syncRows(ctx context.Context, src, dst contracts.ITable, dims int) (*VectorSyncResult, error) {
	count, err := src.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("counting source rows: %w", err)
	}
	if count == 0 {
		return &VectorSyncResult{}, nil
	}

	// Full table scan via JSON Select — deterministic, returns every row.
	// Query().Execute() uses Arrow IPC which has a multi-batch concatenation bug
	// in ipcBytesToRecord (returns only the first batch when rows span fragments).
	rows, err := src.Select(ctx, contracts.QueryConfig{})
	if err != nil {
		return nil, fmt.Errorf("reading source rows: %w", err)
	}

	arrowSchema, vectorType := vectorindex.BuildBehaviorSchema(dims)
	result := &VectorSyncResult{}

	for _, row := range rows {
		id, ok := row["id"].(string)
		if !ok {
			result.RowsSkipped++
			continue
		}

		vec := extractVector(row["vector"])
		if vec == nil {
			result.RowsSkipped++
			continue
		}

		// Upsert: delete then add
		escaped := strings.ReplaceAll(id, "'", "''")
		if delErr := dst.Delete(ctx, fmt.Sprintf("id = '%s'", escaped)); delErr != nil {
			return nil, fmt.Errorf("deleting stale row %s: %w", id, delErr)
		}

		rec, err := buildRecord(arrowSchema, vectorType, id, vec)
		if err != nil {
			return nil, fmt.Errorf("building record for %s: %w", id, err)
		}

		if err := dst.Add(ctx, rec, nil); err != nil {
			rec.Release()
			return nil, fmt.Errorf("adding row %s: %w", id, err)
		}
		rec.Release()
		result.RowsPushed++
	}

	return result, nil
}

// buildRecord creates a single-row Arrow record for a behavior embedding.
func buildRecord(schema *arrow.Schema, vectorType *arrow.FixedSizeListType, id string, vector []float32) (arrow.Record, error) {
	pool := memory.NewGoAllocator()

	idBuilder := array.NewStringBuilder(pool)
	defer idBuilder.Release()
	idBuilder.Append(id)
	idArray := idBuilder.NewArray()
	defer idArray.Release()

	floatBuilder := array.NewFloat32Builder(pool)
	defer floatBuilder.Release()
	floatBuilder.AppendValues(vector, nil)
	floatArray := floatBuilder.NewArray()
	defer floatArray.Release()

	vectorData := array.NewData(vectorType, 1, []*memory.Buffer{nil}, []arrow.ArrayData{floatArray.Data()}, 0, 0)
	defer vectorData.Release()
	vectorArray := array.NewFixedSizeListData(vectorData)
	defer vectorArray.Release()

	rec := array.NewRecord(schema, []arrow.Array{idArray, vectorArray}, 1)
	return rec, nil
}

// openTable opens an existing table. Returns error if table doesn't exist.
func openTable(ctx context.Context, db contracts.IConnection, name string) (contracts.ITable, error) {
	names, err := db.TableNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	for _, n := range names {
		if n == name {
			return db.OpenTable(ctx, name)
		}
	}
	return nil, fmt.Errorf("table %q not found", name)
}

// openOrCreateTable opens a table, or creates it if it doesn't exist.
func openOrCreateTable(ctx context.Context, db contracts.IConnection, name string, dims int) (contracts.ITable, error) {
	names, err := db.TableNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	for _, n := range names {
		if n == name {
			return db.OpenTable(ctx, name)
		}
	}

	lanceSchema, err := vectorindex.BuildLanceSchema(dims)
	if err != nil {
		return nil, fmt.Errorf("building schema: %w", err)
	}
	return db.CreateTable(ctx, name, lanceSchema)
}
