# floop MCP Server Integration

## Overview

The `floop mcp-server` command exposes floop as a Model Context Protocol (MCP) server, enabling deep integration with AI coding tools. This allows AI agents to query active behaviors and capture corrections automatically, creating a true feedback loop without manual intervention.

### Supported AI Tools

- **Continue.dev** (Agent mode)
- **Cursor** (Composer and Agent modes)
- **Cline** (formerly Claude Dev)
- **Windsurf** / **Cascade**
- **GitHub Copilot** (Agent mode - via VSCode extension)

### Benefits

- **Bidirectional Integration**: Tools can both read behaviors AND write corrections automatically
- **Context-Aware**: Behaviors activate based on current file, task, and environment
- **Zero Manual Overhead**: No need to run `floop` commands manually
- **Automatic Learning**: Corrections captured during development are immediately available
- **Consistent Behavior**: All AI agents see the same learned behaviors

## Quick Start

### 1. Start the MCP Server

```bash
# Start server in your project directory
cd /path/to/your/project
floop mcp-server

# Or specify project root explicitly
floop mcp-server --root /path/to/project
```

The server runs as a stdio-based JSON-RPC service, reading requests from stdin and writing responses to stdout.

### 2. Configure Your AI Tool

Add floop to your AI tool's MCP server configuration (see tool-specific sections below).

### 3. Use MCP Tools in Agent Mode

Your AI tool can now invoke floop tools:
- **floop_active** - Get behaviors relevant to current context
- **floop_learn** - Capture corrections during development
- **floop_list** - Browse all learned behaviors

## Tool Reference

### floop_active

Get active behaviors for the current context.

**Parameters:**
- `file` (string, optional): Current file path
- `task` (string, optional): Task type (e.g., "development", "testing", "refactoring")

**Example Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "floop_active",
    "arguments": {
      "file": "cmd/floop/main.go",
      "task": "development"
    }
  },
  "id": 1
}
```

**Example Response:**
```json
{
  "jsonrpc": "2.0",
  "result": {
    "context": {
      "file": "cmd/floop/main.go",
      "language": "go",
      "task": "development"
    },
    "active": [
      {
        "id": "behavior-a1b2c3d4",
        "name": "use-cobra-for-cli",
        "kind": "directive",
        "content": {
          "canonical": "Use spf13/cobra for CLI command structure",
          "expanded": "When building CLI commands in Go, use the Cobra framework following the pattern: create newXxxCmd() functions that return *cobra.Command..."
        },
        "confidence": 0.95,
        "when": {
          "language": "go",
          "file_pattern": "cmd/*/main.go"
        }
      }
    ],
    "count": 1
  },
  "id": 1
}
```

---

### floop_learn

Capture a correction and extract a reusable behavior.

**Parameters:**
- `wrong` (string, required): What the agent did that needs correction
- `right` (string, required): What should have been done instead
- `file` (string, optional): Relevant file path for context

**Example Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "floop_learn",
    "arguments": {
      "wrong": "Used fmt.Println for error logging",
      "right": "Use fmt.Fprintln(os.Stderr, err) for error output",
      "file": "cmd/floop/main.go"
    }
  },
  "id": 2
}
```

**Example Response:**
```json
{
  "jsonrpc": "2.0",
  "result": {
    "correction_id": "correction-x7y8z9",
    "behavior_id": "behavior-e5f6g7h8",
    "auto_accepted": true,
    "confidence": 0.85,
    "requires_review": false,
    "message": "Learned behavior: error-logging-to-stderr"
  },
  "id": 2
}
```

---

### floop_list

List all behaviors or corrections.

**Parameters:**
- `corrections` (boolean, optional): If true, list corrections instead of behaviors (default: false)

**Example Request (list behaviors):**
```json
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "floop_list",
    "arguments": {}
  },
  "id": 3
}
```

**Example Response:**
```json
{
  "jsonrpc": "2.0",
  "result": {
    "behaviors": [
      {
        "id": "behavior-a1b2c3d4",
        "name": "use-cobra-for-cli",
        "kind": "directive",
        "confidence": 0.95,
        "source": "learned",
        "created_at": "2026-01-28T10:30:00Z"
      },
      {
        "id": "behavior-e5f6g7h8",
        "name": "error-logging-to-stderr",
        "kind": "directive",
        "confidence": 0.85,
        "source": "learned",
        "created_at": "2026-01-28T11:15:00Z"
      }
    ],
    "count": 2
  },
  "id": 3
}
```

