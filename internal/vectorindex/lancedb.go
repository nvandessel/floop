//go:build cgo

package vectorindex

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/apache/arrow/go/v17/arrow"
	"github.com/apache/arrow/go/v17/arrow/array"
	"github.com/apache/arrow/go/v17/arrow/memory"
	"github.com/lancedb/lancedb-go/pkg/contracts"
	"github.com/lancedb/lancedb-go/pkg/lancedb"

	"github.com/nvandessel/floop/internal/vecmath"
)

const lanceTableName = "behaviors"

// LanceDBIndex performs approximate nearest neighbor search using LanceDB,
// an embedded vector database. Thread-safe.
type LanceDBIndex struct {
	mu    sync.RWMutex
	db    contracts.IConnection
	table contracts.ITable
	dims  int
}

// NewLanceDBIndex creates a LanceDBIndex backed by the given directory.
// If a table already exists, it is opened; otherwise a new one is created.
func NewLanceDBIndex(cfg LanceDBConfig) (*LanceDBIndex, error) {
	ctx := context.Background()

	db, err := lancedb.Connect(ctx, cfg.Dir, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to LanceDB: %w", err)
	}

	names, err := db.TableNames(ctx)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("list tables: %w", err)
	}

	var table contracts.ITable
	found := false
	for _, n := range names {
		if n == lanceTableName {
			found = true
			break
		}
	}

	if found {
		table, err = db.OpenTable(ctx, lanceTableName)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("open table: %w", err)
		}
		// Validate that the on-disk schema matches the configured dimensions.
		// A mismatch means the embedding model changed; the user must delete .floop/vectors/.
		if schema, serr := table.Schema(ctx); serr == nil {
			vectorFieldFound := false
			for _, field := range schema.Fields() {
				if field.Name == "vector" {
					vectorFieldFound = true
					if fsl, ok := field.Type.(*arrow.FixedSizeListType); ok {
						if int(fsl.Len()) != cfg.Dims {
							table.Close()
							db.Close()
							return nil, fmt.Errorf(
								"vector dimension mismatch: table has %d but config expects %d "+
									"(embedding model changed? delete .floop/vectors/ to rebuild)",
								fsl.Len(), cfg.Dims,
							)
						}
					}
				}
			}
			if !vectorFieldFound {
				table.Close()
				db.Close()
				return nil, fmt.Errorf(
					"existing table is missing the 'vector' field; " +
						"delete .floop/vectors/ to rebuild",
				)
			}
		}
	} else {
		schema, serr := lancedb.NewSchemaBuilder().
			AddStringField("id", false).
			AddVectorField("vector", cfg.Dims, contracts.VectorDataTypeFloat32, false).
			Build()
		if serr != nil {
			db.Close()
			return nil, fmt.Errorf("build schema: %w", serr)
		}
		table, err = db.CreateTable(ctx, lanceTableName, schema)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("create table: %w", err)
		}
	}

	return &LanceDBIndex{db: db, table: table, dims: cfg.Dims}, nil
}

// buildRecord creates a single-row Arrow record for a behavior embedding.
func (l *LanceDBIndex) buildRecord(behaviorID string, vector []float32) (arrow.Record, error) {
	pool := memory.NewGoAllocator()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.BinaryTypes.String},
		{Name: "vector", Type: arrow.FixedSizeListOf(int32(l.dims), arrow.PrimitiveTypes.Float32)},
	}, nil)

	idBuilder := array.NewStringBuilder(pool)
	defer idBuilder.Release()
	idBuilder.Append(behaviorID)
	idArray := idBuilder.NewArray()
	defer idArray.Release()

	floatBuilder := array.NewFloat32Builder(pool)
	defer floatBuilder.Release()
	floatBuilder.AppendValues(vector, nil)
	floatArray := floatBuilder.NewArray()
	defer floatArray.Release()

	vectorType := arrow.FixedSizeListOf(int32(l.dims), arrow.PrimitiveTypes.Float32)
	vectorData := array.NewData(vectorType, 1, []*memory.Buffer{nil}, []arrow.ArrayData{floatArray.Data()}, 0, 0)
	defer vectorData.Release()
	vectorArray := array.NewFixedSizeListData(vectorData)
	defer vectorArray.Release()

	rec := array.NewRecord(arrowSchema, []arrow.Array{idArray, vectorArray}, 1)
	return rec, nil
}

