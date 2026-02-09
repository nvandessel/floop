# Go Development Guidelines

This guide outlines the coding standards for the feedback-loop project.

## 1. Project Structure

```
feedback-loop/
├── cmd/floop/          # CLI entry point
│   └── main.go
├── internal/           # Private application code
│   ├── activation/     # Context building, predicate evaluation, conflict resolution
│   ├── assembly/       # Behavior compilation, token optimization
│   ├── backup/         # Backup and restore operations
│   ├── config/         # Configuration management
│   ├── constants/      # Shared constants
│   ├── dedup/          # Behavior deduplication and merging
│   ├── hooks/          # AI tool hook integration
│   ├── learning/       # Correction capture, extraction, placement
│   ├── llm/            # LLM client interfaces and implementations
│   ├── mcp/            # MCP server protocol handlers
│   ├── models/         # Core data types (Behavior, Correction, Context)
│   ├── pathutil/       # Path utilities
│   ├── ranking/        # Behavior scoring and ranking
│   ├── ratelimit/      # Rate limiting
│   ├── sanitize/       # Input sanitization
│   ├── seed/           # Pre-seeded behaviors
│   ├── session/        # Session state tracking
│   ├── spreading/      # Spreading activation engine
│   ├── store/          # GraphStore interface and implementations
│   ├── summarization/  # Behavior summarization
│   ├── tiering/        # Token budget tiering
│   ├── utils/          # Shared utilities
│   └── visualization/  # Graph visualization
├── docs/               # Documentation
└── .floop/             # Runtime data (JSONL + manifest tracked; DB + audit.jsonl gitignored)
```

## 2. Code Style

### Formatting
- Always use `gofmt` or `goimports`
- Run `go fmt ./...` before committing

### Naming
- `CamelCase` for exported identifiers
- `camelCase` for unexported identifiers
- Short but descriptive names (`ctx` for context, `b` for behavior in loops)
- Package names: short, lowercase, singular (`models`, `store`, `learning`)

### Error Handling
- Return errors as the last return value
- Check errors immediately after function calls
- Wrap errors with context using `fmt.Errorf("doing X: %w", err)`
- Never panic except for truly unrecoverable initialization errors

```go
// Good
result, err := doSomething()
if err != nil {
    return fmt.Errorf("failed to do something: %w", err)
}

// Bad
result, _ := doSomething()  // Don't ignore errors
```

## 3. Testing

### Requirements
- All packages must have `*_test.go` files
- Use table-driven tests for functions with multiple input cases
- Test both success and error paths

### Running Tests
```bash
go test ./...                    # Run all tests
go test ./internal/models/...    # Run specific package
go test -v -cover ./...          # Verbose with coverage
```

### Test File Structure
```go
func TestFunctionName(t *testing.T) {
    tests := []struct {
        name    string
        input   InputType
        want    OutputType
        wantErr bool
    }{
        {"valid input", validInput, expectedOutput, false},
        {"invalid input", invalidInput, nil, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := FunctionName(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if !reflect.DeepEqual(got, tt.want) {
                t.Errorf("got %v, want %v", got, tt.want)
            }
        })
    }
}
```

## 4. Interfaces

### Design Principles
- Define interfaces where they're used, not where they're implemented
- Keep interfaces small (1-3 methods when possible)
- Use interfaces for testability (e.g., `GraphStore` interface enables `InMemoryGraphStore` for tests)

```go
// Good: interface defined in consumer package
type Store interface {
    GetNode(ctx context.Context, id string) (*Node, error)
}

// Implementation in store package
type InMemoryGraphStore struct { ... }
func (s *InMemoryGraphStore) GetNode(ctx context.Context, id string) (*Node, error) { ... }
```

## 5. Context Usage

- Pass `context.Context` as the first parameter
- Use context for cancellation and timeouts, not for passing data
- Never store context in structs

```go
// Good
func (s *Store) GetNode(ctx context.Context, id string) (*Node, error)

// Bad
func (s *Store) GetNode(id string) (*Node, error)  // Missing context
```

## 6. JSON/YAML Struct Tags

- All serializable structs must have explicit tags
- Use `omitempty` for optional fields
- Match JSON field names to the canonical API format

```go
type Behavior struct {
    ID         string            `json:"id" yaml:"id"`
    Name       string            `json:"name" yaml:"name"`
    When       map[string]any    `json:"when,omitempty" yaml:"when,omitempty"`
    Confidence float64           `json:"confidence" yaml:"confidence"`
}
```

## 7. Documentation

- Add comments to all exported functions and types
- Use complete sentences starting with the identifier name
- Document non-obvious behavior, not what's obvious from the code

```go
// Behavior represents a unit of agent behavior that can be
// activated based on context conditions.
type Behavior struct { ... }

// Matches checks if this context matches a 'when' predicate.
// It supports exact matching, array membership, and glob patterns.
func (c *ContextSnapshot) Matches(predicate map[string]any) bool { ... }
```

## 8. Dependencies

- Use `go mod` for dependency management
- Pin specific versions in `go.mod`
- Minimize external dependencies
- Current dependencies:
  - `github.com/spf13/cobra` - CLI framework
  - `gopkg.in/yaml.v3` - YAML parsing

## 9. CLI Patterns (Cobra)

### Command Structure
```go
func newXxxCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "xxx",
        Short: "One line description",
        Long:  `Longer description with examples.`,
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

## 10. Concurrency

- Use channels for goroutine communication
- Use `sync.Mutex` for protecting shared state
- Avoid global mutable state
- Document goroutine safety in struct comments
