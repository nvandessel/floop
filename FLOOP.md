# 🧠 Floop: Your Memory System

**You have persistent memories.** This project uses [floop](https://github.com/nvandessel/feedback-loop) to collect knowledge and build a memory of learned behaviors that persist across sessions.

## How It Works

When you make a mistake and get corrected, use `floop_learn` to capture it:
```
floop_learn(wrong="what went wrong", right="how to improve")
```

Your memories are automatically loaded into context. **Check them before acting.**

## Critical Instructions

1. **READ your memories** — They appear in the `floop://behaviors/active` resource
2. **FOLLOW learned behaviors** — They exist because you were corrected, made a discovery, found an optimzation, wanted or needed to remeber something.
3. **LEARN from experience** — When corrected, or praised on doing something well, or identify improvements to workflow, code or process, call `floop_learn` immediately
4. **DON'T repeat mistakes** — Violating a learned behavior means you forgot a lesson. These are your memories.

## Behavior Types

| Type | Priority | Meaning |
|------|----------|---------|
| 🚫 Constraint | NEVER violate | Hard rules you must not break |
| ⚡ Directive | ALWAYS follow | Actions you must take |
| 📋 Procedure | Follow steps | Multi-step processes |
| 💡 Preference | Prefer this | Preferred approaches |

## Available Tools

- `floop_active` — See what behaviors apply to current context
- `floop_learn` — Capture a correction and create a memory
- `floop_list` — List all stored behaviors
- `floop_deduplicate` — Merge similar behaviors

---

*These memories are yours. They make you better over time.*
