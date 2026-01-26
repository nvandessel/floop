# Behavior Graph System - Technical Specification

> **Purpose**: This document provides comprehensive specifications for building a behavior learning and management system for AI coding agents. It should be used as the primary reference when implementing the system.

## Landscape Analysis

Before building, understand what exists and where we fit:

### Existing Tools by Category

**Rule Distribution (solved problem):**
- `rulebook-ai` - Packs of rules synced to Cursor/Copilot/Claude/etc.
- `Ruler` - Single source of truth distributed to all agents
- `agent-rules` - AGENTS.md standard specification

**Fact Memory (active area):**
- `Mem0` (45k stars) - Universal memory layer for user facts/preferences
- `Zep`, `MemOS`, `OpenMemory` - Various memory backends

**Learning from Corrections (sparse):**
- `claude-reflect-system` (58 stars) - Pattern detection → updates Claude skills
  - Claude Code only, Python/YAML, no graph structure, no sharing

### Our Unique Position

```
What exists:                        What we're building:
─────────────────────────────────────────────────────────────
Rule distribution    ✓ solved       Learning loop         ✓ NEW
Fact memory          ✓ solved       Graph structure       ✓ NEW  
Learning behaviors   ~ partial      Activation conditions ✓ NEW
Graph relationships  ✗ none         Package/sharing       ✓ NEW
Cross-agent CLI      ✗ none         Agent-agnostic        ✓ NEW
```

### Integration Opportunities

1. **Output formats**: Could export to rulebook-ai/Ruler formats for distribution
2. **MCP**: Several tools use MCP servers; potential future integration path
3. **Pattern detection**: claude-reflect-system has pattern matching code worth reviewing

### What NOT to Build

- Don't build another rule distribution system (use existing)
- Don't build fact memory (Mem0 exists)
- Focus on: **learning behavioral rules from corrections** + **graph structure**

---

## Executive Summary

We're building a system that:
1. Captures corrections from human-agent interactions
2. Transforms corrections into durable, reusable behaviors
3. Activates relevant behaviors based on current context
4. Assembles active behaviors into agent prompts
5. Supports sharing and importing behaviors across projects

The system is CLI-first, designed to integrate with any agent that can execute shell commands (Claude Code, Codex, Cursor, Aider, etc.) via `agents.md` instructions.

---

## Project Structure

```
behavior-graph/
├── .beads/                    # Beads tracking for THIS project
├── cmd/
│   └── bg/
│       └── main.go            # CLI entry point
├── internal/
│   ├── models/
│   │   ├── behavior.go        # Behavior, BehaviorContent, BehaviorKind
│   │   ├── correction.go      # Correction struct
│   │   ├── context.go         # ContextSnapshot, context matching
│   │   └── provenance.go      # Provenance, SourceType
│   ├── store/
│   │   ├── store.go           # GraphStore interface
│   │   ├── beads.go           # BeadsGraphStore implementation
│   │   └── memory.go          # InMemoryGraphStore (for testing)
│   ├── learning/
│   │   ├── capture.go         # CorrectionCapture
│   │   ├── extract.go         # BehaviorExtractor
│   │   ├── place.go           # GraphPlacer
│   │   └── loop.go            # LearningLoop orchestrator
│   ├── activation/
│   │   ├── context.go         # ContextBuilder
│   │   ├── evaluate.go        # Predicate evaluation
│   │   └── resolve.go         # Conflict resolution, active set computation
│   └── assembly/
│       ├── compile.go         # Assemble behaviors into prompt
│       └── optimize.go        # Token optimization
├── pkg/
│   └── types/                 # Public types if needed for imports
├── go.mod
├── go.sum
├── AGENTS.md                  # Instructions for agents working on this project
├── README.md
└── .behaviors/                # This project's own behaviors (dogfooding)
    ├── manifest.yaml
    └── learned/
```

---

## Core Data Models

### internal/models/behavior.go

```go
package models

import (
    "time"
)

// BehaviorKind categorizes what type of behavioral guidance this is
type BehaviorKind string

const (
    BehaviorKindDirective   BehaviorKind = "directive"   // Do X
    BehaviorKindConstraint  BehaviorKind = "constraint"  // Never do Y
    BehaviorKindProcedure   BehaviorKind = "procedure"   // Multi-step process
    BehaviorKindPreference  BehaviorKind = "preference"  // Prefer X over Y
)

// BehaviorContent holds multiple representations of the behavior's content
type BehaviorContent struct {
    // Canonical is the minimal representation, optimized for token efficiency
    Canonical string `json:"canonical" yaml:"canonical"`
    
    // Expanded is the full prose version with examples and rationale
    Expanded string `json:"expanded,omitempty" yaml:"expanded,omitempty"`
    
    // Structured holds key-value data when the behavior has clear structure
    // e.g., {"prefer": "pathlib.Path", "avoid": "os.path"}
    Structured map[string]interface{} `json:"structured,omitempty" yaml:"structured,omitempty"`
}

// Behavior represents a unit of agent behavior
type Behavior struct {
    // Identity
    ID   string `json:"id" yaml:"id"`
    Name string `json:"name" yaml:"name"`
    Kind BehaviorKind `json:"kind" yaml:"kind"`
    
    // Activation - when does this behavior apply?
    // Keys are context fields, values are required values
    // e.g., {"language": "python", "task": ["refactor", "write"]}
    When map[string]interface{} `json:"when,omitempty" yaml:"when,omitempty"`
    
    // Content
    Content BehaviorContent `json:"content" yaml:"content"`
    
    // Provenance - where did this come from?
    Provenance Provenance `json:"provenance" yaml:"provenance"`
    
    // Confidence score (0.0 - 1.0)
    // Learned behaviors start lower, increase with successful application
    Confidence float64 `json:"confidence" yaml:"confidence"`
    
    // Priority for conflict resolution (higher wins)
    Priority int `json:"priority" yaml:"priority"`
    
    // Graph relationships (IDs of other behaviors)
    Requires  []string `json:"requires,omitempty" yaml:"requires,omitempty"`   // Hard dependencies
    Overrides []string `json:"overrides,omitempty" yaml:"overrides,omitempty"` // This supersedes those
    Conflicts []string `json:"conflicts,omitempty" yaml:"conflicts,omitempty"` // Mutual exclusion
    SimilarTo []SimilarityLink `json:"similar_to,omitempty" yaml:"similar_to,omitempty"`
    
    // Statistics (updated over time)
    Stats BehaviorStats `json:"stats" yaml:"stats"`
}

// SimilarityLink represents a similarity relationship with a score
type SimilarityLink struct {
    ID    string  `json:"id" yaml:"id"`
    Score float64 `json:"score" yaml:"score"`
}

// BehaviorStats tracks usage statistics
type BehaviorStats struct {
    TimesActivated int        `json:"times_activated" yaml:"times_activated"`
    TimesFollowed  int        `json:"times_followed" yaml:"times_followed"`
    TimesOverridden int       `json:"times_overridden" yaml:"times_overridden"`
    LastActivated  *time.Time `json:"last_activated,omitempty" yaml:"last_activated,omitempty"`
    CreatedAt      time.Time  `json:"created_at" yaml:"created_at"`
    UpdatedAt      time.Time  `json:"updated_at" yaml:"updated_at"`
}
```

### internal/models/correction.go

