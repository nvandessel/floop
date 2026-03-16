package consolidation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/models"
)

// mockLLMClient is a test double for llm.Client.
type mockLLMClient struct {
	responses []string
	errors    []error
	calls     int
}

func (m *mockLLMClient) Complete(_ context.Context, _ []llm.Message) (string, error) {
	idx := m.calls
	m.calls++
	if idx < len(m.errors) && m.errors[idx] != nil {
		return "", m.errors[idx]
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return "", fmt.Errorf("no more mock responses (call %d)", idx)
}

func (m *mockLLMClient) Available() bool { return true }

// makeCandidates creates n test candidates.
func makeCandidates(n int) []Candidate {
	candidates := make([]Candidate, n)
	for i := range candidates {
		candidates[i] = Candidate{
			SourceEvents:  []string{fmt.Sprintf("evt-%d", i)},
			RawText:       fmt.Sprintf("Test candidate %d raw text content here", i),
			CandidateType: "correction",
			Confidence:    0.7,
			SessionContext: map[string]any{
				"session_id": "sess-1",
				"project_id": "proj-1",
			},
		}
	}
	return candidates
}

// makeClassifyResponse builds a valid JSON response for n candidates.
func makeClassifyResponse(candidates []Candidate) string {
	entries := make([]classifiedEntry, len(candidates))
	for i, c := range candidates {
		entries[i] = classifiedEntry{
			SourceEvents: c.SourceEvents,
			Kind:         "directive",
			MemoryType:   "semantic",
			Scope:        "universal",
			Importance:   0.8,
			Content: classifiedContent{
				Canonical: fmt.Sprintf("Canonical form for candidate %d", i),
				Summary:   fmt.Sprintf("Summary for candidate %d", i),
				Tags:      []string{"testing", "go"},
			},
			Rationale: "Test rationale",
		}
	}
	resp := classifiedResponse{Classified: entries}
	data, _ := json.Marshal(resp)
	return string(data)
}

func newTestLLMConsolidator(client llm.Client) *LLMConsolidator {
	return NewLLMConsolidator(client, nil, DefaultLLMConsolidatorConfig())
}

func TestLLMClassify_SingleBatch(t *testing.T) {
	candidates := makeCandidates(5)
	response := makeClassifyResponse(candidates)

	client := &mockLLMClient{responses: []string{response}}
	c := newTestLLMConsolidator(client)

	memories, err := c.Classify(context.Background(), candidates)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	if len(memories) != 5 {
		t.Fatalf("expected 5 memories, got %d", len(memories))
	}

	if client.calls != 1 {
		t.Errorf("expected 1 LLM call, got %d", client.calls)
	}

	for i, mem := range memories {
		if mem.Kind != models.BehaviorKindDirective {
			t.Errorf("memory[%d]: expected directive, got %q", i, mem.Kind)
		}
		if mem.MemoryType != models.MemoryTypeSemantic {
			t.Errorf("memory[%d]: expected semantic, got %q", i, mem.MemoryType)
		}
		if mem.Content.Canonical == mem.RawText {
			t.Errorf("memory[%d]: canonical should differ from raw text", i)
		}
		if mem.Scope != "universal" {
			t.Errorf("memory[%d]: expected universal scope, got %q", i, mem.Scope)
		}
		if mem.Importance != 0.8 {
			t.Errorf("memory[%d]: expected importance 0.8, got %f", i, mem.Importance)
		}
	}
}

func TestLLMClassify_MultiBatch(t *testing.T) {
	// >30 candidates triggers batching with default MaxCandidates=20
	// 35 candidates = 2 batches (20 + 15)
	candidates := makeCandidates(35)
	batch1 := makeClassifyResponse(candidates[:20])
	batch2 := makeClassifyResponse(candidates[20:])

	client := &mockLLMClient{responses: []string{batch1, batch2}}
	c := newTestLLMConsolidator(client)

	memories, err := c.Classify(context.Background(), candidates)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	if len(memories) != 35 {
		t.Fatalf("expected 35 memories, got %d", len(memories))
	}

	if client.calls != 2 {
		t.Errorf("expected 2 LLM calls for batching, got %d", client.calls)
	}
}

func TestLLMClassify_LLMFailure(t *testing.T) {
	candidates := makeCandidates(5)

	client := &mockLLMClient{
		errors: []error{fmt.Errorf("API rate limit exceeded")},
	}
	c := newTestLLMConsolidator(client)

	memories, err := c.Classify(context.Background(), candidates)
	if err != nil {
		t.Fatalf("Classify should fallback on LLM error, got: %v", err)
	}

	if len(memories) != 5 {
		t.Fatalf("expected 5 memories from fallback, got %d", len(memories))
	}

	// Verify fallback produces heuristic-style results
	for _, mem := range memories {
		if mem.Kind != models.BehaviorKindDirective {
			t.Errorf("fallback: expected directive for correction, got %q", mem.Kind)
		}
	}
}

func TestLLMClassify_BadJSON(t *testing.T) {
	candidates := makeCandidates(3)
	validResponse := makeClassifyResponse(candidates)

	// First call returns bad JSON, retry returns valid response
	client := &mockLLMClient{
		responses: []string{"not valid json at all", validResponse},
	}
	c := newTestLLMConsolidator(client)

	memories, err := c.Classify(context.Background(), candidates)
	if err != nil {
		t.Fatalf("Classify should retry and succeed, got: %v", err)
	}

	if len(memories) != 3 {
		t.Fatalf("expected 3 memories after retry, got %d", len(memories))
	}

	if client.calls != 2 {
		t.Errorf("expected 2 LLM calls (initial + retry), got %d", client.calls)
	}
}

func TestLLMClassify_BadJSON_ThenFailure(t *testing.T) {
	candidates := makeCandidates(3)

	// Both calls return bad JSON -> fallback to heuristic
	client := &mockLLMClient{
		responses: []string{"bad json", "still bad json"},
	}
	c := newTestLLMConsolidator(client)

	memories, err := c.Classify(context.Background(), candidates)
	if err != nil {
		t.Fatalf("Classify should fallback after double failure, got: %v", err)
	}

	if len(memories) != 3 {
		t.Fatalf("expected 3 memories from fallback, got %d", len(memories))
	}
}

func TestLLMClassify_InvalidEnums(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		memType string
		wantErr string
	}{
		{"bad kind", "invalid_kind", "semantic", "invalid kind"},
		{"bad memory_type", "directive", "invalid_type", "invalid memory_type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates := makeCandidates(1)
			resp := classifiedResponse{
				Classified: []classifiedEntry{{
					SourceEvents: []string{"evt-0"},
					Kind:         tt.kind,
					MemoryType:   tt.memType,
					Scope:        "universal",
					Importance:   0.5,
					Content: classifiedContent{
						Canonical: "test canonical",
						Summary:   "test summary",
						Tags:      []string{"test"},
					},
				}},
			}
			data, _ := json.Marshal(resp)

			_, err := ParseClassifiedMemories(string(data), candidates)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestLLMClassify_ImportanceValidation(t *testing.T) {
	tests := []struct {
		name       string
		importance float64
		wantErr    bool
	}{
		{"valid low", 0.0, false},
		{"valid high", 1.0, false},
		{"valid mid", 0.5, false},
		{"too low", -0.1, true},
		{"too high", 1.1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates := makeCandidates(1)
			resp := classifiedResponse{
				Classified: []classifiedEntry{{
					SourceEvents: []string{"evt-0"},
					Kind:         "directive",
					MemoryType:   "semantic",
					Scope:        "universal",
					Importance:   tt.importance,
					Content: classifiedContent{
						Canonical: "test canonical",
						Summary:   "test summary",
						Tags:      []string{"test"},
					},
				}},
			}
			data, _ := json.Marshal(resp)

			_, err := ParseClassifiedMemories(string(data), candidates)
			if (err != nil) != tt.wantErr {
				t.Errorf("importance %f: error = %v, wantErr %v", tt.importance, err, tt.wantErr)
			}
		})
	}
}

func TestLLMClassify_WorkflowDetection(t *testing.T) {
	candidates := []Candidate{{
		SourceEvents:  []string{"evt-wf"},
		RawText:       "First build the binary, then run tests, then push to CI",
		CandidateType: "workflow",
		Confidence:    0.6,
		SessionContext: map[string]any{
			"session_id": "sess-1",
		},
	}}

	resp := classifiedResponse{
		Classified: []classifiedEntry{{
			SourceEvents: []string{"evt-wf"},
			Kind:         "workflow",
			MemoryType:   "procedural",
			Scope:        "universal",
			Importance:   0.7,
			Content: classifiedContent{
				Canonical: "Build, test, then push deployment workflow",
				Summary:   "Build-test-push workflow",
				Tags:      []string{"deployment", "ci"},
			},
			WorkflowData: &workflowDataJSON{
				Steps: []workflowStepJSON{
					{Action: "Build binary", Condition: "source changed"},
					{Action: "Run tests", OnFailure: "Abort deployment"},
					{Action: "Push to CI"},
				},
				Trigger:  "code change",
				Verified: false,
			},
		}},
	}
	data, _ := json.Marshal(resp)

	client := &mockLLMClient{responses: []string{string(data)}}
	c := newTestLLMConsolidator(client)

	memories, err := c.Classify(context.Background(), candidates)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}

	mem := memories[0]
	if mem.Kind != models.BehaviorKindWorkflow {
		t.Errorf("expected workflow kind, got %q", mem.Kind)
	}
	if mem.MemoryType != models.MemoryTypeProcedural {
		t.Errorf("expected procedural memory type, got %q", mem.MemoryType)
	}
	if mem.WorkflowData == nil {
		t.Fatal("expected WorkflowData to be populated")
	}
	if len(mem.WorkflowData.Steps) != 3 {
		t.Errorf("expected 3 workflow steps, got %d", len(mem.WorkflowData.Steps))
	}
	if mem.WorkflowData.Trigger != "code change" {
		t.Errorf("expected trigger 'code change', got %q", mem.WorkflowData.Trigger)
	}
}

func TestLLMClassify_EpisodicDetection(t *testing.T) {
	candidates := []Candidate{{
		SourceEvents:  []string{"evt-ep"},
		RawText:       "The deployment failed because we forgot to update the schema",
		CandidateType: "failure",
		Confidence:    0.6,
		SessionContext: map[string]any{
			"session_id": "sess-42",
		},
	}}

	resp := classifiedResponse{
		Classified: []classifiedEntry{{
			SourceEvents: []string{"evt-ep"},
			Kind:         "episodic",
			MemoryType:   "episodic",
			Scope:        "project:myorg/myrepo",
			Importance:   0.75,
			Content: classifiedContent{
				Canonical: "Deployment failed due to missing schema migration",
				Summary:   "Deploy failure: schema not updated",
				Tags:      []string{"deployment", "schema", "failure"},
			},
			EpisodeData: &episodeDataJSON{
				SessionID: "sess-42",
				Timeframe: "2024-01-15",
				Actors:    []string{"user", "ci"},
				Outcome:   "failure",
			},
		}},
	}
	data, _ := json.Marshal(resp)

	client := &mockLLMClient{responses: []string{string(data)}}
	c := newTestLLMConsolidator(client)

	memories, err := c.Classify(context.Background(), candidates)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}

	mem := memories[0]
	if mem.Kind != models.BehaviorKindEpisodic {
		t.Errorf("expected episodic kind, got %q", mem.Kind)
	}
	if mem.MemoryType != models.MemoryTypeEpisodic {
		t.Errorf("expected episodic memory type, got %q", mem.MemoryType)
	}
	if mem.EpisodeData == nil {
		t.Fatal("expected EpisodeData to be populated")
	}
	if mem.EpisodeData.SessionID != "sess-42" {
		t.Errorf("expected session_id 'sess-42', got %q", mem.EpisodeData.SessionID)
	}
	if mem.EpisodeData.Outcome != "failure" {
		t.Errorf("expected outcome 'failure', got %q", mem.EpisodeData.Outcome)
	}
}

