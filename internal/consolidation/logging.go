package consolidation

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nvandessel/floop/internal/logging"
)

// ConsolidationLogEntry represents a structured log entry for a consolidation
// pipeline stage. Each LLM call or decision produces one entry.
type ConsolidationLogEntry struct {
	RunID      string         `json:"run_id"`
	Stage      string         `json:"stage"`
	Model      string         `json:"model"`
	TokensUsed int            `json:"tokens_used,omitempty"`
	DurationMs int64          `json:"duration_ms"`
	InputHash  string         `json:"input_hash,omitempty"`
	Timestamp  time.Time      `json:"timestamp"`
	Data       map[string]any `json:"data"`
}

// ConsolidationLogger wraps a DecisionLogger with consolidation-specific
// helper methods for logging each pipeline stage. A nil ConsolidationLogger
// is safe to use; all methods are no-ops on nil receiver.
type ConsolidationLogger struct {
	dl    *logging.DecisionLogger
	runID string
	model string
}

// NewConsolidationLogger creates a ConsolidationLogger for a specific run.
// Returns nil if the underlying DecisionLogger is nil.
func NewConsolidationLogger(dl *logging.DecisionLogger, runID, model string) *ConsolidationLogger {
	if dl == nil {
		return nil
	}
	return &ConsolidationLogger{
		dl:    dl,
		runID: runID,
		model: model,
	}
}

// logEntry writes a ConsolidationLogEntry to the underlying DecisionLogger.
func (cl *ConsolidationLogger) logEntry(entry ConsolidationLogEntry) {
	if cl == nil {
		return
	}

	m := map[string]any{
		"run_id":      entry.RunID,
		"stage":       entry.Stage,
		"model":       entry.Model,
		"duration_ms": entry.DurationMs,
		"timestamp":   entry.Timestamp.Format(time.RFC3339Nano),
	}
	if entry.TokensUsed > 0 {
		m["tokens_used"] = entry.TokensUsed
	}
	if entry.InputHash != "" {
		m["input_hash"] = entry.InputHash
	}
	if entry.Data != nil {
		m["data"] = entry.Data
	}

	cl.dl.Log(m)
}

// LogExtract logs an extract stage decision.
func (cl *ConsolidationLogger) LogExtract(stage string, durationMs int64, tokensUsed int, inputHash string, data map[string]any) {
	if cl == nil {
		return
	}
	cl.logEntry(ConsolidationLogEntry{
		RunID:      cl.runID,
		Stage:      stage,
		Model:      cl.model,
		TokensUsed: tokensUsed,
		DurationMs: durationMs,
		InputHash:  inputHash,
		Timestamp:  time.Now().UTC(),
		Data:       data,
	})
}

// LogClassify logs a classify stage decision.
func (cl *ConsolidationLogger) LogClassify(durationMs int64, tokensUsed int, inputHash string, data map[string]any) {
	if cl == nil {
		return
	}
	cl.logEntry(ConsolidationLogEntry{
		RunID:      cl.runID,
		Stage:      "classify",
		Model:      cl.model,
		TokensUsed: tokensUsed,
		DurationMs: durationMs,
		InputHash:  inputHash,
		Timestamp:  time.Now().UTC(),
		Data:       data,
	})
}

// LogRelate logs a relate stage decision.
func (cl *ConsolidationLogger) LogRelate(durationMs int64, tokensUsed int, data map[string]any) {
	if cl == nil {
		return
	}
	cl.logEntry(ConsolidationLogEntry{
		RunID:      cl.runID,
		Stage:      "relate",
		Model:      cl.model,
		TokensUsed: tokensUsed,
		DurationMs: durationMs,
		Timestamp:  time.Now().UTC(),
		Data:       data,
	})
}

// LogPromote logs a promote stage decision (promote, merge, or skip).
func (cl *ConsolidationLogger) LogPromote(action string, durationMs int64, data map[string]any) {
	if cl == nil {
		return
	}
	if data == nil {
		data = make(map[string]any)
	}
	data["action"] = action
	cl.logEntry(ConsolidationLogEntry{
		RunID:      cl.runID,
		Stage:      "promote",
		Model:      cl.model,
		DurationMs: durationMs,
		Timestamp:  time.Now().UTC(),
		Data:       data,
	})
}

// LogFallback logs a fallback event when the LLM fails and heuristics are used.
func (cl *ConsolidationLogger) LogFallback(stage string, reason string, err error) {
	if cl == nil {
		return
	}
	data := map[string]any{
		"fallback_used":   true,
		"fallback_reason": reason,
	}
	if err != nil {
		data["error"] = err.Error()
	}
	cl.logEntry(ConsolidationLogEntry{
		RunID:     cl.runID,
		Stage:     stage,
		Model:     cl.model,
		Timestamp: time.Now().UTC(),
		Data:      data,
	})
}

// InputHash computes a SHA-256 hash of the JSON-serialized input for
// training data deduplication across runs. Returns an empty string on
// serialization failure.
func InputHash(input any) string {
	data, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", h)
}