```go
package models

import (
    "time"
)

// Correction represents a captured correction event from a conversation
type Correction struct {
    // Unique identifier (content-addressed hash)
    ID string `json:"id" yaml:"id"`
    
    // When this correction occurred
    Timestamp time.Time `json:"timestamp" yaml:"timestamp"`
    
    // The context when the correction happened
    Context ContextSnapshot `json:"context" yaml:"context"`
    
    // What the agent did (that was wrong)
    AgentAction string `json:"agent_action" yaml:"agent_action"`
    
    // What the human said in response
    HumanResponse string `json:"human_response" yaml:"human_response"`
    
    // What the agent should have done (extracted/inferred)
    CorrectedAction string `json:"corrected_action" yaml:"corrected_action"`
    
    // Conversation reference
    ConversationID string `json:"conversation_id" yaml:"conversation_id"`
    TurnNumber     int    `json:"turn_number" yaml:"turn_number"`
    
    // Who made the correction
    Corrector string `json:"corrector" yaml:"corrector"`
    
    // Processing state
    Processed   bool      `json:"processed" yaml:"processed"`
    ProcessedAt *time.Time `json:"processed_at,omitempty" yaml:"processed_at,omitempty"`
}
```

### internal/models/context.go

```go
package models

import (
    "path/filepath"
    "strings"
    "time"
)

// ContextSnapshot captures the environment at a point in time
type ContextSnapshot struct {
    Timestamp time.Time `json:"timestamp" yaml:"timestamp"`
    
    // Repository info
    Repo      string `json:"repo,omitempty" yaml:"repo,omitempty"`
    RepoRoot  string `json:"repo_root,omitempty" yaml:"repo_root,omitempty"`
    Branch    string `json:"branch,omitempty" yaml:"branch,omitempty"`
    
    // File info
    FilePath     string `json:"file_path,omitempty" yaml:"file_path,omitempty"`
    FileLanguage string `json:"file_language,omitempty" yaml:"file_language,omitempty"`
    FileExt      string `json:"file_ext,omitempty" yaml:"file_ext,omitempty"`
    
    // Task info
    Task string `json:"task,omitempty" yaml:"task,omitempty"`
    
    // User info
    User  string   `json:"user,omitempty" yaml:"user,omitempty"`
    Roles []string `json:"roles,omitempty" yaml:"roles,omitempty"`
    
    // Environment
    Environment string `json:"environment,omitempty" yaml:"environment,omitempty"` // dev, staging, prod
    
    // Custom fields for extensibility
    Custom map[string]interface{} `json:"custom,omitempty" yaml:"custom,omitempty"`
}

// Matches checks if this context matches a 'when' predicate
func (c *ContextSnapshot) Matches(predicate map[string]interface{}) bool {
    for key, required := range predicate {
        actual := c.getField(key)
        if !matchValue(actual, required) {
            return false
        }
    }
    return true
}

// getField retrieves a field value by name
func (c *ContextSnapshot) getField(key string) interface{} {
    switch key {
    case "repo":
        return c.Repo
    case "branch":
        return c.Branch
    case "file_path", "file.path":
        return c.FilePath
    case "file_language", "file.language", "language":
        return c.FileLanguage
    case "file_ext", "file.ext", "ext":
        return c.FileExt
    case "task":
        return c.Task
    case "user":
        return c.User
    case "environment", "env":
        return c.Environment
    default:
        if c.Custom != nil {
            return c.Custom[key]
        }
        return nil
    }
}

// matchValue checks if an actual value matches a required value
// Supports: exact match, array membership, glob patterns
func matchValue(actual interface{}, required interface{}) bool {
    if actual == nil {
        return false
    }
    
    actualStr, actualIsStr := actual.(string)
    
    switch req := required.(type) {
    case string:
        if !actualIsStr {
            return false
        }
        // Support glob patterns
        if strings.Contains(req, "*") {
            matched, _ := filepath.Match(req, actualStr)
            return matched
        }
        return actualStr == req
        
    case []interface{}:
        // Value must be one of the options
        for _, option := range req {
            if optStr, ok := option.(string); ok && actualIsStr {
                if actualStr == optStr {
                    return true
                }
            }
        }
        return false
        
    case []string:
        if !actualIsStr {
            return false
        }
        for _, option := range req {
            if actualStr == option {
                return true
            }
        }
        return false
        
    default:
        return actual == required
    }
}

// InferLanguage attempts to determine language from file extension
func InferLanguage(filePath string) string {
    ext := strings.ToLower(filepath.Ext(filePath))
    switch ext {
    case ".go":
        return "go"
    case ".py":
        return "python"
    case ".js":
        return "javascript"
    case ".ts":
        return "typescript"
    case ".rs":
        return "rust"
    case ".rb":
        return "ruby"
    case ".java":
        return "java"
    case ".c", ".h":
        return "c"
    case ".cpp", ".cc", ".hpp":
        return "cpp"
    case ".md":
        return "markdown"
    case ".yaml", ".yml":
        return "yaml"
    case ".json":
        return "json"
    default:
        return ""
    }
}
```

### internal/models/provenance.go

```go
package models

import (
    "time"
)

// SourceType indicates where a behavior came from
type SourceType string

const (
    SourceTypeAuthored  SourceType = "authored"  // Human wrote it directly
    SourceTypeLearned   SourceType = "learned"   // Extracted from a correction
    SourceTypeImported  SourceType = "imported"  // From an external package
)

// Provenance tracks where a behavior came from
type Provenance struct {
    SourceType SourceType `json:"source_type" yaml:"source_type"`
    CreatedAt  time.Time  `json:"created_at" yaml:"created_at"`
    
    // For authored behaviors
    Author string `json:"author,omitempty" yaml:"author,omitempty"`
    
    // For learned behaviors
    CorrectionID string     `json:"correction_id,omitempty" yaml:"correction_id,omitempty"`
    ApprovedBy   string     `json:"approved_by,omitempty" yaml:"approved_by,omitempty"`
    ApprovedAt   *time.Time `json:"approved_at,omitempty" yaml:"approved_at,omitempty"`
    
    // For imported behaviors
    Package        string `json:"package,omitempty" yaml:"package,omitempty"`
    PackageVersion string `json:"package_version,omitempty" yaml:"package_version,omitempty"`
}

// IsApproved returns true if a learned behavior has been approved
func (p *Provenance) IsApproved() bool {
    return p.SourceType == SourceTypeLearned && p.ApprovedBy != ""
}

// IsPending returns true if a learned behavior is awaiting approval
func (p *Provenance) IsPending() bool {
    return p.SourceType == SourceTypeLearned && p.ApprovedBy == ""
}
```

---

## GraphStore Interface

### internal/store/store.go

```go
package store

import (
    "context"
)

// Node represents a node in the behavior graph
type Node struct {
    ID       string                 `json:"id"`
    Kind     string                 `json:"kind"` // "behavior", "correction", "context-snapshot"
    Content  map[string]interface{} `json:"content"`
    Metadata map[string]interface{} `json:"metadata"`
}

// Edge represents a relationship between nodes
type Edge struct {
    Source   string                 `json:"source"`
    Target   string                 `json:"target"`
    Kind     string                 `json:"kind"` // "requires", "overrides", "conflicts", "learned-from", "similar-to"
    Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// GraphStore defines the interface for storing and querying the behavior graph
type GraphStore interface {
    // Node operations
    AddNode(ctx context.Context, node Node) (string, error)
    UpdateNode(ctx context.Context, node Node) error
    GetNode(ctx context.Context, id string) (*Node, error)
    DeleteNode(ctx context.Context, id string) error
    
    // Query nodes by predicate
    // predicate is a map of field paths to required values
    // e.g., {"kind": "behavior", "metadata.confidence": 0.8}
    QueryNodes(ctx context.Context, predicate map[string]interface{}) ([]Node, error)
    
    // Edge operations
    AddEdge(ctx context.Context, edge Edge) error
    RemoveEdge(ctx context.Context, source, target, kind string) error
    GetEdges(ctx context.Context, nodeID string, direction Direction, kind string) ([]Edge, error)
    
    // Graph traversal
    // Returns all nodes reachable from start by following edges of the given kinds
    Traverse(ctx context.Context, start string, edgeKinds []string, direction Direction, maxDepth int) ([]Node, error)
    
    // Persistence
    Sync(ctx context.Context) error
    Close() error
}

// Direction specifies edge traversal direction
type Direction string

const (
    DirectionOutbound Direction = "outbound" // Follow edges from source to target
    DirectionInbound  Direction = "inbound"  // Follow edges from target to source
    DirectionBoth     Direction = "both"     // Follow edges in both directions
)
```