func TestLLMClassify_ScopeInference(t *testing.T) {
	candidates := []Candidate{{
		SourceEvents:  []string{"evt-scope"},
		RawText:       "In our CI pipeline, always run linting before tests",
		CandidateType: "correction",
		Confidence:    0.7,
		SessionContext: map[string]any{
			"session_id": "sess-1",
			"project_id": "myorg/myrepo",
		},
	}}

	resp := classifiedResponse{
		Classified: []classifiedEntry{{
			SourceEvents: []string{"evt-scope"},
			Kind:         "directive",
			MemoryType:   "semantic",
			Scope:        "project:myorg/myrepo",
			Importance:   0.8,
			Content: classifiedContent{
				Canonical: "Run linting before tests in CI",
				Summary:   "Lint before test in CI",
				Tags:      []string{"ci", "linting", "testing"},
			},
		}},
	}
	data, _ := json.Marshal(resp)

	client := &mockLLMClient{responses: []string{string(data)}}
	c := newTestLLMConsolidator(client)

	memories, err := c.Classify(context.Background(), candidates)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	if memories[0].Scope != "project:myorg/myrepo" {
		t.Errorf("expected project scope, got %q", memories[0].Scope)
	}
}

func TestLLMClassify_EmptyCandidates(t *testing.T) {
	client := &mockLLMClient{}
	c := newTestLLMConsolidator(client)

	memories, err := c.Classify(context.Background(), nil)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}
	if memories != nil {
		t.Errorf("expected nil for empty candidates, got %v", memories)
	}
	if client.calls != 0 {
		t.Errorf("expected 0 LLM calls for empty input, got %d", client.calls)
	}
}

