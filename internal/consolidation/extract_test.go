package consolidation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/models"
)

// mockLLMClient is a test double for llm.Client.
type mockLLMClient struct {
	// responses maps call index to response string.
	responses []string
	// errors maps call index to error (nil = success).
	errs []error
	// callIndex tracks the current call number.
	callIndex int
	// calls records messages from each call for inspection.
	calls [][]llm.Message
}

func (m *mockLLMClient) Complete(_ context.Context, messages []llm.Message) (string, error) {
	idx := m.callIndex
	m.callIndex++
	m.calls = append(m.calls, messages)

	if idx < len(m.errs) && m.errs[idx] != nil {
		return "", m.errs[idx]
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return "{}", nil
}

func (m *mockLLMClient) Available() bool { return true }

// makeEvents creates n test events with sequential IDs.
func makeEvents(n int) []events.Event {
	evts := make([]events.Event, n)
	for i := range n {
		evts[i] = events.Event{
			ID:        fmt.Sprintf("evt-%d", i+1),
			SessionID: "sess-1",
			Actor:     events.ActorUser,
			Kind:      events.KindMessage,
			Content:   fmt.Sprintf("Test message %d with enough content to pass filters", i+1),
			ProjectID: "proj-1",
		}
	}
	return evts
}

// makeSummaryResponse builds a valid JSON summary response for Pass 1.
func makeSummaryResponse(chunkIdx int) string {
	s := extractChunkSummary{
		Summary:     fmt.Sprintf("Chunk %d summary", chunkIdx),
		Tone:        "neutral",
		Phase:       "building",
		Pattern:     "collaborating",
		KeyMoments:  []keyMoment{{EventID: fmt.Sprintf("evt-%d", chunkIdx*20+1), Type: "decision", Brief: "Decided on approach"}},
		OpenThreads: []string{"unresolved topic"},
	}
	data, _ := json.Marshal(s)
	return string(data)
}

// makeArcResponse builds a valid JSON arc response for Pass 2.
func makeArcResponse() string {
	a := extractArcSummary{
		Arc:               "User worked through auth implementation",
		DominantTone:      "neutral",
		SessionOutcome:    "resolved",
		Themes:            []string{"auth", "testing"},
		BehavioralSignals: []string{"User prefers explicit error handling"},
	}
	data, _ := json.Marshal(a)
	return string(data)
}

// makeExtractResponse builds a valid JSON extract response for Pass 3.
func makeExtractResponse(eventIDs []string, alreadyCaptured bool) string {
	r := extractResponse{
		Candidates: []extractCandidate{
			{
				SourceEvents:       eventIDs,
				RawText:            "No don't mock the database, use the real thing",
				CandidateType:      "correction",
				Confidence:         0.92,
				Sentiment:          "frustrated",
				SessionPhase:       "stuck",
				InteractionPattern: "teaching",
				Rationale:          "Explicit correction about testing approach",
				AlreadyCaptured:    alreadyCaptured,
			},
		},
	}
	data, _ := json.Marshal(r)
	return string(data)
}

func TestLLMExtract_SingleChunk(t *testing.T) {
	evts := makeEvents(10)

	mock := &mockLLMClient{
		responses: []string{
			makeSummaryResponse(0), // Pass 1: summarize
			makeArcResponse(),      // Pass 2: arc
			makeExtractResponse([]string{"evt-1", "evt-2"}, false), // Pass 3: extract
		},
	}

	config := DefaultLLMConsolidatorConfig()
	config.ChunkSize = 20
	c := NewLLMConsolidator(mock, nil, config)

	candidates, err := c.Extract(context.Background(), evts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	cand := candidates[0]
	if cand.CandidateType != "correction" {
		t.Errorf("expected type 'correction', got %q", cand.CandidateType)
	}
	if cand.Confidence != 0.92 {
		t.Errorf("expected confidence 0.92, got %f", cand.Confidence)
	}
	if cand.Sentiment != "frustrated" {
		t.Errorf("expected sentiment 'frustrated', got %q", cand.Sentiment)
	}
	if cand.SessionPhase != "stuck" {
		t.Errorf("expected session_phase 'stuck', got %q", cand.SessionPhase)
	}
	if cand.InteractionPattern != "teaching" {
		t.Errorf("expected interaction_pattern 'teaching', got %q", cand.InteractionPattern)
	}
	if cand.Rationale == "" {
		t.Error("expected non-empty rationale")
	}
	// AlreadyCaptured is only used as a filter signal and not propagated to output

	// Verify 3 LLM calls were made (summarize + arc + extract)
	if mock.callIndex != 3 {
		t.Errorf("expected 3 LLM calls, got %d", mock.callIndex)
	}
}

func TestLLMExtract_MultiChunk(t *testing.T) {
	evts := makeEvents(50)

	mock := &mockLLMClient{
		responses: []string{
			// Pass 1: 3 chunks (20 + 20 + 10)
			makeSummaryResponse(0),
			makeSummaryResponse(1),
			makeSummaryResponse(2),
			// Pass 2: arc
			makeArcResponse(),
			// Pass 3: 3 chunk extractions
			makeExtractResponse([]string{"evt-1"}, false),
			makeExtractResponse([]string{"evt-21"}, false),
			makeExtractResponse([]string{"evt-41"}, false),
		},
	}

	config := DefaultLLMConsolidatorConfig()
	config.ChunkSize = 20
	c := NewLLMConsolidator(mock, nil, config)

	candidates, err := c.Extract(context.Background(), evts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates (one per chunk), got %d", len(candidates))
	}

	// Verify 7 LLM calls: 3 summarize + 1 arc + 3 extract
	if mock.callIndex != 7 {
		t.Errorf("expected 7 LLM calls, got %d", mock.callIndex)
	}
}

func TestLLMExtract_Pass1Failure(t *testing.T) {
	evts := makeEvents(10)

	mock := &mockLLMClient{
		responses: []string{
			"", // Pass 1: will error (index 0) — chunk skipped
			makeExtractResponse([]string{"evt-1"}, false), // Pass 3: extract without arc (index 1)
		},
		errs: []error{
			errors.New("API unavailable"), // Pass 1 fails for chunk
			nil,                           // Pass 3 succeeds
		},
	}

	config := DefaultLLMConsolidatorConfig()
	config.ChunkSize = 20
	c := NewLLMConsolidator(mock, nil, config)

	candidates, err := c.Extract(context.Background(), evts)
	if err != nil {
		t.Fatalf("Extract returned error despite graceful degradation: %v", err)
	}

	// Pass 1 chunk skipped → no summaries → no arc → Pass 3 still runs
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate from Pass 3, got %d", len(candidates))
	}

	// 2 calls: failed summarize (skipped) + extract (no arc because no summaries)
	if mock.callIndex != 2 {
		t.Errorf("expected 2 LLM calls (failed summarize + extract), got %d", mock.callIndex)
	}
}

func TestLLMExtract_Pass2Failure(t *testing.T) {
	evts := makeEvents(10)

	mock := &mockLLMClient{
		responses: []string{
			makeSummaryResponse(0), // Pass 1: succeeds
			"",                     // Pass 2: will error
			makeExtractResponse([]string{"evt-1"}, false), // Pass 3: extract without arc
		},
		errs: []error{
			nil,                              // Pass 1 succeeds
			errors.New("arc synthesis fail"), // Pass 2 fails
			nil,                              // Pass 3 succeeds
		},
	}

	config := DefaultLLMConsolidatorConfig()
	config.ChunkSize = 20
	c := NewLLMConsolidator(mock, nil, config)

	candidates, err := c.Extract(context.Background(), evts)
	if err != nil {
		t.Fatalf("Extract returned error despite graceful degradation: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	// 3 calls: summarize + failed arc + extract
	if mock.callIndex != 3 {
		t.Errorf("expected 3 LLM calls, got %d", mock.callIndex)
	}
}

func TestLLMExtract_Pass3Failure(t *testing.T) {
	evts := makeEvents(10)

	// Add a correction pattern so heuristic fallback produces a candidate
	evts[0].Content = "No, don't do that. Instead use the real database for integration tests."

	mock := &mockLLMClient{
		responses: []string{
			makeSummaryResponse(0), // Pass 1
			makeArcResponse(),      // Pass 2
			"",                     // Pass 3: will error
		},
		errs: []error{
			nil,                              // Pass 1
			nil,                              // Pass 2
			errors.New("extract chunk fail"), // Pass 3 fails
		},
	}

	config := DefaultLLMConsolidatorConfig()
	config.ChunkSize = 20
	c := NewLLMConsolidator(mock, nil, config)

	candidates, err := c.Extract(context.Background(), evts)
	if err != nil {
		t.Fatalf("Extract returned error despite heuristic fallback: %v", err)
	}

	// Heuristic fallback should find the "no, don't" pattern
	if len(candidates) == 0 {
		t.Fatal("expected at least 1 candidate from heuristic fallback")
	}

	found := false
	for _, cand := range candidates {
		if cand.CandidateType == "correction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected heuristic fallback to find correction candidate")
	}
}

func TestLLMExtract_BadJSON(t *testing.T) {
	evts := makeEvents(10)
	evts[0].Content = "No, don't do that. Instead use proper error wrapping with context."

	mock := &mockLLMClient{
		responses: []string{
			"not valid json at all", // Pass 1: bad JSON → skipped, no summaries
		},
	}

	config := DefaultLLMConsolidatorConfig()
	config.ChunkSize = 20
	c := NewLLMConsolidator(mock, nil, config)

	candidates, err := c.Extract(context.Background(), evts)
	if err != nil {
		t.Fatalf("Extract returned error despite graceful degradation: %v", err)
	}

	// Bad JSON in Pass 1 → chunk skipped → no summaries → no arc → Pass 3 still runs.
	// Pass 3 receives "{}" from mock default, which parses as zero candidates.
	// No error from Pass 3, so heuristic fallback does not trigger.
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates from bad-JSON degradation path, got %d", len(candidates))
	}

	// 2 LLM calls: failed summarize (bad JSON) + Pass 3 extract
	if mock.callIndex != 2 {
		t.Errorf("expected 2 LLM calls, got %d", mock.callIndex)
	}
}

func TestLLMExtract_AlreadyCaptured(t *testing.T) {
	evts := makeEvents(10)

	mock := &mockLLMClient{
		responses: []string{
			makeSummaryResponse(0),
			makeArcResponse(),
			makeExtractResponse([]string{"evt-1"}, true), // already_captured=true
		},
	}

	config := DefaultLLMConsolidatorConfig()
	config.ChunkSize = 20
	c := NewLLMConsolidator(mock, nil, config)

	candidates, err := c.Extract(context.Background(), evts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	// already_captured candidates should be filtered out
	if len(candidates) != 0 {
		t.Fatalf("expected 0 candidates (all already_captured), got %d", len(candidates))
	}
}

func TestLLMExtract_EmptyEvents(t *testing.T) {
	mock := &mockLLMClient{}
	config := DefaultLLMConsolidatorConfig()
	c := NewLLMConsolidator(mock, nil, config)

	candidates, err := c.Extract(context.Background(), nil)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if candidates != nil {
		t.Fatalf("expected nil candidates for empty events, got %d", len(candidates))
	}

	// No LLM calls should be made
	if mock.callIndex != 0 {
		t.Errorf("expected 0 LLM calls, got %d", mock.callIndex)
	}
}

func TestChunkEvents(t *testing.T) {
	tests := []struct {
		name         string
		numEvents    int
		chunkSize    int
		wantChunks   int
		wantLastSize int
	}{
		{"exact multiple", 40, 20, 2, 20},
		{"remainder", 50, 20, 3, 10},
		{"single chunk", 10, 20, 1, 10},
		{"one event", 1, 20, 1, 1},
		{"zero size defaults", 10, 0, 1, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evts := makeEvents(tt.numEvents)
			chunks := chunkEvents(evts, tt.chunkSize)

			if len(chunks) != tt.wantChunks {
				t.Errorf("expected %d chunks, got %d", tt.wantChunks, len(chunks))
			}

			if len(chunks) > 0 {
				lastChunk := chunks[len(chunks)-1]
				if len(lastChunk) != tt.wantLastSize {
					t.Errorf("expected last chunk size %d, got %d", tt.wantLastSize, len(lastChunk))
				}
			}
		})
	}
}

func TestSummarizeChunkPrompt(t *testing.T) {
	evts := []events.Event{
		{ID: "evt-1", Actor: events.ActorUser, Kind: events.KindMessage, Content: "Fix the auth bug"},
		{ID: "evt-2", Actor: events.ActorAgent, Kind: events.KindAction, Content: "Reading auth.go"},
	}

	messages := summarizeChunkPrompt(evts)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Role != "system" {
		t.Errorf("expected system role, got %q", messages[0].Role)
	}
	if messages[1].Role != "user" {
		t.Errorf("expected user role, got %q", messages[1].Role)
	}

	// System prompt should mention JSON schema
	if !strings.Contains(messages[0].Content, "summary") {
		t.Error("system prompt should reference summary field")
	}
	if !strings.Contains(messages[0].Content, "tone") {
		t.Error("system prompt should reference tone field")
	}

	// User content should contain event data
	if !strings.Contains(messages[1].Content, "evt-1") {
		t.Error("user content should contain event IDs")
	}
	if !strings.Contains(messages[1].Content, "Fix the auth bug") {
		t.Error("user content should contain event content")
	}
}

func TestArcSynthesisPrompt(t *testing.T) {
	summaries := []extractChunkSummary{
		{Summary: "Worked on auth", Tone: "neutral", Phase: "building"},
	}

	messages := arcSynthesisPrompt(summaries)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if !strings.Contains(messages[0].Content, "arc") {
		t.Error("system prompt should reference arc field")
	}
	if !strings.Contains(messages[1].Content, "Worked on auth") {
		t.Error("user content should contain summary data")
	}
}

func TestExtractCandidatesPrompt(t *testing.T) {
	evts := []events.Event{
		{ID: "evt-1", Actor: events.ActorUser, Kind: events.KindCorrection, Content: "No, use pathlib instead"},
	}
	arc := &extractArcSummary{Arc: "User refactoring file handling"}

	messages := extractCandidatesPrompt(evts, arc, nil)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if !strings.Contains(messages[0].Content, "candidates") {
		t.Error("system prompt should reference candidates field")
	}
	if !strings.Contains(messages[0].Content, "already_captured") {
		t.Error("system prompt should reference already_captured field")
	}
	if !strings.Contains(messages[1].Content, "evt-1") {
		t.Error("user content should contain event data")
	}
	if !strings.Contains(messages[1].Content, "User refactoring file handling") {
		t.Error("user content should contain arc context")
	}
}

func TestExtractCandidatesPrompt_WithBehaviors(t *testing.T) {
	evts := makeEvents(5)

	// Without behaviors, should not mention "Existing behaviors"
	messages := extractCandidatesPrompt(evts, nil, nil)
	if strings.Contains(messages[1].Content, "Existing behaviors") {
		t.Error("should not include behaviors section when nil")
	}

	// With behaviors, should include them in the prompt
	behaviors := []models.Behavior{
		{ID: "beh-1", Kind: "correction", Content: models.BehaviorContent{Canonical: "Always use parameterized queries"}},
		{ID: "beh-2", Kind: "preference", Content: models.BehaviorContent{Canonical: "Prefer explicit error handling"}},
	}
	messages = extractCandidatesPrompt(evts, nil, behaviors)
	if !strings.Contains(messages[1].Content, "Existing behaviors") {
		t.Error("should include behaviors section when behaviors provided")
	}
	if !strings.Contains(messages[1].Content, "beh-1") {
		t.Error("should include behavior ID beh-1")
	}
	if !strings.Contains(messages[1].Content, "Always use parameterized queries") {
		t.Error("should include behavior content")
	}
	if !strings.Contains(messages[1].Content, "beh-2") {
		t.Error("should include behavior ID beh-2")
	}
}

func TestExtractCandidatesPrompt_ConditionalContext(t *testing.T) {
	evts := makeEvents(5)
	arc := &extractArcSummary{Arc: "Test arc"}
	behaviors := []models.Behavior{
		{ID: "beh-1", Kind: "correction", Content: models.BehaviorContent{Canonical: "test"}},
	}

	// No arc, no behaviors: system prompt should NOT claim context is available
	msgs := extractCandidatesPrompt(evts, nil, nil)
	if strings.Contains(msgs[0].Content, "You have session arc context") {
		t.Error("system prompt should not mention arc context when arc is nil")
	}
	if strings.Contains(msgs[0].Content, "existing behaviors for deduplication") {
		t.Error("system prompt should not mention behaviors when none provided")
	}

	// Arc only
	msgs = extractCandidatesPrompt(evts, arc, nil)
	if !strings.Contains(msgs[0].Content, "session arc context") {
		t.Error("system prompt should mention arc context when arc is provided")
	}
	if strings.Contains(msgs[0].Content, "existing behaviors") {
		t.Error("system prompt should not mention behaviors when none provided")
	}

	// Behaviors only
	msgs = extractCandidatesPrompt(evts, nil, behaviors)
	if strings.Contains(msgs[0].Content, "session arc context") {
		t.Error("system prompt should not mention arc when nil")
	}
	if !strings.Contains(msgs[0].Content, "existing behaviors") {
		t.Error("system prompt should mention behaviors when provided")
	}

	// Both arc and behaviors
	msgs = extractCandidatesPrompt(evts, arc, behaviors)
	if !strings.Contains(msgs[0].Content, "session arc context") {
		t.Error("system prompt should mention arc context when both provided")
	}
	if !strings.Contains(msgs[0].Content, "existing behaviors") {
		t.Error("system prompt should mention behaviors when both provided")
	}
}

func TestBuildSessionContext(t *testing.T) {
	evts := []events.Event{
		{SessionID: "sess-1", ProjectID: "proj-1"},
	}
	ctx := buildSessionContext(evts)
	if ctx["session_id"] != "sess-1" {
		t.Errorf("expected session_id 'sess-1', got %v", ctx["session_id"])
	}
	if ctx["project_id"] != "proj-1" {
		t.Errorf("expected project_id 'proj-1', got %v", ctx["project_id"])
	}

	// With Provenance
	evts = []events.Event{
		{
			SessionID: "sess-2",
			Provenance: &events.EventProvenance{
				Model:       "gpt-4",
				Branch:      "feat/test",
				TaskContext: "implement auth",
			},
		},
	}
	ctx = buildSessionContext(evts)
	if ctx["model"] != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %v", ctx["model"])
	}
	if ctx["branch"] != "feat/test" {
		t.Errorf("expected branch 'feat/test', got %v", ctx["branch"])
	}
	if ctx["task"] != "implement auth" {
		t.Errorf("expected task 'implement auth', got %v", ctx["task"])
	}

	// Empty events → nil
	ctx = buildSessionContext(nil)
	if ctx != nil {
		t.Errorf("expected nil context for nil events, got %v", ctx)
	}

	// Events with no metadata → nil
	ctx = buildSessionContext([]events.Event{{}})
	if ctx != nil {
		t.Errorf("expected nil context for empty event, got %v", ctx)
	}
}

