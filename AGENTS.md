# Feedback Loop - Agent Instructions

## Project Overview

**feedback-loop** (`floop`) is a CLI tool that enables AI agents to learn from corrections and maintain consistent behavior across sessions.

**Tech stack:** Go 1.25+, Cobra CLI, YAML, Beads (issue tracking)

## Essential Reading

1. `docs/SPEC.md` - Full technical specification
2. `docs/GO_GUIDELINES.md` - Go coding standards (read before writing code)
3. `docs/PLAN.md` - Current implementation plan and task breakdown

## Quick Reference

### Issue Tracking (Beads)
```bash
bd ready              # Find available work (no blockers)
bd show <id>          # View issue details
bd update <id> --status in_progress  # Claim work
bd close <id> --reason "..."         # Complete work
bd create "Title" --type task --priority 2 --description "..."
bd sync               # Sync changes
```

### Feedback Loop (Dogfooding)
```bash
floop learn --wrong "what you did" --right "what should be done" --file "path"
floop list --corrections    # View captured corrections
```

**Use floop when you make mistakes!** Capture corrections to build the learning dataset.

### Development
```bash
go build ./cmd/floop        # Build CLI
go install ./cmd/floop      # Install globally
go test ./...               # Run all tests
go test -v -cover ./...     # Verbose with coverage
go fmt ./...                # Format code
```

## Development Workflow

### Starting Work
1. Run `bd ready` to find available tasks
2. Read the task with `bd show <id>`
3. Claim it: `bd update <id> --status in_progress`
4. Check dependencies - some tasks block others

### Writing Code
1. **Read GO_GUIDELINES.md first** - follow the patterns
2. **Write tests** - all packages need `*_test.go` files
3. **Test coverage** - test both success and error paths
4. **Format code** - run `go fmt ./...` before committing

### Making Commits
- Make small, incremental commits
- Use conventional commit format:
  - `feat:` new features
  - `fix:` bug fixes
  - `docs:` documentation
  - `test:` test additions
  - `chore:` maintenance
- Include `Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>`

### Completing Work
1. Ensure tests pass: `go test ./...`
2. Format code: `go fmt ./...`
3. Close the issue: `bd close <id> --reason "..."`
4. Sync beads: `bd sync`
5. Commit and push changes

## Project Structure

```
feedback-loop/
├── cmd/floop/main.go       # CLI entry point
├── internal/
│   ├── models/             # Behavior, Correction, Context, Provenance
│   ├── store/              # GraphStore interface, InMemoryGraphStore, BeadsGraphStore
│   ├── learning/           # CorrectionCapture, BehaviorExtractor, GraphPlacer
│   ├── activation/         # ContextBuilder, predicate evaluation, conflict resolution
│   └── assembly/           # Behavior compilation for prompts
├── docs/
│   ├── SPEC.md             # Full specification
│   ├── GO_GUIDELINES.md    # Coding standards
│   └── PLAN.md             # Implementation plan
├── .floop/                 # Corrections and learned behaviors (version controlled)
└── .beads/                 # Issue tracking (version controlled)
```

## Code Patterns

### CLI Commands
```go
func newXxxCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "xxx",
        Short: "One line description",
        RunE: func(cmd *cobra.Command, args []string) error {
            jsonOut, _ := cmd.Flags().GetBool("json")
            // Implementation
            if jsonOut {
                json.NewEncoder(os.Stdout).Encode(result)
            }
            return nil
        },
    }
    return cmd
}
```

### Error Handling
```go
result, err := doSomething()
if err != nil {
    return fmt.Errorf("context for what failed: %w", err)
}
```

### Testing
```go
func TestFunction(t *testing.T) {
    tests := []struct {
        name    string
        input   Type
        want    Type
        wantErr bool
    }{
        {"valid case", input, expected, false},
        {"error case", badInput, nil, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test implementation
        })
    }
}
```

## When You Make Mistakes

Capture corrections using floop:

```bash
floop learn --wrong "what you did" --right "what I said to do" --file "relevant/file.go"
```

This builds the dataset we'll use to train the learning loop.

## Session Protocol

### Before Ending Any Session

1. **Run tests**: `go test ./...`
2. **Format code**: `go fmt ./...`
3. **Update issues**: Close completed work, note progress on in-progress items
4. **Commit changes**: Small, incremental commits
5. **Sync and push**:
   ```bash
   bd sync
   git push
   ```
6. **Verify**: `git status` shows "up to date with origin"

**Work is NOT complete until pushed to remote.**

## Current Phase

Check `bd ready` for current tasks. See `docs/PLAN.md` for the full implementation roadmap.

Phase 1 focus: Core models, GraphStore interface, and CLI commands.