### internal/store/beads.go

```go
package store

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
)

// BeadsGraphStore implements GraphStore using Beads as the backend
type BeadsGraphStore struct {
    root       string // Path to .behaviors directory
    beadsRoot  string // Path to .beads directory (for the beads CLI)
}

// NewBeadsGraphStore creates a new BeadsGraphStore
func NewBeadsGraphStore(projectRoot string) (*BeadsGraphStore, error) {
    behaviorsRoot := filepath.Join(projectRoot, ".behaviors")
    beadsRoot := filepath.Join(projectRoot, ".beads")
    
    // Ensure directories exist
    if err := os.MkdirAll(behaviorsRoot, 0755); err != nil {
        return nil, fmt.Errorf("failed to create .behaviors directory: %w", err)
    }
    
    return &BeadsGraphStore{
        root:      behaviorsRoot,
        beadsRoot: beadsRoot,
    }, nil
}

// Implementation notes:
// 
// Beads stores issues in .beads/beads.jsonl as JSONL format.
// Each line is a JSON object representing an issue/bead.
// 
// For our purposes, we'll store behaviors as a special type of bead,
// or in a separate file within .behaviors/ that follows the same format.
//
// Mapping:
//   Node.ID       -> Bead ID (bd-xxxx format or custom behavior-xxxx)
//   Node.Kind     -> Bead type field
//   Node.Content  -> Bead description + custom fields
//   Node.Metadata -> Bead metadata fields
//   Edge          -> Stored in bead's dependencies/blocks fields + custom relations
//
// The bd CLI can be used for some operations, but we may need to
// read/write the JSONL directly for full control.

func (s *BeadsGraphStore) AddNode(ctx context.Context, node Node) (string, error) {
    // TODO: Implement
    // Option 1: Use bd create with custom type
    // Option 2: Write directly to a behaviors.jsonl file
    return "", fmt.Errorf("not implemented")
}

func (s *BeadsGraphStore) UpdateNode(ctx context.Context, node Node) error {
    // TODO: Implement
    return fmt.Errorf("not implemented")
}

func (s *BeadsGraphStore) GetNode(ctx context.Context, id string) (*Node, error) {
    // TODO: Implement
    return nil, fmt.Errorf("not implemented")
}

func (s *BeadsGraphStore) DeleteNode(ctx context.Context, id string) error {
    // TODO: Implement
    return fmt.Errorf("not implemented")
}

func (s *BeadsGraphStore) QueryNodes(ctx context.Context, predicate map[string]interface{}) ([]Node, error) {
    // TODO: Implement
    return nil, fmt.Errorf("not implemented")
}

func (s *BeadsGraphStore) AddEdge(ctx context.Context, edge Edge) error {
    // TODO: Implement
    return fmt.Errorf("not implemented")
}

func (s *BeadsGraphStore) RemoveEdge(ctx context.Context, source, target, kind string) error {
    // TODO: Implement
    return fmt.Errorf("not implemented")
}

func (s *BeadsGraphStore) GetEdges(ctx context.Context, nodeID string, direction Direction, kind string) ([]Edge, error) {
    // TODO: Implement
    return nil, fmt.Errorf("not implemented")
}

func (s *BeadsGraphStore) Traverse(ctx context.Context, start string, edgeKinds []string, direction Direction, maxDepth int) ([]Node, error) {
    // TODO: Implement
    return nil, fmt.Errorf("not implemented")
}

func (s *BeadsGraphStore) Sync(ctx context.Context) error {
    // TODO: Implement - ensure all changes are written to disk
    return nil
}

func (s *BeadsGraphStore) Close() error {
    return s.Sync(context.Background())
}

// Helper: run bd command and return output
func (s *BeadsGraphStore) runBd(args ...string) ([]byte, error) {
    cmd := exec.Command("bd", args...)
    cmd.Dir = filepath.Dir(s.beadsRoot)
    return cmd.Output()
}
```

### internal/store/memory.go

```go
package store

import (
    "context"
    "fmt"
    "sync"
)

// InMemoryGraphStore implements GraphStore for testing
type InMemoryGraphStore struct {
    mu    sync.RWMutex
    nodes map[string]Node
    edges []Edge
}

// NewInMemoryGraphStore creates a new in-memory store
func NewInMemoryGraphStore() *InMemoryGraphStore {
    return &InMemoryGraphStore{
        nodes: make(map[string]Node),
        edges: make([]Edge, 0),
    }
}

func (s *InMemoryGraphStore) AddNode(ctx context.Context, node Node) (string, error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    if node.ID == "" {
        return "", fmt.Errorf("node ID is required")
    }
    
    s.nodes[node.ID] = node
    return node.ID, nil
}

func (s *InMemoryGraphStore) UpdateNode(ctx context.Context, node Node) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    if _, exists := s.nodes[node.ID]; !exists {
        return fmt.Errorf("node not found: %s", node.ID)
    }
    
    s.nodes[node.ID] = node
    return nil
}

func (s *InMemoryGraphStore) GetNode(ctx context.Context, id string) (*Node, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    node, exists := s.nodes[id]
    if !exists {
        return nil, nil
    }
    return &node, nil
}

func (s *InMemoryGraphStore) DeleteNode(ctx context.Context, id string) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    delete(s.nodes, id)
    
    // Remove edges involving this node
    filtered := make([]Edge, 0)
    for _, e := range s.edges {
        if e.Source != id && e.Target != id {
            filtered = append(filtered, e)
        }
    }
    s.edges = filtered
    
    return nil
}

func (s *InMemoryGraphStore) QueryNodes(ctx context.Context, predicate map[string]interface{}) ([]Node, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    results := make([]Node, 0)
    for _, node := range s.nodes {
        if matchesPredicate(node, predicate) {
            results = append(results, node)
        }
    }
    return results, nil
}

func (s *InMemoryGraphStore) AddEdge(ctx context.Context, edge Edge) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    s.edges = append(s.edges, edge)
    return nil
}

func (s *InMemoryGraphStore) RemoveEdge(ctx context.Context, source, target, kind string) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    filtered := make([]Edge, 0)
    for _, e := range s.edges {
        if !(e.Source == source && e.Target == target && e.Kind == kind) {
            filtered = append(filtered, e)
        }
    }
    s.edges = filtered
    return nil
}

func (s *InMemoryGraphStore) GetEdges(ctx context.Context, nodeID string, direction Direction, kind string) ([]Edge, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    results := make([]Edge, 0)
    for _, e := range s.edges {
        if kind != "" && e.Kind != kind {
            continue
        }
        
        switch direction {
        case DirectionOutbound:
            if e.Source == nodeID {
                results = append(results, e)
            }
        case DirectionInbound:
            if e.Target == nodeID {
                results = append(results, e)
            }
        case DirectionBoth:
            if e.Source == nodeID || e.Target == nodeID {
                results = append(results, e)
            }
        }
    }
    return results, nil
}

func (s *InMemoryGraphStore) Traverse(ctx context.Context, start string, edgeKinds []string, direction Direction, maxDepth int) ([]Node, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    visited := make(map[string]bool)
    results := make([]Node, 0)
    
    s.traverseRecursive(start, edgeKinds, direction, maxDepth, 0, visited, &results)
    
    return results, nil
}

func (s *InMemoryGraphStore) traverseRecursive(current string, edgeKinds []string, direction Direction, maxDepth, depth int, visited map[string]bool, results *[]Node) {
    if depth > maxDepth || visited[current] {
        return
    }
    visited[current] = true
    
    if node, exists := s.nodes[current]; exists {
        *results = append(*results, node)
    }
    
    for _, e := range s.edges {
        if !edgeKindMatches(e.Kind, edgeKinds) {
            continue
        }
        
        var next string
        switch direction {
        case DirectionOutbound:
            if e.Source == current {
                next = e.Target
            }
        case DirectionInbound:
            if e.Target == current {
                next = e.Source
            }
        case DirectionBoth:
            if e.Source == current {
                next = e.Target
            } else if e.Target == current {
                next = e.Source
            }
        }
        
        if next != "" {
            s.traverseRecursive(next, edgeKinds, direction, maxDepth, depth+1, visited, results)
        }
    }
}

func (s *InMemoryGraphStore) Sync(ctx context.Context) error {
    return nil // No-op for in-memory
}

func (s *InMemoryGraphStore) Close() error {
    return nil
}

// Helper functions

func matchesPredicate(node Node, predicate map[string]interface{}) bool {
    for key, required := range predicate {
        var actual interface{}
        
        switch key {
        case "kind":
            actual = node.Kind
        case "id":
            actual = node.ID
        default:
            // Check content and metadata
            if val, ok := node.Content[key]; ok {
                actual = val
            } else if val, ok := node.Metadata[key]; ok {
                actual = val
            }
        }
        
        if actual != required {
            return false
        }
    }
    return true
}

func edgeKindMatches(kind string, allowed []string) bool {
    if len(allowed) == 0 {
        return true
    }
    for _, k := range allowed {
        if k == kind {
            return true
        }
    }
    return false
}
```

