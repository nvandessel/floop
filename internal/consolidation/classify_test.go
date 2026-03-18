package consolidation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nvandessel/floop/internal/models"
)

// makeClassifyResponse builds a valid JSON response for n candidates.
func makeClassifyResponse(candidates []Candidate) string {
	return makeClassifyResponseWithOffset(candidates, 0)
}

// makeClassifyResponseWithOffset builds a valid JSON response with index offset.
func makeClassifyResponseWithOffset(candidates []Candidate, offset int) string {
	entries := make([]classifiedEntry, len(candidates))
	for i, c := range candidates {
		entries[i] = classifiedEntry{
			Index:        offset + i,
			SourceEvents: c.SourceEvents,
			Kind:         "directive",
			MemoryType:   "semantic",
			Scope:        "universal",
			Importance:   0.8,
			Content: classifiedContent{
				Canonical: fmt.Sprintf("Canonical form for candidate %d", offset+i),
				Summary:   fmt.Sprintf("Summary for candidate %d", offset+i),
				Tags:      []string{"testing", "go"},
			},
			Rationale: "Test rationale",
		}
	}
	resp := classifiedResponse{Classified: entries}
	data, _ := json.Marshal(resp)
	return string(data)
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

	if client.callIndex != 1 {
		t.Errorf("expected 1 LLM call, got %d", client.callIndex)
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
	// Use MaxCandidates=20 so threshold=30, and 35 candidates triggers 2 batches (20+15)
	candidates := makeCandidates(35)
	batch1 := makeClassifyResponse(candidates[:20])
	batch2 := makeClassifyResponseWithOffset(candidates[20:], 0)

	client := &mockLLMClient{responses: []string{batch1, batch2}}
	cfg := DefaultLLMConsolidatorConfig()
	cfg.MaxCandidates = 20
	c := newTestLLMConsolidatorWithConfig(client, cfg)

	memories, err := c.Classify(context.Background(), candidates)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	if len(memories) != 35 {
		t.Fatalf("expected 35 memories, got %d", len(memories))
	}

	if client.callIndex != 2 {
		t.Errorf("expected 2 LLM calls for batching, got %d", client.callIndex)
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

	if client.callIndex != 2 {
		t.Errorf("expected 2 LLM calls (initial + retry), got %d", client.callIndex)
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
					Index:        0,
					SourceEvents: []string{"evt-0"},
					Kind:         tt.kind,
					MemoryType:   tt.memType,
					Scope:        "universal",
					Importance:   0.5,
					Content: classifiedContent{
						Canonical: "test canonical",
						Summary:   "test summary",
						Tags:      []string{"test", "enum"},
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

func TestLLMClassify_CaseInsensitiveEnums(t *testing.T) {
	candidates := makeCandidates(1)
	resp := classifiedResponse{
		Classified: []classifiedEntry{{
			Index:        0,
			SourceEvents: []string{"evt-0"},
			Kind:         "Directive",
			MemoryType:   "Semantic",
			Scope:        "universal",
			Importance:   0.5,
			Content: classifiedContent{
				Canonical: "test canonical",
				Summary:   "test summary",
				Tags:      []string{"test", "case"},
			},
		}},
	}
	data, _ := json.Marshal(resp)

	memories, err := ParseClassifiedMemories(string(data), candidates)
	if err != nil {
		t.Fatalf("case-insensitive parsing should succeed: %v", err)
	}
	if memories[0].Kind != models.BehaviorKindDirective {
		t.Errorf("expected directive, got %q", memories[0].Kind)
	}
	if memories[0].MemoryType != models.MemoryTypeSemantic {
		t.Errorf("expected semantic, got %q", memories[0].MemoryType)
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
					Index:        0,
					SourceEvents: []string{"evt-0"},
					Kind:         "directive",
					MemoryType:   "semantic",
					Scope:        "universal",
					Importance:   tt.importance,
					Content: classifiedContent{
						Canonical: "test canonical",
						Summary:   "test summary",
						Tags:      []string{"test", "importance"},
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

func TestLLMClassify_KindMemoryTypeCrossValidation(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		memType string
		wantErr bool
	}{
		{"directive+semantic", "directive", "semantic", false},
		{"constraint+semantic", "constraint", "semantic", false},
		{"preference+semantic", "preference", "semantic", false},
		{"procedure+procedural", "procedure", "procedural", false},
		{"workflow+procedural", "workflow", "procedural", false},
		{"episodic+episodic", "episodic", "episodic", false},
		{"directive+episodic mismatch", "directive", "episodic", true},
		{"workflow+semantic mismatch", "workflow", "semantic", true},
		{"episodic+procedural mismatch", "episodic", "procedural", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates := makeCandidates(1)
			entry := classifiedEntry{
				Index:        0,
				SourceEvents: []string{"evt-0"},
				Kind:         tt.kind,
				MemoryType:   tt.memType,
				Scope:        "universal",
				Importance:   0.5,
				Content: classifiedContent{
					Canonical: "test canonical",
					Summary:   "test summary",
					Tags:      []string{"test", "cross"},
				},
			}
			// Add required structured data for episodic/workflow
			if tt.kind == "episodic" {
				entry.EpisodeData = &episodeDataJSON{
					SessionID: "sess-1", Timeframe: "2024-01-15",
					Actors: []string{"user"}, Outcome: "success",
				}
			}
			if tt.kind == "workflow" {
				entry.WorkflowData = &workflowDataJSON{
					Steps:   []workflowStepJSON{{Action: "step1"}},
					Trigger: "manual", Verified: false,
				}
			}
			resp := classifiedResponse{Classified: []classifiedEntry{entry}}
			data, _ := json.Marshal(resp)

			_, err := ParseClassifiedMemories(string(data), candidates)
			if (err != nil) != tt.wantErr {
				t.Errorf("kind=%s memType=%s: error = %v, wantErr %v", tt.kind, tt.memType, err, tt.wantErr)
			}
		})
	}
}

func TestLLMClassify_ScopeValidation(t *testing.T) {
	tests := []struct {
		name    string
		scope   string
		wantErr bool
	}{
		{"universal", "universal", false},
		{"project scope", "project:myorg/myrepo", false},
		{"empty defaults to universal", "", false},
		{"invalid scope", "org-wide", true},
		{"bare project", "project", true},
		{"project colon empty namespace", "project:", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates := makeCandidates(1)
			resp := classifiedResponse{
				Classified: []classifiedEntry{{
					Index:        0,
					SourceEvents: []string{"evt-0"},
					Kind:         "directive",
					MemoryType:   "semantic",
					Scope:        tt.scope,
					Importance:   0.5,
					Content: classifiedContent{
						Canonical: "test canonical",
						Summary:   "test summary",
						Tags:      []string{"test", "scope"},
					},
				}},
			}
			data, _ := json.Marshal(resp)

			_, err := ParseClassifiedMemories(string(data), candidates)
			if (err != nil) != tt.wantErr {
				t.Errorf("scope %q: error = %v, wantErr %v", tt.scope, err, tt.wantErr)
			}
		})
	}
}

func TestLLMClassify_TagCountValidation(t *testing.T) {
	tests := []struct {
		name    string
		tags    []string
		wantErr bool
	}{
		{"2 tags (min)", []string{"a", "b"}, false},
		{"5 tags (max)", []string{"a", "b", "c", "d", "e"}, false},
		{"3 tags", []string{"a", "b", "c"}, false},
		{"0 tags", []string{}, true},
		{"1 tag", []string{"a"}, true},
		{"6 tags", []string{"a", "b", "c", "d", "e", "f"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates := makeCandidates(1)
			resp := classifiedResponse{
				Classified: []classifiedEntry{{
					Index:        0,
					SourceEvents: []string{"evt-0"},
					Kind:         "directive",
					MemoryType:   "semantic",
					Scope:        "universal",
					Importance:   0.5,
					Content: classifiedContent{
						Canonical: "test canonical",
						Summary:   "test summary",
						Tags:      tt.tags,
					},
				}},
			}
			data, _ := json.Marshal(resp)

			_, err := ParseClassifiedMemories(string(data), candidates)
			if (err != nil) != tt.wantErr {
				t.Errorf("tags %v: error = %v, wantErr %v", tt.tags, err, tt.wantErr)
			}
		})
	}
}

func TestLLMClassify_StructuredDataEnforcement(t *testing.T) {
	t.Run("episodic without episode_data", func(t *testing.T) {
		candidates := makeCandidates(1)
		resp := classifiedResponse{
			Classified: []classifiedEntry{{
				Index:        0,
				SourceEvents: []string{"evt-0"},
				Kind:         "episodic",
				MemoryType:   "episodic",
				Scope:        "universal",
				Importance:   0.5,
				Content: classifiedContent{
					Canonical: "test canonical",
					Summary:   "test summary",
					Tags:      []string{"test", "episodic"},
				},
				EpisodeData: nil,
			}},
		}
		data, _ := json.Marshal(resp)

		_, err := ParseClassifiedMemories(string(data), candidates)
		if err == nil {
			t.Fatal("expected error for episodic without episode_data")
		}
		if !strings.Contains(err.Error(), "requires episode_data") {
			t.Errorf("expected episode_data error, got %q", err.Error())
		}
	})

	t.Run("workflow without workflow_data", func(t *testing.T) {
		candidates := makeCandidates(1)
		resp := classifiedResponse{
			Classified: []classifiedEntry{{
				Index:        0,
				SourceEvents: []string{"evt-0"},
				Kind:         "workflow",
				MemoryType:   "procedural",
				Scope:        "universal",
				Importance:   0.5,
				Content: classifiedContent{
					Canonical: "test canonical",
					Summary:   "test summary",
					Tags:      []string{"test", "workflow"},
				},
				WorkflowData: nil,
			}},
		}
		data, _ := json.Marshal(resp)

		_, err := ParseClassifiedMemories(string(data), candidates)
		if err == nil {
			t.Fatal("expected error for workflow without workflow_data")
		}
		if !strings.Contains(err.Error(), "requires workflow_data") {
			t.Errorf("expected workflow_data error, got %q", err.Error())
		}
	})
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
			Index:        0,
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
			Index:        0,
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
			Index:        0,
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
	if client.callIndex != 0 {
		t.Errorf("expected 0 LLM calls for empty input, got %d", client.callIndex)
	}
}

func TestLLMClassify_SummaryTruncation(t *testing.T) {
	candidates := makeCandidates(1)
	longSummary := strings.Repeat("x", 80) // 80 chars, should be truncated to 60

	resp := classifiedResponse{
		Classified: []classifiedEntry{{
			Index:        0,
			SourceEvents: []string{"evt-0"},
			Kind:         "directive",
			MemoryType:   "semantic",
			Scope:        "universal",
			Importance:   0.5,
			Content: classifiedContent{
				Canonical: "Some canonical form",
				Summary:   longSummary,
				Tags:      []string{"test", "truncation"},
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
			Index:        0,
			SourceEvents: []string{"evt-0"},
			Kind:         "directive",
			MemoryType:   "semantic",
			Scope:        "universal",
			Importance:   0.5,
			Content: classifiedContent{
				Canonical: "",
				Summary:   "test",
				Tags:      []string{"test", "empty"},
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

	tests := []struct {
		name    string
		wrapped string
	}{
		{"lowercase json", "```json\n" + inner + "\n```"},
		{"uppercase JSON", "```JSON\n" + inner + "\n```"},
		{"mixed case Json", "```Json\n" + inner + "\n```"},
		{"no language tag", "```\n" + inner + "\n```"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memories, err := ParseClassifiedMemories(tt.wrapped, candidates)
			if err != nil {
				t.Fatalf("ParseClassifiedMemories should strip code fences, got: %v", err)
			}
			if len(memories) != 1 {
				t.Fatalf("expected 1 memory, got %d", len(memories))
			}
		})
	}
}

func TestClassifyCandidatesPrompt(t *testing.T) {
	candidates := makeCandidates(3)
	msgs, err := ClassifyCandidatesPrompt(candidates)
	if err != nil {
		t.Fatalf("ClassifyCandidatesPrompt returned error: %v", err)
	}

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
			entry := classifiedEntry{
				Index:        0,
				SourceEvents: []string{"evt-0"},
				Kind:         tt.kind,
				MemoryType:   tt.memType,
				Scope:        "universal",
				Importance:   0.5,
				Content: classifiedContent{
					Canonical: "Test canonical for " + tt.kind,
					Summary:   "Test summary",
					Tags:      []string{"test", tt.kind},
				},
			}
			// Add required structured data
			if tt.kind == "episodic" {
				entry.EpisodeData = &episodeDataJSON{
					SessionID: "sess-1", Timeframe: "2024-01-15",
					Actors: []string{"user"}, Outcome: "success",
				}
			}
			if tt.kind == "workflow" {
				entry.WorkflowData = &workflowDataJSON{
					Steps:   []workflowStepJSON{{Action: "step1"}},
					Trigger: "manual", Verified: false,
				}
			}
			resp := classifiedResponse{Classified: []classifiedEntry{entry}}
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
	// Use MaxCandidates=20 so threshold=30, and 35 candidates triggers 2 batches
	// First batch LLM fails (fallback to heuristic), second batch succeeds via LLM
	candidates := makeCandidates(35)
	batch2Response := makeClassifyResponseWithOffset(candidates[20:], 0)

	client := &mockLLMClient{
		responses: []string{"", batch2Response},
		errors:    []error{fmt.Errorf("timeout"), nil},
	}
	cfg := DefaultLLMConsolidatorConfig()
	cfg.MaxCandidates = 20
	c := newTestLLMConsolidatorWithConfig(client, cfg)

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

	// Should have had 2 LLM calls: batch1 (failed) + batch2 (succeeded)
	if client.callIndex != 2 {
		t.Errorf("expected 2 LLM calls, got %d", client.callIndex)
	}
}

func TestLLMClassify_ContextCancellation(t *testing.T) {
	candidates := makeCandidates(35)
	cfg := DefaultLLMConsolidatorConfig()
	cfg.MaxCandidates = 20

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	client := &mockLLMClient{}
	c := newTestLLMConsolidatorWithConfig(client, cfg)

	_, err := c.Classify(ctx, candidates)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("expected cancellation error, got %q", err.Error())
	}
	if client.callIndex != 0 {
		t.Errorf("expected 0 LLM calls after cancellation, got %d", client.callIndex)
	}
}

func TestLLMClassify_SourceEventsMismatch(t *testing.T) {
	candidates := makeCandidates(1)
	resp := classifiedResponse{
		Classified: []classifiedEntry{{
			Index:        0,
			SourceEvents: []string{"evt-unknown"},
			Kind:         "directive",
			MemoryType:   "semantic",
			Scope:        "universal",
			Importance:   0.5,
			Content: classifiedContent{
				Canonical: "test canonical",
				Summary:   "test summary",
				Tags:      []string{"test", "mismatch"},
			},
		}},
	}
	data, _ := json.Marshal(resp)

	_, err := ParseClassifiedMemories(string(data), candidates)
	if err == nil {
		t.Fatal("expected error for source_events mismatch")
	}
	if !strings.Contains(err.Error(), "not found in input candidates") {
		t.Errorf("expected source_events mismatch error, got %q", err.Error())
	}
}

func TestClassifyCandidatesPrompt_MarshalError(t *testing.T) {
	// Create candidate with non-serializable context
	candidates := []Candidate{{
		SourceEvents:   []string{"evt-0"},
		RawText:        "test",
		CandidateType:  "correction",
		Confidence:     0.7,
		SessionContext: map[string]any{"bad": make(chan int)},
	}}

	_, err := ClassifyCandidatesPrompt(candidates)
	if err == nil {
		t.Fatal("expected marshal error for non-serializable context")
	}
	if !strings.Contains(err.Error(), "marshalling classify candidates") {
		t.Errorf("expected marshalling error, got %q", err.Error())
	}
}

func TestLLMClassify_OrderInsensitiveSourceEvents(t *testing.T) {
	// Candidate has events in one order, LLM returns them in another
	candidates := []Candidate{{
		SourceEvents:   []string{"evt-b", "evt-a"},
		RawText:        "Multi-event candidate",
		CandidateType:  "correction",
		Confidence:     0.7,
		SessionContext: map[string]any{"session_id": "sess-1"},
	}}

	resp := classifiedResponse{
		Classified: []classifiedEntry{{
			Index:        0,
			SourceEvents: []string{"evt-a", "evt-b"}, // reversed order
			Kind:         "directive",
			MemoryType:   "semantic",
			Scope:        "universal",
			Importance:   0.8,
			Content: classifiedContent{
				Canonical: "Canonical for multi-event",
				Summary:   "Multi-event summary",
				Tags:      []string{"test", "ordering"},
			},
		}},
	}
	data, _ := json.Marshal(resp)

	memories, err := ParseClassifiedMemories(string(data), candidates)
	if err != nil {
		t.Fatalf("order-insensitive source_events should match: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
}

func TestLLMClassify_IndexBasedResolution(t *testing.T) {
	// Two candidates; LLM returns entries in swapped positional order but with correct index fields.
	// Entry at position 0 has Index=1 (belongs to candidate 1), and entry at position 1 has Index=0
	// (belongs to candidate 0). Positional matching (strategy 2) would assign them incorrectly;
	// only index-based resolution (strategy 1) maps them correctly.
	candidates := makeCandidates(2)

	resp := classifiedResponse{
		Classified: []classifiedEntry{
			{
				// Position 0, but Index=1 → should resolve to candidates[1]
				Index:        1,
				SourceEvents: candidates[1].SourceEvents,
				Kind:         "constraint",
				MemoryType:   "semantic",
				Scope:        "universal",
				Importance:   0.7,
				Content: classifiedContent{
					Canonical: "Second candidate canonical",
					Summary:   "Second summary",
					Tags:      []string{"test", "index"},
				},
			},
			{
				// Position 1, but Index=0 → should resolve to candidates[0]
				Index:        0,
				SourceEvents: candidates[0].SourceEvents,
				Kind:         "directive",
				MemoryType:   "semantic",
				Scope:        "universal",
				Importance:   0.9,
				Content: classifiedContent{
					Canonical: "First candidate canonical",
					Summary:   "First summary",
					Tags:      []string{"test", "index"},
				},
			},
		},
	}
	data, _ := json.Marshal(resp)

	memories, err := ParseClassifiedMemories(string(data), candidates)
	if err != nil {
		t.Fatalf("index-based resolution should work: %v", err)
	}
	if len(memories) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(memories))
	}
	// Verify correct mapping: memories appear in response order, but each is matched
	// to the correct candidate via index-based resolution (strategy 1).
	// Response position 0 (Index=1) → resolved to candidates[1] → constraint
	// Response position 1 (Index=0) → resolved to candidates[0] → directive
	if memories[0].Kind != models.BehaviorKindConstraint {
		t.Errorf("memory[0]: expected constraint (from candidate 1), got %q", memories[0].Kind)
	}
	if memories[0].Candidate.SourceEvents[0] != candidates[1].SourceEvents[0] {
		t.Errorf("memory[0]: should be matched to candidate 1")
	}
	if memories[1].Kind != models.BehaviorKindDirective {
		t.Errorf("memory[1]: expected directive (from candidate 0), got %q", memories[1].Kind)
	}
	if memories[1].Candidate.SourceEvents[0] != candidates[0].SourceEvents[0] {
		t.Errorf("memory[1]: should be matched to candidate 0")
	}
}

func TestSourceEventsKey_OrderInsensitive(t *testing.T) {
	key1 := sourceEventsKey([]string{"b", "a", "c"})
	key2 := sourceEventsKey([]string{"c", "a", "b"})
	if key1 != key2 {
		t.Errorf("sourceEventsKey should be order-insensitive: %q != %q", key1, key2)
	}
}

func TestSourceEventsMatch_OrderInsensitive(t *testing.T) {
	if !sourceEventsMatch([]string{"b", "a"}, []string{"a", "b"}) {
		t.Error("sourceEventsMatch should be order-insensitive")
	}
	if sourceEventsMatch([]string{"a", "b"}, []string{"a", "c"}) {
		t.Error("sourceEventsMatch should reject different events")
	}
	if sourceEventsMatch([]string{"a"}, []string{"a", "b"}) {
		t.Error("sourceEventsMatch should reject different lengths")
	}
}