func TestLLMClassify_SummaryTruncation(t *testing.T) {
	candidates := makeCandidates(1)
	longSummary := strings.Repeat("x", 80) // 80 chars, should be truncated to 60

	resp := classifiedResponse{
		Classified: []classifiedEntry{{
			SourceEvents: []string{"evt-0"},
			Kind:         "directive",
			MemoryType:   "semantic",
			Scope:        "universal",
			Importance:   0.5,
			Content: classifiedContent{
				Canonical: "Some canonical form",
				Summary:   longSummary,
				Tags:      []string{"test"},
			},
		}},
	}
	data, _ := json.Marshal(resp)

	memories, err := ParseClassifiedMemories(string(data), candidates)
	if err != nil {
		t.Fatalf("ParseClassifiedMemories returned error: %v", err)
	}

	if len([]rune(memories[0].Content.Summary)) > 60 {
		t.Errorf("summary should be truncated to 60 chars, got %d", len([]rune(memories[0].Content.Summary)))
	}
}

func TestLLMClassify_EmptyCanonical(t *testing.T) {
	candidates := makeCandidates(1)
	resp := classifiedResponse{
		Classified: []classifiedEntry{{
			SourceEvents: []string{"evt-0"},
			Kind:         "directive",
			MemoryType:   "semantic",
			Scope:        "universal",
			Importance:   0.5,
			Content: classifiedContent{
				Canonical: "",
				Summary:   "test",
				Tags:      []string{"test"},
			},
		}},
	}
	data, _ := json.Marshal(resp)

	_, err := ParseClassifiedMemories(string(data), candidates)
	if err == nil {
		t.Fatal("expected error for empty canonical")
	}
	if !strings.Contains(err.Error(), "canonical is empty") {
		t.Errorf("expected canonical error, got %q", err.Error())
	}
}