---

## Learning Loop

### internal/learning/loop.go

```go
package learning

import (
    "context"
    "fmt"
    "time"
    
    "github.com/YOUR_USERNAME/behavior-graph/internal/models"
    "github.com/YOUR_USERNAME/behavior-graph/internal/store"
)

// LearningResult represents the result of processing a correction
type LearningResult struct {
    Correction       models.Correction
    CandidateBehavior models.Behavior
    Placement        PlacementDecision
    AutoAccepted     bool
    RequiresReview   bool
    ReviewReasons    []string
}

// PlacementDecision describes where a new behavior should go in the graph
type PlacementDecision struct {
    Action           string   // "create", "merge", "specialize"
    TargetID         string   // For merge/specialize, the existing behavior
    ProposedEdges    []ProposedEdge
    SimilarBehaviors []SimilarityMatch
    Confidence       float64
}

// ProposedEdge represents a proposed edge to add
type ProposedEdge struct {
    From string
    To   string
    Kind string
}

// SimilarityMatch represents a similar existing behavior
type SimilarityMatch struct {
    ID    string
    Score float64
}

// LearningLoop orchestrates the correction -> behavior pipeline
type LearningLoop struct {
    store               store.GraphStore
    capturer            *CorrectionCapture
    extractor           *BehaviorExtractor
    placer              *GraphPlacer
    autoAcceptThreshold float64
}

// NewLearningLoop creates a new learning loop
func NewLearningLoop(s store.GraphStore) *LearningLoop {
    return &LearningLoop{
        store:               s,
        capturer:            NewCorrectionCapture(),
        extractor:           NewBehaviorExtractor(),
        placer:              NewGraphPlacer(s),
        autoAcceptThreshold: 0.8,
    }
}

// ProcessCorrection processes a single correction into a candidate behavior
func (l *LearningLoop) ProcessCorrection(ctx context.Context, correction models.Correction) (*LearningResult, error) {
    // Step 1: Extract candidate behavior
    candidate, err := l.extractor.Extract(correction)
    if err != nil {
        return nil, fmt.Errorf("extraction failed: %w", err)
    }
    
    // Step 2: Determine graph placement
    placement, err := l.placer.Place(ctx, candidate)
    if err != nil {
        return nil, fmt.Errorf("placement failed: %w", err)
    }
    
    // Step 3: Decide if auto-accept or needs review
    requiresReview, reasons := l.needsReview(candidate, placement)
    autoAccepted := !requiresReview && placement.Confidence >= l.autoAcceptThreshold
    
    // Step 4: If auto-accepted, commit to graph
    if autoAccepted {
        if err := l.commitBehavior(ctx, candidate, placement); err != nil {
            return nil, fmt.Errorf("commit failed: %w", err)
        }
    }
    
    return &LearningResult{
        Correction:        correction,
        CandidateBehavior: *candidate,
        Placement:         *placement,
        AutoAccepted:      autoAccepted,
        RequiresReview:    requiresReview,
        ReviewReasons:     reasons,
    }, nil
}

// needsReview determines if human review is required
func (l *LearningLoop) needsReview(candidate *models.Behavior, placement *PlacementDecision) (bool, []string) {
    var reasons []string
    
    // Constraints always need review
    if candidate.Kind == models.BehaviorKindConstraint {
        reasons = append(reasons, "Constraints require human review")
    }
    
    // Merging into existing behavior needs review
    if placement.Action == "merge" {
        reasons = append(reasons, fmt.Sprintf("Would merge into existing behavior: %s", placement.TargetID))
    }
    
    // Conflicts need review
    if len(candidate.Conflicts) > 0 {
        reasons = append(reasons, fmt.Sprintf("Conflicts with: %v", candidate.Conflicts))
    }
    
    // Low confidence placements need review
    if placement.Confidence < 0.6 {
        reasons = append(reasons, fmt.Sprintf("Low placement confidence: %.2f", placement.Confidence))
    }
    
    // High similarity to existing might be duplicate
    for _, sim := range placement.SimilarBehaviors {
        if sim.Score > 0.85 {
            reasons = append(reasons, fmt.Sprintf("Very similar to existing: %s (%.2f)", sim.ID, sim.Score))
        }
    }
    
    return len(reasons) > 0, reasons
}

// commitBehavior saves the behavior to the graph
func (l *LearningLoop) commitBehavior(ctx context.Context, behavior *models.Behavior, placement *PlacementDecision) error {
    // Convert behavior to node
    node := store.Node{
        ID:   behavior.ID,
        Kind: "behavior",
        Content: map[string]interface{}{
            "name":       behavior.Name,
            "kind":       string(behavior.Kind),
            "when":       behavior.When,
            "content":    behavior.Content,
            "provenance": behavior.Provenance,
            "requires":   behavior.Requires,
            "overrides":  behavior.Overrides,
            "conflicts":  behavior.Conflicts,
        },
        Metadata: map[string]interface{}{
            "confidence": behavior.Confidence,
            "priority":   behavior.Priority,
            "stats":      behavior.Stats,
        },
    }
    
    // Add the node
    if _, err := l.store.AddNode(ctx, node); err != nil {
        return err
    }
    
    // Add edges
    for _, e := range placement.ProposedEdges {
        edge := store.Edge{
            Source: e.From,
            Target: e.To,
            Kind:   e.Kind,
        }
        if err := l.store.AddEdge(ctx, edge); err != nil {
            return err
        }
    }
    
    return l.store.Sync(ctx)
}

// ApprovePending approves a pending behavior
func (l *LearningLoop) ApprovePending(ctx context.Context, behaviorID, approver string) error {
    node, err := l.store.GetNode(ctx, behaviorID)
    if err != nil {
        return err
    }
    if node == nil {
        return fmt.Errorf("behavior not found: %s", behaviorID)
    }
    
    // Update provenance
    if prov, ok := node.Content["provenance"].(map[string]interface{}); ok {
        prov["approved_by"] = approver
        prov["approved_at"] = time.Now()
    }
    
    // Increase confidence
    if conf, ok := node.Metadata["confidence"].(float64); ok {
        node.Metadata["confidence"] = min(1.0, conf+0.2)
    }
    
    return l.store.UpdateNode(ctx, *node)
}

// RejectPending rejects a pending behavior
func (l *LearningLoop) RejectPending(ctx context.Context, behaviorID, rejector, reason string) error {
    node, err := l.store.GetNode(ctx, behaviorID)
    if err != nil {
        return err
    }
    if node == nil {
        return fmt.Errorf("behavior not found: %s", behaviorID)
    }
    
    node.Kind = "rejected-behavior"
    node.Metadata["rejected_by"] = rejector
    node.Metadata["rejected_at"] = time.Now()
    node.Metadata["rejection_reason"] = reason
    
    return l.store.UpdateNode(ctx, *node)
}

func min(a, b float64) float64 {
    if a < b {
        return a
    }
    return b
}
```

