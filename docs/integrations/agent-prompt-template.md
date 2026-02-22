# Agent Prompt Template

Ready-to-paste instructions for teaching any AI coding agent how to use floop.

## Prerequisites

1. Install floop and run `floop init` in your project root.
2. Configure the MCP server for your tool — see the [compatibility matrix](./README.md) for tool-specific config snippets.

## MCP Template (Bidirectional)

For tools that support MCP (Cursor, Cline, Windsurf, Continue.dev, GitHub Copilot, etc.).

Copy everything between the fences into your tool's instruction file:

````markdown
### floop — Learned Behaviors

You have access to floop, a persistent memory system that stores learned
behaviors from past corrections. floop MCP tools are available in your
tool list.

#### Loading Behaviors

At the beginning of each new task or conversation, call `floop_active`
with the current file path (`file`) and task type (`task`, e.g.
"development", "testing", "refactoring") to load relevant behaviors.
Follow the returned behaviors as working instructions.

Note: If your tool auto-loads MCP resources, active behaviors may already
be in your context via `floop://behaviors/active`. Check before calling
`floop_active` to avoid redundant queries.

#### When Corrected

When the user corrects you, immediately call `floop_learn` with:
- `wrong`: what you did that was incorrect
- `right`: what the correct approach is
- `file`: the relevant file path (if applicable)
- `task`: the current task type (if applicable)

Capture corrections immediately — this is expected behavior and does not
require user confirmation. Duplicate behaviors are automatically merged.

#### Confirming or Overriding Behaviors

After following a behavior and finding it helpful, call `floop_feedback`
with the `behavior_id` and signal `"confirmed"`.
If you deliberately deviate from a behavior, call `floop_feedback` with
signal `"overridden"`.

Behavior IDs are returned in the `floop_active` response. If behaviors
were auto-loaded and you don't have IDs, call `floop_list` to find them.

#### Browsing and Maintenance

- Call `floop_list` to see all stored behaviors.
- Call `floop_list` with `tag` to filter by topic.
- Call `floop_deduplicate` periodically to merge duplicate behaviors.

#### Troubleshooting

If floop tools return errors about missing or uninitialized state, run
`floop init` in the project root.
````

> **Note:** Actual callable tool names may vary by integration. For example,
> Claude Code uses `mcp__floop__floop_active` while Cursor uses `floop_active`.
> Use whatever names appear in your tool's MCP tool list.

## Static Template (Read-Only)

For tools without MCP support (Aider, OpenAI Codex CLI, or any tool where you prefer a simpler setup).

Run this command to generate a snapshot of your current behaviors:

```bash
floop prompt
```

Paste the output into your tool's instruction file (`.cursorrules`, `copilot-instructions.md`, `agents.md`, etc.).

**Limitations:** Static mode is **read-only** — the agent can follow behaviors but cannot capture new corrections automatically. To capture corrections, run `floop learn` manually in your terminal:

```bash
floop learn --wrong 'what the agent did wrong' --right 'what it should do instead'
```

Or switch to an MCP-capable tool for bidirectional support.

**Refresh reminder:** Static output is a point-in-time snapshot. Regenerate with `floop prompt` after capturing new corrections to pick up new behaviors.

**Automated refresh:** Use a wrapper script to regenerate behaviors before each session:

```bash
#!/bin/bash
# Regenerate floop behaviors before each session
floop prompt > .cursorrules-floop
```

> Ensure `floop init` has been run first, or `floop prompt` will output an
> initialization warning instead of behaviors.

## Where to Paste

See the [compatibility matrix](./README.md) for your tool's instruction file location.
