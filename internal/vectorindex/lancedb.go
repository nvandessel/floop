package vectorindex

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/apache/arrow/go/v17/arrow"
	"github.com/apache/arrow/go/v17/arrow/array"
	"github.com/apache/arrow/go/v17/arrow/memory"
	"github.com/lancedb/lancedb-go/pkg/contracts"
	"github.com/lancedb/lancedb-go/pkg/lancedb"

	"github.com/nvandessel/floop/internal/vecmath"
)

const lanceTableName = "behaviors"

// LanceDBConfig holds configuration for LanceDBIndex.
type LanceDBConfig struct {
	// Dir is the directory where LanceDB stores its data files.
	Dir string

	// Dims is the dimensionality of the embedding vectors.
	Dims int
}

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
	idBuilder.Append(behaviorID)
	idArray := idBuilder.NewArray()
	defer idArray.Release()

	floatBuilder := array.NewFloat32Builder(pool)
	floatBuilder.AppendValues(vector, nil)
	floatArray := floatBuilder.NewArray()
	defer floatArray.Release()

	vectorType := arrow.FixedSizeListOf(int32(l.dims), arrow.PrimitiveTypes.Float32)
	vectorArray := array.NewFixedSizeListData(
		array.NewData(vectorType, 1, []*memory.Buffer{nil}, []arrow.ArrayData{floatArray.Data()}, 0, 0),
	)
	defer vectorArray.Release()

	rec := array.NewRecord(arrowSchema, []arrow.Array{idArray, vectorArray}, 1)
	return rec, nil
}

// Add inserts or replaces the vector for the given behavior ID.
func (l *LanceDBIndex) Add(_ context.Context, behaviorID string, vector []float32) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	cp := make([]float32, len(vector))
	copy(cp, vector)

	ctx := context.Background()

	// Delete existing entry for upsert semantics.
	_ = l.table.Delete(ctx, fmt.Sprintf("id = '%s'", behaviorID))

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
func (l *LanceDBIndex) Remove(_ context.Context, behaviorID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// LanceDB delete is idempotent — no error for missing rows.
	return l.table.Delete(context.Background(), fmt.Sprintf("id = '%s'", behaviorID))
}

// Search returns the topK most similar vectors to query, sorted by descending score.
// Score is cosine similarity in [-1, 1], computed from the returned vectors.
func (l *LanceDBIndex) Search(_ context.Context, query []float32, topK int) ([]SearchResult, error) {
	if len(query) == 0 || topK <= 0 {
		return nil, nil
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	ctx := context.Background()

	count, err := l.table.Count(ctx)
	if err != nil || count == 0 {
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
		var score float64
		if vec := extractVector(row["vector"]); vec != nil {
			score = vecmath.CosineSimilarity(query, vec)
		}

		results = append(results, SearchResult{
			BehaviorID: id,
			Score:      score,
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

	if l.table != nil {
		l.table.Close()
	}
	if l.db != nil {
		l.db.Close()
	}
	return nil
}

// Verify LanceDBIndex satisfies the VectorIndex interface at compile time.
var _ VectorIndex = (*LanceDBIndex)(nil)