### internal/learning/capture.go

```go
package learning

import (
    "crypto/sha256"
    "encoding/hex"
    "strings"
    "time"
    
    "github.com/YOUR_USERNAME/behavior-graph/internal/models"
)

// CorrectionCapture detects and structures correction events
type CorrectionCapture struct {
    // Signals that often indicate a correction
    correctionSignals []string
}

// NewCorrectionCapture creates a new capture instance
func NewCorrectionCapture() *CorrectionCapture {
    return &CorrectionCapture{
        correctionSignals: []string{
            "no,", "don't", "instead", "actually,", "not like that",
            "that's wrong", "that's not right", "shouldn't",
            "prefer", "better to", "rather than", "use this instead",
            "that's incorrect", "please use", "you should",
        },
    }
}

// CaptureFromCLI creates a correction from CLI input
// This is the primary entry point when an agent self-reports a correction
func (c *CorrectionCapture) CaptureFromCLI(wrong, right string, ctx models.ContextSnapshot) (*models.Correction, error) {
    id := c.generateID(wrong, right)
    
    return &models.Correction{
        ID:              id,
        Timestamp:       time.Now(),
        Context:         ctx,
        AgentAction:     wrong,
        HumanResponse:   "", // Not captured in CLI mode
        CorrectedAction: right,
        ConversationID:  "", // Could be passed from agent
        TurnNumber:      0,
        Corrector:       ctx.User,
        Processed:       false,
    }, nil
}

// MightBeCorrection checks if a message looks like a correction
func (c *CorrectionCapture) MightBeCorrection(text string) bool {
    lower := strings.ToLower(text)
    for _, signal := range c.correctionSignals {
        if strings.Contains(lower, signal) {
            return true
        }
    }
    return false
}

func (c *CorrectionCapture) generateID(wrong, right string) string {
    content := wrong[:min(100, len(wrong))] + right[:min(100, len(right))]
    hash := sha256.Sum256([]byte(content))
    return "correction-" + hex.EncodeToString(hash[:])[:12]
}

func min(a, b int) int {
    if a < b {
        return a
    }
    return b
}
```

### internal/learning/extract.go

```go
package learning

import (
    "crypto/sha256"
    "encoding/hex"
    "strings"
    "time"
    
    "github.com/YOUR_USERNAME/behavior-graph/internal/models"
)

// BehaviorExtractor transforms corrections into candidate behaviors
type BehaviorExtractor struct {}

// NewBehaviorExtractor creates a new extractor
func NewBehaviorExtractor() *BehaviorExtractor {
    return &BehaviorExtractor{}
}

// Extract creates a candidate behavior from a correction
func (e *BehaviorExtractor) Extract(correction models.Correction) (*models.Behavior, error) {
    // Generate ID
    id := e.generateID(correction)
    
    // Infer the 'when' predicate from context
    when := e.inferWhen(correction.Context)
    
    // Determine behavior kind
    kind := e.inferKind(correction)
    
    // Build content
    content := models.BehaviorContent{
        Canonical: correction.CorrectedAction,
        Structured: map[string]interface{}{
            "avoid":  correction.AgentAction,
            "prefer": correction.CorrectedAction,
        },
    }
    
    // Build provenance
    provenance := models.Provenance{
        SourceType:   models.SourceTypeLearned,
        CreatedAt:    time.Now(),
        CorrectionID: correction.ID,
    }
    
    // Generate a human-readable name
    name := e.generateName(correction)
    
    return &models.Behavior{
        ID:         id,
        Name:       name,
        Kind:       kind,
        When:       when,
        Content:    content,
        Provenance: provenance,
        Confidence: 0.6, // Learned behaviors start with lower confidence
        Priority:   0,
        Stats: models.BehaviorStats{
            CreatedAt: time.Now(),
            UpdatedAt: time.Now(),
        },
    }, nil
}

// inferWhen creates a 'when' predicate from the correction context
func (e *BehaviorExtractor) inferWhen(ctx models.ContextSnapshot) map[string]interface{} {
    when := make(map[string]interface{})
    
    // Include language if present
    if ctx.FileLanguage != "" {
        when["language"] = ctx.FileLanguage
    }
    
    // Include file pattern if we can generalize it
    if ctx.FilePath != "" {
        // Try to extract a meaningful pattern
        // e.g., "src/db/migrations/001.go" -> "db/*" or "*.go"
        // For now, just use the directory
        parts := strings.Split(ctx.FilePath, "/")
        if len(parts) > 1 {
            // Use first significant directory
            for _, part := range parts {
                if part != "" && part != "." && part != "src" {
                    when["file_path"] = part + "/*"
                    break
                }
            }
        }
    }
    
    // Include task if present
    if ctx.Task != "" {
        when["task"] = ctx.Task
    }
    
    return when
}

// inferKind determines the behavior kind from the correction
func (e *BehaviorExtractor) inferKind(correction models.Correction) models.BehaviorKind {
    lower := strings.ToLower(correction.CorrectedAction)
    
    // Check for constraint signals
    constraintSignals := []string{"never", "don't", "must not", "forbidden", "prohibited"}
    for _, signal := range constraintSignals {
        if strings.Contains(lower, signal) {
            return models.BehaviorKindConstraint
        }
    }
    
    // Check for preference signals
    preferenceSignals := []string{"prefer", "instead of", "rather than", "better to"}
    for _, signal := range preferenceSignals {
        if strings.Contains(lower, signal) {
            return models.BehaviorKindPreference
        }
    }
    
    // Default to directive
    return models.BehaviorKindDirective
}

// generateName creates a human-readable name for the behavior
func (e *BehaviorExtractor) generateName(correction models.Correction) string {
    // Take first 50 chars of the corrected action, clean it up
    name := correction.CorrectedAction
    if len(name) > 50 {
        name = name[:50]
    }
    
    // Replace problematic characters
    name = strings.ReplaceAll(name, "\n", " ")
    name = strings.ReplaceAll(name, "  ", " ")
    name = strings.TrimSpace(name)
    
    // Convert to slug-ish format
    name = strings.ToLower(name)
    name = strings.ReplaceAll(name, " ", "-")
    
    return "learned/" + name
}

func (e *BehaviorExtractor) generateID(correction models.Correction) string {
    content := correction.AgentAction + correction.CorrectedAction
    hash := sha256.Sum256([]byte(content))
    return "behavior-" + hex.EncodeToString(hash[:])[:12]
}
```

### internal/learning/place.go