**Example Request (list corrections):**
```json
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "floop_list",
    "arguments": {
      "corrections": true
    }
  },
  "id": 4
}
```

## Configuration Examples

### Continue.dev

Add floop to your Continue config at `.continue/config.json`:

```json
{
  "mcpServers": {
    "floop": {
      "command": "floop",
      "args": ["mcp-server"],
      "cwd": "${workspaceFolder}",
      "env": {}
    }
  }
}
```

**Usage in Continue:**
1. Open Agent mode (Cmd/Ctrl + Shift + P → "Continue: Open Agent")
2. The agent can now invoke `floop_active`, `floop_learn`, and `floop_list`
3. Example: "What behaviors are active for this file?" → Agent calls `floop_active`

---

### Cursor

Add floop to `.cursor/mcp.json` in your project:

```json
{
  "mcpServers": {
    "floop": {
      "command": "floop",
      "args": ["mcp-server"],
      "cwd": "${workspaceFolder}"
    }
  }
}
```

**Usage in Cursor:**
1. Open Composer or Agent mode
2. Reference floop tools: "@floop_active" or ask "What behaviors should I follow?"
3. Cursor automatically invokes MCP tools when relevant

---

### Cline

Add floop to Cline's MCP settings (accessible via Cline panel → Settings → MCP Servers):

```json
{
  "mcpServers": {
    "floop": {
      "command": "floop",
      "args": ["mcp-server"],
      "cwd": "${workspaceFolder}"
    }
  }
}
```

**Usage in Cline:**
1. Open Cline panel
2. Start a task: "Implement user authentication"
3. Cline automatically queries `floop_active` for relevant behaviors
4. When you correct Cline, it can call `floop_learn` to capture the correction

---

### Windsurf / Cascade

In Windsurf settings, add to MCP server configuration:

```json
{
  "mcp": {
    "servers": {
      "floop": {
        "command": "floop",
        "args": ["mcp-server"],
        "cwd": "${workspaceFolder}"
      }
    }
  }
}
```

---

### GitHub Copilot (VSCode Extension - Agent Mode)

Add floop to VSCode settings for Copilot's MCP support:

In `.vscode/settings.json`:
```json
{
  "github.copilot.chat.mcpServers": {
    "floop": {
      "command": "floop",
      "args": ["mcp-server"],
      "cwd": "${workspaceFolder}"
    }
  }
}
```

