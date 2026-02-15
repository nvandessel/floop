# Feedback Loop - Agent Instructions

> **For floop contributors.** If you're a user, see [docs/integrations/](docs/integrations/) for setup guides.

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
- `floop_feedback` - Signal whether a behavior was helpful (`confirmed`) or contradicted (`overridden`)
- `floop_list` - List all stored behaviors
- `floop_deduplicate` - Merge duplicate behaviors

---

## Project Overview

**feedback-loop** (`floop`) is a CLI tool that enables AI agents to learn from corrections and maintain consistent behavior across sessions.

**Tech stack:** Go 1.25+, Cobra CLI, YAML, Beads (issue tracking)

## Essential Reading

1. `docs/GO_GUIDELINES.md` - Go coding standards (read before writing code)

## Quick Reference

### Issue Tracking (Beads)
```bash
bv --robot-triage             # Get ranked recommendations
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
1. **Run quality gates** (if code changed):
   - `go test ./...` — Run tests
   - `go fmt ./...` — Format code
   - If `cmd/floop/` changed: verify `docs/CLI_REFERENCE.md` is current
2. Close the issue: `bd close <id> --reason "..."`
3. Sync beads: `bd sync`
4. Commit changes on a feature branch
5. Push and create a PR — **never commit directly to main**
6. Wait for review before merging

## Project Structure

- **`cmd/floop/`** — CLI entry point
- **`internal/`** — All application packages. Run `ls internal/` for current list.
- **`docs/`** — Documentation (`GO_GUIDELINES.md`, `FLOOP_USAGE.md`, `integrations/`)
- **`.floop/`** — Learned behaviors (JSONL + manifest tracked; DB + audit.jsonl gitignored)
- **`.beads/`** — Issue tracking (version controlled)

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

Check `bd ready` for current tasks.

## Session Completion (Landing the Plane)

**When ending a work session**, you MUST complete ALL steps below.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **Sync beads** - `bd sync`
5. **Commit and push on a branch** - Never commit directly to main:
   ```bash
   git checkout -b chore/session-cleanup  # or use existing feature branch
   git add <specific files>
   git commit -m "chore: sync beads state"
   git push -u origin HEAD
   ```
6. **Create PR** - `gh pr create` and present to user for review
7. **Clean up** - Clear stashes, prune remote branches
8. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- **NEVER** commit directly to main — always use a feature branch + PR
- **NEVER** merge PRs without user review — present PRs and wait for approval
- NEVER stop before pushing your branch — that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push to a branch
- If push fails, resolve and retry until it succeeds

### Using bv as an AI Sidecar

For graph-aware issue triage, use `bv` with `--robot-*` flags. See **[docs/BV_SIDECAR.md](docs/BV_SIDECAR.md)** for full documentation.

**Quick start:**
```bash
bv --robot-triage    # Get ranked recommendations
bv --robot-next      # Get single top pick
```

**CRITICAL:** Use ONLY `--robot-*` flags. Bare `bv` launches an interactive TUI that blocks your session.