```go
package learning

import (
    "context"
    
    "github.com/YOUR_USERNAME/behavior-graph/internal/models"
    "github.com/YOUR_USERNAME/behavior-graph/internal/store"
)

// GraphPlacer determines where a new behavior fits in the graph
type GraphPlacer struct {
    store store.GraphStore
}

// NewGraphPlacer creates a new placer
func NewGraphPlacer(s store.GraphStore) *GraphPlacer {
    return &GraphPlacer{store: s}
}

// Place determines where a behavior should be placed in the graph
func (p *GraphPlacer) Place(ctx context.Context, behavior *models.Behavior) (*PlacementDecision, error) {
    decision := &PlacementDecision{
        Action:           "create",
        ProposedEdges:    make([]ProposedEdge, 0),
        SimilarBehaviors: make([]SimilarityMatch, 0),
        Confidence:       0.7, // Default confidence
    }
    
    // Find existing behaviors with overlapping 'when' conditions
    existingBehaviors, err := p.findRelatedBehaviors(ctx, behavior)
    if err != nil {
        return nil, err
    }
    
    // Check for high similarity (potential duplicates)
    for _, existing := range existingBehaviors {
        similarity := p.computeSimilarity(behavior, &existing)
        if similarity > 0.5 {
            decision.SimilarBehaviors = append(decision.SimilarBehaviors, SimilarityMatch{
                ID:    existing.ID,
                Score: similarity,
            })
        }
        
        // If very high similarity, suggest merge
        if similarity > 0.9 {
            decision.Action = "merge"
            decision.TargetID = existing.ID
            decision.Confidence = 0.5 // Lower confidence for merges
        }
    }
    
    // Determine edges based on relationships
    decision.ProposedEdges = p.determineEdges(behavior, existingBehaviors)
    
    return decision, nil
}

// findRelatedBehaviors finds behaviors with overlapping activation conditions
func (p *GraphPlacer) findRelatedBehaviors(ctx context.Context, behavior *models.Behavior) ([]models.Behavior, error) {
    // Query for behaviors with similar 'when' conditions
    nodes, err := p.store.QueryNodes(ctx, map[string]interface{}{
        "kind": "behavior",
    })
    if err != nil {
        return nil, err
    }
    
    related := make([]models.Behavior, 0)
    for _, node := range nodes {
        // Check for overlapping conditions
        if p.hasOverlappingConditions(behavior.When, node.Content) {
            b := p.nodeToBehavior(node)
            related = append(related, b)
        }
    }
    
    return related, nil
}

// hasOverlappingConditions checks if two behaviors might apply in the same context
func (p *GraphPlacer) hasOverlappingConditions(when map[string]interface{}, content map[string]interface{}) bool {
    existingWhen, ok := content["when"].(map[string]interface{})
    if !ok {
        return false
    }
    
    // Check if any conditions match
    for key, value := range when {
        if existingValue, exists := existingWhen[key]; exists {
            if value == existingValue {
                return true
            }
        }
    }
    
    return false
}

// computeSimilarity calculates similarity between two behaviors
// This is a simple implementation; could be enhanced with embeddings
func (p *GraphPlacer) computeSimilarity(a, b *models.Behavior) float64 {
    // For now, use a simple heuristic based on:
    // 1. Overlapping 'when' conditions
    // 2. Similar content
    
    score := 0.0
    
    // Check 'when' overlap
    whenOverlap := p.computeWhenOverlap(a.When, b.When)
    score += whenOverlap * 0.4
    
    // Check content similarity (simple word overlap)
    contentSim := p.computeContentSimilarity(a.Content.Canonical, b.Content.Canonical)
    score += contentSim * 0.6
    
    return score
}

func (p *GraphPlacer) computeWhenOverlap(a, b map[string]interface{}) float64 {
    if len(a) == 0 || len(b) == 0 {
        return 0
    }
    
    matches := 0
    total := len(a) + len(b)
    
    for key, valueA := range a {
        if valueB, exists := b[key]; exists && valueA == valueB {
            matches += 2
        }
    }
    
    return float64(matches) / float64(total)
}

func (p *GraphPlacer) computeContentSimilarity(a, b string) float64 {
    // Simple Jaccard similarity on words
    wordsA := tokenize(a)
    wordsB := tokenize(b)
    
    setA := make(map[string]bool)
    for _, w := range wordsA {
        setA[w] = true
    }
    
    setB := make(map[string]bool)
    for _, w := range wordsB {
        setB[w] = true
    }
    
    intersection := 0
    for w := range setA {
        if setB[w] {
            intersection++
        }
    }
    
    union := len(setA) + len(setB) - intersection
    if union == 0 {
        return 0
    }
    
    return float64(intersection) / float64(union)
}

func tokenize(s string) []string {
    // Simple tokenization
    words := make([]string, 0)
    current := ""
    for _, r := range s {
        if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
            current += string(r)
        } else if current != "" {
            words = append(words, current)
            current = ""
        }
    }
    if current != "" {
        words = append(words, current)
    }
    return words
}

// determineEdges proposes edges for the new behavior
func (p *GraphPlacer) determineEdges(behavior *models.Behavior, existing []models.Behavior) []ProposedEdge {
    edges := make([]ProposedEdge, 0)
    
    // If this behavior has a more specific 'when' than an existing one,
    // it might override that existing behavior
    for _, e := range existing {
        if p.isMoreSpecific(behavior.When, e.When) {
            edges = append(edges, ProposedEdge{
                From: behavior.ID,
                To:   e.ID,
                Kind: "overrides",
            })
        }
    }
    
    return edges
}

func (p *GraphPlacer) isMoreSpecific(a, b map[string]interface{}) bool {
    // A is more specific than B if it has all of B's conditions plus more
    if len(a) <= len(b) {
        return false
    }
    
    for key, valueB := range b {
        if valueA, exists := a[key]; !exists || valueA != valueB {
            return false
        }
    }
    
    return true
}

func (p *GraphPlacer) nodeToBehavior(node store.Node) models.Behavior {
    // Convert node back to behavior
    // This is a simplified conversion
    b := models.Behavior{
        ID:   node.ID,
        Kind: models.BehaviorKind(node.Content["kind"].(string)),
    }
    
    if name, ok := node.Content["name"].(string); ok {
        b.Name = name
    }
    if when, ok := node.Content["when"].(map[string]interface{}); ok {
        b.When = when
    }
    if content, ok := node.Content["content"].(models.BehaviorContent); ok {
        b.Content = content
    }
    
    return b
}
```

---

## CLI Implementation

### cmd/bg/main.go

```go
package main

import (
    "fmt"
    "os"
    
    "github.com/spf13/cobra"
)

var version = "0.1.0"

func main() {
    rootCmd := &cobra.Command{
        Use:   "bg",
        Short: "Behavior graph management for AI agents",
        Long: `bg manages learned behaviors and conventions for AI coding agents.

It captures corrections, extracts reusable behaviors, and provides
context-aware behavior activation for consistent agent operation.`,
    }
    
    // Add subcommands
    rootCmd.AddCommand(
        newLearnCmd(),
        newActiveCmd(),
        newListCmd(),
        newShowCmd(),
        newWhyCmd(),
        newPendingCmd(),
        newApproveCmd(),
        newRejectCmd(),
        newInitCmd(),
        newVersionCmd(),
    )
    
    // Global flags
    rootCmd.PersistentFlags().Bool("json", false, "Output as JSON (for agent consumption)")
    rootCmd.PersistentFlags().String("root", ".", "Project root directory")
    
    if err := rootCmd.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

func newVersionCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "version",
        Short: "Print version information",
        Run: func(cmd *cobra.Command, args []string) {
            fmt.Printf("bg version %s\n", version)
        },
    }
}

func newInitCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "init",
        Short: "Initialize behavior tracking in current directory",
        RunE: func(cmd *cobra.Command, args []string) error {
            // TODO: Implement
            // 1. Create .behaviors directory
            // 2. Create manifest.yaml
            // 3. Create learned/ directory
            fmt.Println("Initialized .behaviors/")
            return nil
        },
    }
}

func newLearnCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "learn",
        Short: "Capture a correction for learning",
        Long: `Capture a correction from a human-agent interaction.