func TestEventIDs(t *testing.T) {
	evts := makeEvents(3)
	ids := eventIDs(evts)
	if len(ids) != 3 {
		t.Fatalf("expected 3 IDs, got %d", len(ids))
	}
	for i, id := range ids {
		expected := fmt.Sprintf("evt-%d", i+1)
		if id != expected {
			t.Errorf("expected %q, got %q", expected, id)
		}
	}
}

func TestLLMExtract_MaxCandidatesCap(t *testing.T) {
	evts := makeEvents(40) // 2 chunks of 20

	// Chunk 0 produces a low-confidence candidate, chunk 1 produces high-confidence
	lowConfResp := `{"candidates": [{"source_events": ["evt-1"], "raw_text": "low conf", "candidate_type": "discovery", "confidence": 0.5, "sentiment": "neutral", "session_phase": "opening", "interaction_pattern": "collaborating", "rationale": "low", "already_captured": false}]}`
	highConfResp := `{"candidates": [{"source_events": ["evt-21"], "raw_text": "high conf", "candidate_type": "correction", "confidence": 0.95, "sentiment": "frustrated", "session_phase": "resolving", "interaction_pattern": "teaching", "rationale": "high", "already_captured": false}]}`

	mock := &mockLLMClient{
		responses: []string{
			makeSummaryResponse(0),
			makeSummaryResponse(1),
			makeArcResponse(),
			lowConfResp,
			highConfResp,
		},
	}

	config := DefaultLLMConsolidatorConfig()
	config.ChunkSize = 20
	config.MaxCandidates = 1
	c := NewLLMConsolidator(mock, nil, config)

	candidates, err := c.Extract(context.Background(), evts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate (capped by MaxCandidates), got %d", len(candidates))
	}

	// Should keep the highest-confidence candidate, not the first by chunk order
	if candidates[0].Confidence != 0.95 {
		t.Errorf("expected highest-confidence candidate (0.95) to be kept, got %f", candidates[0].Confidence)
	}
}

