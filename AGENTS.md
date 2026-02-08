# Feedback Loop - Agent Instructions

## Floop Integration (REQUIRED)

You have persistent memory via floop. Your learned behaviors are auto-injected at session start via hooks.

**When corrected, IMMEDIATELY capture it:**
```
mcp__floop__floop_learn(wrong="what you did", right="what to do instead")
```

Do NOT wait for permission. Capture learnings proactively. The hooks will also auto-detect corrections, but explicit capture is more reliable.

**Available MCP tools:**
- `floop_active` - See currently active behaviors for this context
- `floop_learn` - Capture a correction (USE PROACTIVELY)
- `floop_list` - List all stored behaviors
- `floop_deduplicate` - Merge duplicate behaviors

---

## Project Overview

**feedback-loop** (`floop`) is a CLI tool that enables AI agents to learn from corrections and maintain consistent behavior across sessions.

**Tech stack:** Go 1.25+, Cobra CLI, YAML, Beads (issue tracking)

## Essential Reading

1. `docs/SPEC.md` - Full technical specification
2. `docs/GO_GUIDELINES.md` - Go coding standards (read before writing code)
3. `docs/PLAN.md` - Current implementation plan and task breakdown

## Quick Reference

### Issue Tracking (Beads)
```bashbv --robot-triage 
bd ready              # Find available work (no blockers)
bd show <id>          # View issue details
bd update <id> --status in_progress  # Claim work
bd close <id> --reason "..."         # Complete work
bd create "Title" --type task --priority 2 --description "..."
bd sync               # Sync changes
```

### Feedback Loop (Dogfooding) ⭐

Use floop MCP tools proactively. Capture corrections as they happen - don't wait for permission.

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
1. Run `bv --robot-triage` to find available tasks
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
│   ├── store/              # GraphStore interface, InMemoryGraphStore, FileGraphStore
│   ├── learning/           # CorrectionCapture, BehaviorExtractor, GraphPlacer
│   ├── activation/         # ContextBuilder, predicate evaluation, conflict resolution
│   └── assembly/           # Behavior compilation for prompts
├── docs/
│   ├── SPEC.md             # Full specification
│   ├── GO_GUIDELINES.md    # Coding standards
│   └── PLAN.md             # Implementation plan
├── .floop/                 # Corrections and learned behaviors (data NOT version controlled)
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

Use `floop_learn` to capture corrections immediately. This builds the dataset for the learning loop.

## Current Phase

Check `bd ready` for current tasks. See `docs/PLAN.md` for the full implementation roadmap.

Phase 1 focus: Core models, GraphStore interface, and CLI commands.

## Session Completion (Landing the Plane)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

### Using bv as an AI Sidecar

For graph-aware issue triage, use `bv` with `--robot-*` flags. See **[docs/BV_SIDECAR.md](docs/BV_SIDECAR.md)** for full documentation.

**Quick start:**
```bash
bv --robot-triage    # Get ranked recommendations
bv --robot-next      # Get single top pick
```

**CRITICAL:** Use ONLY `--robot-*` flags. Bare `bv` launches an interactive TUI that blocks your session.