func TestLLMClassify_CodeFenceStripping(t *testing.T) {
	candidates := makeCandidates(1)
	inner := makeClassifyResponse(candidates)
	wrapped := "```json\n" + inner + "\n```"

	memories, err := ParseClassifiedMemories(wrapped, candidates)
	if err != nil {
		t.Fatalf("ParseClassifiedMemories should strip code fences, got: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
}

func TestClassifyCandidatesPrompt(t *testing.T) {
	candidates := makeCandidates(3)
	msgs := ClassifyCandidatesPrompt(candidates)

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}

	if msgs[0].Role != "system" {
		t.Errorf("first message should be system, got %q", msgs[0].Role)
	}
	if msgs[1].Role != "user" {
		t.Errorf("second message should be user, got %q", msgs[1].Role)
	}

	// System prompt should contain taxonomy
	if !strings.Contains(msgs[0].Content, "directive") {
		t.Error("system prompt should contain taxonomy")
	}

	// User prompt should contain candidate count
	if !strings.Contains(msgs[1].Content, "3 candidate") {
		t.Error("user prompt should mention candidate count")
	}
}

func TestBatchCandidates(t *testing.T) {
	tests := []struct {
		name      string
		count     int
		maxSize   int
		wantBatch int
	}{
		{"small set no batching", 10, 20, 1},
		{"at threshold no batching", 30, 20, 1},
		{"over threshold", 31, 20, 2},
		{"large set", 45, 20, 3},
		{"zero maxSize defaults", 10, 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates := makeCandidates(tt.count)
			batches := batchCandidates(candidates, tt.maxSize)
			if len(batches) != tt.wantBatch {
				t.Errorf("expected %d batches, got %d", tt.wantBatch, len(batches))
			}

			// Verify all candidates are accounted for
			total := 0
			for _, b := range batches {
				total += len(b)
			}
			if total != tt.count {
				t.Errorf("expected %d total candidates across batches, got %d", tt.count, total)
			}
		})
	}
}