This command is called by agents when they receive a correction.
It extracts a reusable behavior from the correction and proposes
adding it to the behavior graph.`,
        RunE: func(cmd *cobra.Command, args []string) error {
            wrong, _ := cmd.Flags().GetString("wrong")
            right, _ := cmd.Flags().GetString("right")
            file, _ := cmd.Flags().GetString("file")
            task, _ := cmd.Flags().GetString("task")
            
            // TODO: Implement
            // 1. Build context from flags and environment
            // 2. Create correction
            // 3. Run learning loop
            // 4. Output result (JSON if --json flag)
            
            fmt.Printf("Learning from correction:\n")
            fmt.Printf("  Wrong: %s\n", wrong)
            fmt.Printf("  Right: %s\n", right)
            fmt.Printf("  File: %s\n", file)
            fmt.Printf("  Task: %s\n", task)
            
            return nil
        },
    }
    
    cmd.Flags().String("wrong", "", "What the agent did (required)")
    cmd.Flags().String("right", "", "What should have been done (required)")
    cmd.Flags().String("file", "", "Current file path")
    cmd.Flags().String("task", "", "Current task type")
    cmd.MarkFlagRequired("wrong")
    cmd.MarkFlagRequired("right")
    
    return cmd
}

func newActiveCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "active",
        Short: "Show behaviors active in current context",
        Long: `List all behaviors that are currently active based on the
current context (file, task, language, etc.).

Use --json for machine-readable output suitable for agent consumption.`,
        RunE: func(cmd *cobra.Command, args []string) error {
            file, _ := cmd.Flags().GetString("file")
            task, _ := cmd.Flags().GetString("task")
            jsonOut, _ := cmd.Flags().GetBool("json")
            
            // TODO: Implement
            // 1. Build context
            // 2. Query behaviors
            // 3. Evaluate activation conditions
            // 4. Resolve conflicts
            // 5. Output active behaviors
            
            if jsonOut {
                fmt.Println(`{"active": [], "context": {}}`)
            } else {
                fmt.Printf("Active behaviors (file=%s, task=%s):\n", file, task)
                fmt.Println("  (none yet)")
            }
            
            return nil
        },
    }
    
    cmd.Flags().String("file", "", "Current file path")
    cmd.Flags().String("task", "", "Current task type")
    
    return cmd
}

func newListCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "list",
        Short: "List all behaviors",
        RunE: func(cmd *cobra.Command, args []string) error {
            kind, _ := cmd.Flags().GetString("kind")
            jsonOut, _ := cmd.Flags().GetBool("json")
            
            // TODO: Implement
            
            if jsonOut {
                fmt.Println(`{"behaviors": []}`)
            } else {
                fmt.Printf("Behaviors (kind=%s):\n", kind)
                fmt.Println("  (none yet)")
            }
            
            return nil
        },
    }
    
    cmd.Flags().String("kind", "", "Filter by kind (directive, constraint, preference, procedure)")
    
    return cmd
}

func newShowCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "show [behavior-id]",
        Short: "Show details of a behavior",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            id := args[0]
            
            // TODO: Implement
            
            fmt.Printf("Behavior: %s\n", id)
            fmt.Println("  (not found)")
            
            return nil
        },
    }
}

func newWhyCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "why [behavior-id]",
        Short: "Explain why a behavior exists (provenance chain)",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            id := args[0]
            
            // TODO: Implement
            // Show the full provenance chain:
            // - When was it created
            // - If learned: what correction led to it
            // - If imported: from which package
            // - Approval status
            
            fmt.Printf("Provenance for: %s\n", id)
            fmt.Println("  (not found)")
            
            return nil
        },
    }
}

func newPendingCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "pending",
        Short: "List behaviors pending review",
        RunE: func(cmd *cobra.Command, args []string) error {
            jsonOut, _ := cmd.Flags().GetBool("json")
            
            // TODO: Implement
            
            if jsonOut {
                fmt.Println(`{"pending": []}`)
            } else {
                fmt.Println("Pending behaviors:")
                fmt.Println("  (none)")
            }
            
            return nil
        },
    }
}

func newApproveCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "approve [behavior-id]",
        Short: "Approve a pending behavior",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            id := args[0]
            
            // TODO: Implement
            
            fmt.Printf("Approved: %s\n", id)
            
            return nil
        },
    }
    
    return cmd
}

func newRejectCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "reject [behavior-id]",
        Short: "Reject a pending behavior",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            id := args[0]
            reason, _ := cmd.Flags().GetString("reason")
            
            // TODO: Implement
            
            fmt.Printf("Rejected: %s (reason: %s)\n", id, reason)
            
            return nil
        },
    }
    
    cmd.Flags().String("reason", "", "Rejection reason")
    cmd.MarkFlagRequired("reason")
    
    return cmd
}
```

---

## AGENTS.md Template

This goes in the behavior-graph project root for agents working ON this project:

```markdown
# Behavior Graph - Development Guide

## Project Overview

Building `bg` - a CLI tool for learning and managing agent behaviors.

**Tech stack:** Go, Cobra CLI, YAML, Beads (for storage)

**Read first:** `docs/SPEC.md` contains the full specification.

## Current Phase

Check `bd list` to see current tasks. Start with the highest priority incomplete task.

## Code Patterns

### CLI Commands (Cobra)

All commands go in `cmd/bg/main.go`. Pattern:

```go
func newXxxCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "xxx",
        Short: "One line description",
        RunE: func(cmd *cobra.Command, args []string) error {
            // Implementation
            return nil
        },
    }
    cmd.Flags().String("flag", "", "description")
    return cmd
}
```

### JSON Output

All commands must support `--json` flag for agent consumption:

```go
jsonOut, _ := cmd.Flags().GetBool("json")
if jsonOut {
    json.NewEncoder(os.Stdout).Encode(result)
} else {
    // Human-readable output
}
```

### Error Handling

Return errors, don't panic. Let Cobra handle display.

## Testing

Run tests with: `go test ./...`

Each package should have `*_test.go` files.

## When You Make Mistakes

If I correct you, capture it:

```bash
bg learn --wrong "what you did" --right "what I said to do"
```

(Once the tool is built, we'll use it to improve itself!)
```

---

## AGENTS.md for Projects USING the System

This goes in user projects that want agent behavior management:

```markdown
### Behavior System

This project uses `bg` for learned behaviors.

**Before starting work:**
```bash
bg active --file "$FILE" --task "$TASK" --json
```

**When I correct you:**
```bash
bg learn --wrong "what you did" --right "what I said" --file "$FILE"
```

**To see all behaviors:**
```bash
bg list --json
```
```

---

## Testing Strategy

### Unit Tests

Each package should have corresponding `_test.go` files:

```
internal/models/behavior_test.go      - Test context matching
internal/store/memory_test.go         - Test in-memory store operations
internal/learning/capture_test.go     - Test correction detection
internal/learning/extract_test.go     - Test behavior extraction
internal/learning/place_test.go       - Test graph placement
```

### Integration Tests

```
tests/integration/learning_loop_test.go  - End-to-end learning flow
tests/integration/cli_test.go            - CLI command integration
```

### Test Fixtures

```
tests/fixtures/
  corrections/           - Sample correction inputs
  behaviors/             - Sample behavior definitions
  contexts/              - Sample context snapshots
```

