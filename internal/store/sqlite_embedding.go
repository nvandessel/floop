package store

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
)

// encodeEmbedding converts a float32 slice to a binary blob (little-endian).
func encodeEmbedding(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// decodeEmbedding converts a binary blob back to a float32 slice.
func decodeEmbedding(data []byte) []float32 {
	if len(data) == 0 || len(data)%4 != 0 {
		return nil
	}
	vec := make([]float32, len(data)/4)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return vec
}

// StoreEmbedding stores an embedding vector for a behavior.
func (s *SQLiteGraphStore) StoreEmbedding(ctx context.Context, behaviorID string, embedding []float32, modelName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.storeEmbeddingUnlocked(ctx, behaviorID, embedding, modelName)
}

// storeEmbeddingUnlocked stores an embedding without acquiring the mutex.
// Caller must ensure exclusive access (e.g., during import or while holding the lock).
func (s *SQLiteGraphStore) storeEmbeddingUnlocked(ctx context.Context, behaviorID string, embedding []float32, modelName string) error {
	blob := encodeEmbedding(embedding)
	result, err := s.db.ExecContext(ctx,
		`UPDATE behaviors SET embedding = ?, embedding_model = ? WHERE id = ?`,
		blob, modelName, behaviorID)
	if err != nil {
		return fmt.Errorf("store embedding for %s: %w", behaviorID, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("behavior not found: %s", behaviorID)
	}

	return nil
}

// GetAllEmbeddings returns all behaviors that have embeddings.
func (s *SQLiteGraphStore) GetAllEmbeddings(ctx context.Context) ([]BehaviorEmbedding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, embedding FROM behaviors WHERE embedding IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("query embeddings: %w", err)
	}
	defer rows.Close()

	var results []BehaviorEmbedding
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, fmt.Errorf("scan embedding: %w", err)
		}
		vec := decodeEmbedding(blob)
		if vec == nil {
			continue
		}
		results = append(results, BehaviorEmbedding{
			BehaviorID: id,
			Embedding:  vec,
		})
	}

	return results, nil
}

// GetBehaviorIDsWithoutEmbeddings returns IDs of behaviors that do not have embeddings.
func (s *SQLiteGraphStore) GetBehaviorIDsWithoutEmbeddings(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM behaviors WHERE embedding IS NULL AND kind = 'behavior'`)
	if err != nil {
		return nil, fmt.Errorf("query behaviors without embeddings: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan behavior ID: %w", err)
		}
		ids = append(ids, id)
	}

	return ids, nil
}