// Add inserts or replaces the vector for the given behavior ID.
func (l *LanceDBIndex) Add(ctx context.Context, behaviorID string, vector []float32) error {
	if len(vector) != l.dims {
		return fmt.Errorf("vector dimension mismatch: got %d, want %d", len(vector), l.dims)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	cp := make([]float32, len(vector))
	copy(cp, vector)

	// Delete existing entry for upsert semantics.
	// Note: each delete creates a tombstone. LanceDB v0.1.2 does not expose
	// Optimize/Compact — tombstones are cleaned up on the next full rewrite
	// (e.g., index rebuild). This is acceptable for the expected write volume.
	escaped := strings.ReplaceAll(behaviorID, "'", "''")
	_ = l.table.Delete(ctx, fmt.Sprintf("id = '%s'", escaped))

	rec, err := l.buildRecord(behaviorID, cp)
	if err != nil {
		return fmt.Errorf("build record: %w", err)
	}
	defer rec.Release()

	if err := l.table.Add(ctx, rec, nil); err != nil {
		return fmt.Errorf("add to LanceDB: %w", err)
	}

	return nil
}

// Remove deletes the vector for the given behavior ID. No-op if not found.
func (l *LanceDBIndex) Remove(ctx context.Context, behaviorID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// LanceDB delete is idempotent — no error for missing rows.
	escaped := strings.ReplaceAll(behaviorID, "'", "''")
	return l.table.Delete(ctx, fmt.Sprintf("id = '%s'", escaped))
}

// Search returns the topK most similar vectors to query, sorted by descending score.
// Score is cosine similarity in [-1, 1], computed from the returned vectors.
func (l *LanceDBIndex) Search(ctx context.Context, query []float32, topK int) ([]SearchResult, error) {
	if len(query) == 0 || topK <= 0 {
		return nil, nil
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	count, err := l.table.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count table: %w", err)
	}
	if count == 0 {
		return nil, nil
	}

	// Clamp topK to actual row count.
	k := topK
	if int64(k) > count {
		k = int(count)
	}

	rows, err := l.table.VectorSearch(ctx, "vector", query, k)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	results := make([]SearchResult, 0, len(rows))
	for _, row := range rows {
		id, ok := row["id"].(string)
		if !ok {
			continue
		}

		// Compute cosine similarity from the returned vector.
		// LanceDB's VectorSearch may use L2 by default; computing our own
		// cosine similarity ensures consistent scoring with BruteForceIndex.
		vec := extractVector(row["vector"])
		if vec == nil {
			// Skip results with unparseable vectors rather than returning score=0.
			continue
		}

		results = append(results, SearchResult{
			BehaviorID: id,
			Score:      vecmath.CosineSimilarity(query, vec),
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results, nil
}

// extractVector converts the vector value from a LanceDB result map
// (typically []interface{} of float64) into []float32.
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
				return nil // unknown element type — skip this result
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

// Len returns the number of vectors in the index.
func (l *LanceDBIndex) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	count, err := l.table.Count(context.Background())
	if err != nil {
		return 0
	}
	return int(count)
}

// Save is a no-op. LanceDB auto-persists on write.
func (l *LanceDBIndex) Save(_ context.Context) error {
	return nil
}

// Close releases resources.
func (l *LanceDBIndex) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var errs []error
	if l.table != nil {
		if err := l.table.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close table: %w", err))
		}
	}
	if l.db != nil {
		if err := l.db.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close db: %w", err))
		}
	}
	return errors.Join(errs...)
}

// Verify LanceDBIndex satisfies the VectorIndex interface at compile time.
var _ VectorIndex = (*LanceDBIndex)(nil)