---

## Quick Start Commands

Run these in your terminal to set up the project:

```bash
# 1. Create and enter project directory
mkdir -p ~/projects/behavior-graph
cd ~/projects/behavior-graph

# 2. Initialize git
git init

# 3. Initialize Go module (replace with your GitHub username)
go mod init github.com/YOUR_USERNAME/behavior-graph

# 4. Add cobra dependency
go get github.com/spf13/cobra@latest
go get gopkg.in/yaml.v3

# 5. Create directory structure
mkdir -p cmd/bg internal/{models,store,learning,activation,assembly} pkg

# 6. Initialize Beads (if available)
bd init

# 7. Create initial files
touch cmd/bg/main.go
touch internal/models/{behavior,correction,context,provenance}.go
touch internal/store/{store,memory,beads}.go
touch AGENTS.md README.md

# 8. Copy this spec into the project
# (save this file as docs/SPEC.md)
mkdir -p docs
```

After setup, create initial beads to track work:

```bash
bd create "Phase 1: Core models and CLI skeleton" --type epic
bd create "Phase 2: Learning loop (capture → extract → place)" --type epic  
bd create "Phase 3: Activation and conflict resolution" --type epic
bd create "Phase 4: Beads persistence backend" --type epic
bd create "Phase 5: Package import/export" --type epic
```

---

## Implementation Order

### Phase 1: Foundation (Start Here)

**Goal**: Working CLI that can `init`, `version`, and store behaviors in memory.

**Files to create:**
```
cmd/bg/main.go           # CLI entry point with cobra
internal/models/behavior.go
internal/models/correction.go  
internal/models/context.go
internal/models/provenance.go
internal/store/store.go      # GraphStore interface
internal/store/memory.go     # InMemoryGraphStore for testing
```

**Commands to implement:**
- `bg version` - Print version
- `bg init` - Create .behaviors/ directory
- `bg list` - List all behaviors (empty initially)

**Success criteria**: `bg init && bg list --json` works.

---

### Phase 2: Learning Loop

**Goal**: Capture corrections and create candidate behaviors.

**Files to create:**
```
internal/learning/capture.go   # CorrectionCapture
internal/learning/extract.go   # BehaviorExtractor  
internal/learning/place.go     # GraphPlacer
internal/learning/loop.go      # LearningLoop orchestrator
```

**Commands to implement:**
- `bg learn --wrong "X" --right "Y"` - Capture a correction

**Success criteria**: 
```bash
bg learn --wrong "used pip" --right "use uv instead" --file "setup.py"
bg list  # Shows the learned behavior
```

---

### Phase 3: Activation

**Goal**: Query which behaviors are active for a given context.

**Files to create:**
```
internal/activation/context.go   # ContextBuilder
internal/activation/evaluate.go  # Predicate evaluation
internal/activation/resolve.go   # Conflict resolution
```

**Commands to implement:**
- `bg active --file "X" --task "Y"` - Show active behaviors
- `bg show <id>` - Show behavior details
- `bg why <id>` - Show provenance chain

**Success criteria**:
```bash
bg active --file "db/migrate.py" --json
# Returns behaviors matching that context
```

---

### Phase 4: Persistence

**Goal**: Behaviors survive between CLI invocations using Beads.

**Files to create:**
```
internal/store/beads.go  # BeadsGraphStore implementation
```

**Modify**: All commands to use BeadsGraphStore by default.

**Success criteria**: 
```bash
bg learn --wrong "X" --right "Y"
# Restart terminal
bg list  # Still shows the behavior
```

---

### Phase 5: Review Workflow

**Goal**: Human approval for learned behaviors.

**Commands to implement:**
- `bg pending` - List behaviors awaiting approval
- `bg approve <id>` - Approve a pending behavior
- `bg reject <id> --reason "..."` - Reject with reason

**Success criteria**: Learned behaviors start as pending, require approval to become active.

---

### Phase 6: Packages (Future)

**Goal**: Import/export behaviors as packages.

**Commands to implement:**
- `bg import github.com/user/repo`
- `bg export ./my-package`

---

## Dependencies

```go
// go.mod
module github.com/YOUR_USERNAME/behavior-graph

go 1.21

require (
    github.com/spf13/cobra v1.8.0
    gopkg.in/yaml.v3 v3.0.1
)
```

---

## Success Criteria

The system is working when:

1. **Learning works**: `bg learn --wrong "X" --right "Y"` creates a pending behavior
2. **Activation works**: `bg active --file foo.py` returns relevant behaviors
3. **Persistence works**: Behaviors survive between CLI invocations
4. **Integration works**: An agent following AGENTS.md can use the system

---

## Minimum Viable Product (MVP)

For the first working version, focus on:

**Must have:**
- [ ] `bg init` - Initialize .behaviors/ directory
- [ ] `bg learn --wrong "..." --right "..."` - Capture correction
- [ ] `bg list` - Show all behaviors  
- [ ] `bg active` - Show behaviors for current context
- [ ] `--json` flag on all commands for agent consumption
- [ ] InMemoryGraphStore (persistence can come later)

**Nice to have (Phase 2+):**
- [ ] Beads persistence
- [ ] Approval workflow
- [ ] Similarity detection
- [ ] Package import/export

**Explicitly defer:**
- Embedding-based similarity (use explicit declarations first)
- Web UI
- MCP integration
- Multi-user/team features

---

## Patterns from claude-reflect-system

The claude-reflect-system project has working pattern detection. Key patterns to detect corrections:

```go
// HIGH confidence - explicit corrections
var highConfidencePatterns = []string{
    "no, use",
    "don't use",
    "instead of",
    "never",
    "always",
    "stop using",
    "use X not Y",
    "wrong, it should be",
    "that's incorrect",
}

// MEDIUM confidence - approvals (reinforcement)
var mediumConfidencePatterns = []string{
    "yes, perfect",
    "that's right",
    "exactly",
    "good job",
    "that works",
}

// LOW confidence - suggestions
var lowConfidencePatterns = []string{
    "have you considered",
    "what about",
    "maybe try",
    "you could also",
}
```

Their approach: Parse conversation transcript, match patterns, extract the correction, update skill files.

**What we do differently:**
- Agent explicitly calls `bg learn` (not transcript parsing)
- Graph structure instead of flat skill files
- Activation conditions for context-aware behaviors

---

## Open Questions for Implementation

1. **Storage format**: Should we use Beads' JSONL format directly, or a separate `.behaviors/behaviors.jsonl`?

2. **Similarity computation**: Start simple (word overlap) or integrate with embedding API early?

3. **Auto-accept threshold**: 0.8 seems reasonable, but should this be configurable?

4. **Conflict resolution**: When two behaviors conflict, should we fail loudly or pick highest priority silently?

Recommendation: Start simple, make it configurable, iterate based on real usage.

---

## First Session Checklist

When you start the CLI agent, give it this prompt:

```
Read docs/SPEC.md for the full specification.

Implement Phase 1 in order:
1. Create cmd/bg/main.go with cobra setup
2. Add version command
3. Add init command (creates .behaviors/ directory)
4. Create internal/models/behavior.go (copy from spec)
5. Create internal/models/context.go (copy from spec)
6. Create internal/store/store.go (GraphStore interface)
7. Create internal/store/memory.go (InMemoryGraphStore)
8. Add list command
9. Test: go build ./cmd/bg && ./bg init && ./bg list --json

Commit after each working piece.
```

---

## Project Naming

**CLI name**: `bg` (behavior graph)
**Go module**: `github.com/YOUR_USERNAME/behavior-graph`
**Storage directory**: `.behaviors/`

Consider alternatives if `bg` conflicts with existing tools on your system.