func TestLLMExtract_MinConfidenceFilter(t *testing.T) {
	evts := makeEvents(10)

	// Return a candidate with confidence below the 0.7 threshold
	lowConfResp := `{"candidates": [{"source_events": ["evt-1"], "raw_text": "maybe use pathlib", "candidate_type": "discovery", "confidence": 0.4, "sentiment": "neutral", "session_phase": "exploring", "interaction_pattern": "collaborating", "rationale": "weak signal", "already_captured": false}]}`

	mock := &mockLLMClient{
		responses: []string{
			makeSummaryResponse(0),
			makeArcResponse(),
			lowConfResp,
		},
	}

	config := DefaultLLMConsolidatorConfig()
	config.ChunkSize = 20
	// MinConfidence defaults to 0.7
	c := NewLLMConsolidator(mock, nil, config)

	candidates, err := c.Extract(context.Background(), evts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	// Candidate at 0.4 confidence should be filtered out by MinConfidence=0.7
	if len(candidates) != 0 {
		t.Fatalf("expected 0 candidates (below MinConfidence), got %d", len(candidates))
	}
}

func TestLLMExtract_MinConfidenceDisabled(t *testing.T) {
	evts := makeEvents(10)

	lowConfResp := `{"candidates": [{"source_events": ["evt-1"], "raw_text": "maybe use pathlib", "candidate_type": "discovery", "confidence": 0.3, "sentiment": "neutral", "session_phase": "exploring", "interaction_pattern": "collaborating", "rationale": "weak signal", "already_captured": false}]}`

	mock := &mockLLMClient{
		responses: []string{
			makeSummaryResponse(0),
			makeArcResponse(),
			lowConfResp,
		},
	}

	config := DefaultLLMConsolidatorConfig()
	config.ChunkSize = 20
	config.MinConfidence = 0 // disabled
	c := NewLLMConsolidator(mock, nil, config)

	candidates, err := c.Extract(context.Background(), evts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	// With MinConfidence=0, low-confidence candidates should pass through
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate with MinConfidence disabled, got %d", len(candidates))
	}
}

func TestLLMExtract_EmptyRawTextFiltered(t *testing.T) {
	evts := makeEvents(10)

	emptyRawResp := `{"candidates": [{"source_events": ["evt-1"], "raw_text": "  ", "candidate_type": "correction", "confidence": 0.9, "sentiment": "neutral", "session_phase": "building", "interaction_pattern": "collaborating", "rationale": "test", "already_captured": false}]}`

	mock := &mockLLMClient{
		responses: []string{
			makeSummaryResponse(0),
			makeArcResponse(),
			emptyRawResp,
		},
	}

	config := DefaultLLMConsolidatorConfig()
	config.ChunkSize = 20
	c := NewLLMConsolidator(mock, nil, config)

	candidates, err := c.Extract(context.Background(), evts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if len(candidates) != 0 {
		t.Fatalf("expected 0 candidates (empty raw_text), got %d", len(candidates))
	}
}

func TestLLMExtract_ConfidenceClamping(t *testing.T) {
	evts := makeEvents(10)

	// Return a candidate with out-of-bounds confidence
	outOfBoundsResp := `{"candidates": [{"source_events": ["evt-1"], "raw_text": "test", "candidate_type": "correction", "confidence": 1.5, "sentiment": "neutral", "session_phase": "building", "interaction_pattern": "collaborating", "rationale": "test", "already_captured": false}]}`

	mock := &mockLLMClient{
		responses: []string{
			makeSummaryResponse(0),
			makeArcResponse(),
			outOfBoundsResp,
		},
	}

	config := DefaultLLMConsolidatorConfig()
	config.ChunkSize = 20
	c := NewLLMConsolidator(mock, nil, config)

	candidates, err := c.Extract(context.Background(), evts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Confidence != 1.0 {
		t.Errorf("expected confidence clamped to 1.0, got %f", candidates[0].Confidence)
	}
}

func TestLLMExtract_Pass1PartialFailure(t *testing.T) {
	evts := makeEvents(40) // 2 chunks of 20

	// Chunk 0 summarization fails, chunk 1 succeeds → arc from partial summaries
	mock := &mockLLMClient{
		responses: []string{
			"",                     // Pass 1 chunk 0: will error
			makeSummaryResponse(1), // Pass 1 chunk 1: succeeds
			makeArcResponse(),      // Pass 2: arc from chunk 1 only
			makeExtractResponse([]string{"evt-1"}, false),  // Pass 3 chunk 0
			makeExtractResponse([]string{"evt-21"}, false), // Pass 3 chunk 1
		},
		errs: []error{
			errors.New("chunk 0 API error"), // Pass 1 chunk 0 fails
			nil,                             // Pass 1 chunk 1 succeeds
			nil,                             // Pass 2 succeeds
			nil,                             // Pass 3 chunk 0 succeeds
			nil,                             // Pass 3 chunk 1 succeeds
		},
	}

	config := DefaultLLMConsolidatorConfig()
	config.ChunkSize = 20
	c := NewLLMConsolidator(mock, nil, config)

	candidates, err := c.Extract(context.Background(), evts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	// Both chunks should produce candidates via Pass 3
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	// 5 calls: 2 summarize (1 failed) + 1 arc + 2 extract
	if mock.callIndex != 5 {
		t.Errorf("expected 5 LLM calls, got %d", mock.callIndex)
	}
}