func TestLLMClassify_AllKinds(t *testing.T) {
	kinds := []struct {
		kind    string
		memType string
		want    models.BehaviorKind
	}{
		{"directive", "semantic", models.BehaviorKindDirective},
		{"constraint", "semantic", models.BehaviorKindConstraint},
		{"procedure", "procedural", models.BehaviorKindProcedure},
		{"preference", "semantic", models.BehaviorKindPreference},
		{"episodic", "episodic", models.BehaviorKindEpisodic},
		{"workflow", "procedural", models.BehaviorKindWorkflow},
	}

	for _, tt := range kinds {
		t.Run(tt.kind, func(t *testing.T) {
			candidates := makeCandidates(1)
			resp := classifiedResponse{
				Classified: []classifiedEntry{{
					SourceEvents: []string{"evt-0"},
					Kind:         tt.kind,
					MemoryType:   tt.memType,
					Scope:        "universal",
					Importance:   0.5,
					Content: classifiedContent{
						Canonical: "Test canonical for " + tt.kind,
						Summary:   "Test summary",
						Tags:      []string{"test"},
					},
				}},
			}
			data, _ := json.Marshal(resp)

			memories, err := ParseClassifiedMemories(string(data), candidates)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if memories[0].Kind != tt.want {
				t.Errorf("expected kind %q, got %q", tt.want, memories[0].Kind)
			}
		})
	}
}

func TestLLMClassify_MultiBatchPartialFailure(t *testing.T) {
	// 35 candidates = 2 batches; first batch LLM fails, second succeeds
	candidates := makeCandidates(35)
	batch2Response := makeClassifyResponse(candidates[20:])

	client := &mockLLMClient{
		responses: []string{"", batch2Response},
		errors:    []error{fmt.Errorf("timeout"), nil},
	}
	c := newTestLLMConsolidator(client)

	memories, err := c.Classify(context.Background(), candidates)
	if err != nil {
		t.Fatalf("Classify should handle partial failure with fallback, got: %v", err)
	}

	if len(memories) != 35 {
		t.Fatalf("expected 35 memories (20 fallback + 15 LLM), got %d", len(memories))
	}

	// First 20 should be heuristic fallback (directive for correction type)
	for i := 0; i < 20; i++ {
		if memories[i].Kind != models.BehaviorKindDirective {
			t.Errorf("memory[%d]: expected fallback directive, got %q", i, memories[i].Kind)
		}
	}
}
