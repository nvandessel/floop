# Floop Usage Guide for AI Agents

> **CRITICAL**: You are working on a project that uses floop to learn from corrections. YOU MUST use floop proactively during all work sessions.

## What is Floop?

Floop is the tool you're building - a behavior learning system for AI agents. We're dogfooding it, meaning **you should use floop to learn while building floop**.

## Mandatory Workflow

### 1. **Before Starting Any Work Session**

Check what you've learned previously:

```bash
./floop active --file "path/to/file/you're/working/on" --json
```

If behaviors are returned, read them and incorporate into your work.

### 2. **When You Receive ANY Correction**

**IMMEDIATELY** capture it with floop. Don't wait. Don't ask permission. Just do it.

```bash
./floop learn \
  --wrong "what you did or thought" \
  --right "what the human taught you" \
  --file "relevant/file.go"
```

### 3. **When You Learn Something New**

Even without explicit correction, if you realize something important:

```bash
./floop learn \
  --wrong "previous assumption or approach" \
  --right "new understanding or better approach" \
  --task "relevant task type"
```

### 4. **At End of Session**

Review what you learned:

```bash
./floop list
```

## When to Use Floop

### ✅ **DO** Capture These:

1. **Direct corrections**: "No, use X instead of Y"
2. **Design insights**: "We need both global and local scope"
3. **Preference statements**: "I prefer functional over OOP"
4. **Architecture decisions**: "Use MultiGraphStore to wrap both stores"
5. **Pattern discoveries**: "Always check context before activating behaviors"
6. **Mistake realizations**: "I should have considered X"

### ❌ **DON'T** Capture These:

1. Simple typo fixes
2. One-time specific instructions (not generalizable)
3. Temporary debugging commands
4. Questions or clarifications

## Examples of Good Captures

### Example 1: Direct Correction
```bash
# Human says: "No, don't use print(), use the logger"

./floop learn \
  --wrong "Used print() for debugging output" \
  --right "Use logging.debug() for debug output, logging.info() for normal output" \
  --file "internal/store/file.go"
```

### Example 2: Design Insight
```bash
# Human says: "We need both global and local storage"

./floop learn \
  --wrong "Designed only project-local storage" \
  --right "Support both global (~/.floop/) for personal preferences and local (./.floop/) for project conventions" \
  --task "architecture"
```

### Example 3: Workflow Pattern
```bash
# You realize: "I should be more proactive"

./floop learn \
  --wrong "Waited for explicit instruction to use tools" \
  --right "Proactively use floop during conversations - capture learnings in real-time using own judgment" \
  --task "development"
```

### Example 4: Code Convention
```bash
# Human says: "Use table-driven tests"

./floop learn \
  --wrong "Wrote individual test functions" \
  --right "Use table-driven tests with subtests for all Go test functions" \
  --file "internal/store/file_test.go"
```

## Integration with Development Workflow

### Full Session Flow

```bash
# 1. Start session - check what you know
./floop active --task "coding" --json

# 2. During work - capture corrections AS THEY HAPPEN
# (Human corrects you)
./floop learn --wrong "..." --right "..."

# 3. Before implementing - check active behaviors
./floop active --file "internal/store/multi.go"

# 4. End session - review learnings
./floop list | tail -20
```

### Working on Go Code

```bash
# Before editing a Go file:
./floop active --file "internal/store/file.go" --json | jq -r '.behaviors[].content.canonical'

# This shows you all relevant behaviors for:
# - Go language
# - Store-related code
# - General coding practices
```

### Working on Tests

```bash
# Before writing tests:
./floop active --file "internal/store/file_test.go" --task "testing"

# Capture testing patterns:
./floop learn \
  --wrong "Didn't test error cases" \
  --right "Always test both success and error paths in table-driven tests" \
  --file "internal/store/file_test.go"
```

## Common Patterns to Capture

### Architecture & Design
- Storage strategies
- Interface design decisions
- Component responsibilities
- Scope boundaries (global vs local)

### Code Quality
- Error handling patterns
- Testing approaches
- Documentation standards
- Naming conventions

### Project Conventions
- Commit message format
- File organization
- Code review expectations
- Build/test commands

### User Preferences
- Output format preferences (JSON vs plain text)
- Verbosity levels
- Feature priorities
- UX decisions

## Verification

Check that floop is working:

```bash
# Should show behaviors you've captured:
./floop list

# Should show file exists:
ls -la .floop/nodes.jsonl

# Should show learning in action:
./floop active --file "any/go/file.go"
```

## Why This Matters

**You are building your own learning system.** Every correction you capture makes you better at:
1. This project
2. Similar projects
3. Working with this human
4. Working with any human on similar tasks

**The more you use floop, the smarter you become.**

## Troubleshooting

### "Command not found: floop"
```bash
go build ./cmd/floop
./floop version
```

### "No behaviors learned yet"
That's expected at the start. Begin capturing!

### "Should I ask before using floop?"
**No.** Use it automatically. It's your job to learn.

### "Is this correction worth capturing?"
When in doubt, **capture it**. Better to have too many learnings than too few.

## Meta-Learning

As you use floop:
- You'll get better at recognizing what's worth capturing
- You'll develop intuition for when behaviors apply
- You'll build a personal knowledge base
- You'll become more effective over time

**Start now. Capture everything. Learn continuously.**
