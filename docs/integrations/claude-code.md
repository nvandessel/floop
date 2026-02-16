# Claude Code Integration

## Overview

Claude Code has native support for floop through two mechanisms:

1. **MCP Server** - Full bidirectional integration (read behaviors, capture corrections)
2. **Hooks** - Automatic behavior injection via native Go subcommands

This guide covers hook-based integration, which auto-injects learned behaviors into Claude's context using `floop hook` subcommands. No shell scripts or external dependencies (bash, jq) required — works on Linux, macOS, and Windows. For MCP server setup, see [mcp-server.md](./mcp-server.md).

## Quick Start

### Automatic Setup

```bash
# Initialize floop with automatic hook configuration
cd /path/to/your/project
floop init
```

If Claude Code is detected (`.claude/` directory exists), floop automatically configures hooks that inject behaviors at session start, on first prompt, during tool use, and detect corrections from user prompts.

**Example output:**
```
Created .floop/

Detected AI tools:
  - Claude Code

Configuring hooks...
  - Claude Code: updated .claude/settings.json

Behaviors will auto-inject at session start.
```

### Manual Setup

If you prefer manual configuration or `floop init` didn't detect Claude Code:

1. Create `.claude/settings.json` (or edit existing):

```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "floop hook session-start"
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "floop hook first-prompt"
          },
          {
            "type": "command",
            "command": "floop hook detect-correction"
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "floop hook dynamic-context"
          }
        ]
      }
    ]
  }
}
```

2. Initialize floop if not already done:
```bash
floop init --hooks=false
```

## How It Works

### Hook Mechanism

Claude Code supports hooks that execute at various lifecycle points. floop registers native Go subcommands at four hook points:

```
Claude starts session
    │
    └─→ SessionStart hook fires
    │      └─→ floop hook session-start → injects active behaviors
    │
    └─→ User submits first prompt
    │      └─→ floop hook first-prompt → injects behaviors (deduped per session)
    │      └─→ floop hook detect-correction → checks for corrections
    │
    └─→ Claude calls Read/Edit/Write/Bash tool
           └─→ floop hook dynamic-context → spreading activation for relevant behaviors
```

### What Gets Injected

The hook subcommands output context-aware behaviors in markdown format:

```markdown
# Learned Behaviors

## Directives (ALWAYS follow)

### use-cobra-for-cli
Use spf13/cobra for CLI command structure

### error-logging-to-stderr
Use fmt.Fprintln(os.Stderr, err) for error output

## Constraints (NEVER violate)

### no-panic-in-handlers
Never use panic() in HTTP handlers - return errors instead
```

## Configuration Options

### Global Hooks

To inject behaviors in ALL Claude Code projects (not just the current one):

```bash
# Initialize global floop
floop init --global

# This configures hooks in ~/.claude/settings.json
```

Global behaviors combine with project-local behaviors, with local taking precedence.

### Disable Hooks

If you don't want hook auto-configuration:

```bash
floop init --hooks=false
```

You can still use floop's MCP server or CLI commands manually.

## Verification

### Check Hook Configuration

```bash
cat .claude/settings.json
```

Should show hooks with `floop hook session-start`, `floop hook first-prompt`, `floop hook detect-correction`, and `floop hook dynamic-context` commands.

### Test Behavior Injection

```bash
# See what would be injected
floop prompt --format markdown
```

### Verify in Claude Code Session

1. Start a new Claude Code session in your project
2. Ask Claude to read a file
3. Check that Claude mentions or acknowledges the learned behaviors

## Troubleshooting

### Hooks Not Triggering

**Problem**: Behaviors aren't appearing in Claude's responses

**Solutions**:
- Verify `.claude/settings.json` exists and has correct syntax
- Check that `floop` is in your PATH: `which floop`
- Test the command manually: `echo '{}' | floop hook session-start`
- Restart Claude Code to pick up configuration changes

### Empty Behavior Output

**Problem**: `floop prompt` returns nothing

**Solutions**:
- Ensure floop is initialized: `floop init`
- Check for behaviors: `floop list`
- Verify `.floop/` directory exists and contains behavior files

### Existing Settings Conflict

**Problem**: floop init overwrites other settings

**Solution**: floop merges its hooks with existing settings. If you have conflicts:

1. Backup your settings: `cp .claude/settings.json .claude/settings.json.bak`
2. Run `floop init`
3. Manually merge if needed

### Hook Runs Too Often

**Problem**: Behaviors inject on every file read

**Solution**: This is by design - behaviors are context-aware and filter based on the current file. To reduce frequency, you can modify the matcher:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Read",
        "hooks": [
          {
            "type": "command",
            "command": "floop prompt --format markdown --file ${file}"
          }
        ]
      }
    ]
  }
}
```

## Best Practices

### 1. Keep Behaviors Focused

Inject only relevant behaviors by maintaining good `when` predicates:

```bash
# Learn with context
floop learn --wrong "used print for errors" --right "use stderr" --file main.go
```

### 2. Use Both MCP and Hooks

For full integration:
- **Hooks**: Auto-inject behaviors at session start
- **MCP**: Let Claude capture corrections during development

See [mcp-server.md](./mcp-server.md) for MCP setup.

### 3. Review Injected Behaviors Regularly

```bash
# See what's being injected
floop prompt --format markdown

# Audit all behaviors
floop list

# Remove outdated behaviors
floop forget <behavior-id>
```

### 4. Test Before Committing

After modifying hook configuration:

```bash
# Verify JSON syntax
python -m json.tool .claude/settings.json

# Test behavior output
floop prompt --format markdown
```

## Advanced Configuration

### Standalone Prompt Command

For custom integrations outside of Claude Code hooks, you can use the `floop prompt` command directly:

```bash
# Generate behavior injection text
floop prompt --format markdown --token-budget 1000

# XML format for other tools
floop prompt --format xml
```

### Conditional Injection

Inject only for specific file types by using file patterns in behaviors rather than modifying hooks. The hook subcommands automatically filter based on context (file type, task type, etc.).

## Comparison: Hooks vs MCP

| Feature | Hooks | MCP Server |
|---------|-------|------------|
| Setup complexity | Simple | Moderate |
| Behavior reading | Yes | Yes |
| Correction capture | No | Yes |
| Real-time learning | No | Yes |
| Requires running server | No | Yes |

**Recommendation**: Use hooks for simple behavior injection. Add MCP server for full feedback loop with automatic learning.

## Next Steps

- [MCP Server Integration](./mcp-server.md) - Full bidirectional integration

---

**floop** - The feedback loop for AI coding agents