**Note:** MCP support in GitHub Copilot is in preview. Check [GitHub Copilot documentation](https://docs.github.com/copilot) for the latest status.

---

## Architecture

### How It Works

```
┌─────────────────┐
│   AI Tool       │
│ (Continue/      │
│  Cursor/Cline)  │
└────────┬────────┘
         │ JSON-RPC over stdio
         ↓
┌─────────────────┐
│  floop          │
│  mcp-server     │
└────────┬────────┘
         │
         ├─→ Read: GraphStore (.floop/ directory)
         │        Query behaviors, evaluate context
         │
         └─→ Write: LearningLoop
                  Process corrections, extract behaviors
```

### Protocol Details

- **Transport**: stdio (stdin/stdout)
- **Protocol**: JSON-RPC 2.0
- **Specification**: [Model Context Protocol](https://modelcontextprotocol.io/)

### MCP Methods Implemented

- `initialize` - Protocol handshake, version negotiation
- `tools/list` - Returns available tools (floop_active, floop_learn, floop_list)
- `tools/call` - Executes a tool with parameters

### Data Flow

1. **AI Tool** sends JSON-RPC request to stdin
2. **MCP Server** parses request, routes to appropriate handler
3. **Handler** calls internal floop packages:
   - `floop_active` → `internal/activation` package
   - `floop_learn` → `internal/learning` package
   - `floop_list` → `internal/store` package
4. **MCP Server** formats response as JSON-RPC and writes to stdout
5. **AI Tool** receives response and uses it in agent execution

## Troubleshooting

### Server Won't Start

**Problem**: `floop mcp-server` exits immediately or shows errors

**Solutions**:
- Ensure floop is initialized: `floop init` (creates `.floop/` directory)
- Check `--root` flag points to correct project directory
- Verify floop binary is in PATH: `which floop`

---

### Tool Not Recognized by AI Tool

**Problem**: AI tool doesn't see floop tools

**Solutions**:
- Restart your AI tool after adding MCP config
- Check MCP config file syntax (valid JSON)
- Verify `command` path is correct (use absolute path if needed)
- Check AI tool's MCP server logs for connection errors

---

### Behaviors Not Activating

**Problem**: `floop_active` returns empty list

**Solutions**:
- Verify behaviors exist: `floop list`
- Check behavior activation conditions (when predicates)
- Ensure file path and task context match behavior conditions
- Try: `floop active --file path/to/file` to test manually

---

### Corrections Not Saving

**Problem**: `floop_learn` succeeds but behaviors don't persist

**Solutions**:
- Check `.floop/` directory is writable
- Verify corrections saved: `floop list --corrections`
- Review learning result: look for `auto_accepted` or `requires_review` status

---

### Performance Issues

**Problem**: MCP server slow to respond

**Solutions**:
- Large `.floop/` directory: Consider using `floop forget` to remove old behaviors
- Network latency: Ensure `--root` points to local filesystem, not network mount
- Check for disk I/O issues

---

## Best Practices

### 1. Use Context-Rich Corrections

When using `floop_learn`, provide detailed context:

```json
{
  "wrong": "Used var for constant value",
  "right": "Use const for values that never change",
  "file": "internal/store/constants.go"
}
```

Better than:
```json
{
  "wrong": "Used var",
  "right": "Use const"
}
```

---

### 2. Use Global + Local Storage

- **Global** (`~/.floop/`): Personal conventions, language preferences
- **Local** (`./.floop/`): Project-specific patterns, team conventions

```bash
# Initialize both
floop init          # Local
floop init --global # Global
```

The MCP server queries both stores and merges results (local takes precedence).

---

### 3. Keep Behaviors Focused

Use `floop merge` to combine similar behaviors instead of accumulating many small ones.

---

## Advanced Usage

### Custom Project Root

If your AI tool runs in a different directory than your project:

```json
{
  "mcpServers": {
    "floop": {
      "command": "floop",
      "args": ["mcp-server", "--root", "/absolute/path/to/project"]
    }
  }
}
```

---

### Multiple Projects

Run separate MCP servers for different projects by configuring multiple MCP server entries:

```json
{
  "mcpServers": {
    "floop-project-a": {
      "command": "floop",
      "args": ["mcp-server", "--root", "/path/to/project-a"]
    },
    "floop-project-b": {
      "command": "floop",
      "args": ["mcp-server", "--root", "/path/to/project-b"]
    }
  }
}
```

---

### Debugging MCP Communication

To see JSON-RPC messages:

```bash
# Manual test
echo '{"jsonrpc":"2.0","method":"tools/list","id":1}' | floop mcp-server

# Expected output
{"jsonrpc":"2.0","result":{"tools":[...]},"id":1}
```

Or use the `--json` flag for structured output:
```bash
floop active --json --file cmd/floop/main.go
```

---

## FAQ

### Q: Can multiple AI tools connect to the same MCP server?

A: No, each AI tool should spawn its own `floop mcp-server` instance. The server is designed for single-client stdio communication.

---

### Q: Does the MCP server modify my code?

A: No. The MCP server only reads behaviors and writes corrections to `.floop/`. It never modifies your source code.

---

### Q: What happens if I correct the AI agent during development?

A: If the AI tool supports it, it can call `floop_learn` automatically. Otherwise, you can manually run:
```bash
floop learn --wrong "what agent did" --right "what it should do" --file path/to/file
```

---

### Q: Can I use this with non-AI tools?

A: Yes! Any MCP-compatible tool can connect to `floop mcp-server`. The protocol is language-agnostic.

---

### Q: How do I disable the MCP server?

A: Simply remove the floop entry from your AI tool's MCP configuration and restart the tool.

---

## Next Steps

- **Explore Behaviors**: `floop list` to see what's been learned
- **Manually Learn**: `floop learn --wrong "wrong" --right "right"` to add corrections
- **Curate**: Use `floop forget`, `floop merge`, `floop deprecate` to manage behaviors
- **Prompt Generation**: `floop prompt` to generate AI-readable behavior summaries

For more details, see the [floop documentation](../README.md).

---

## Resources

- [Model Context Protocol Specification](https://modelcontextprotocol.io/)
- [MCP Servers Repository](https://github.com/modelcontextprotocol/servers)
- [Continue.dev MCP Docs](https://docs.continue.dev/features/model-context-protocol)
- [Cursor MCP Support](https://docs.cursor.com/context/model-context-protocol)

---

**Generated by floop** - The feedback loop for AI coding agents
