# Token Budget System

The token budget system controls how many tokens of learned behavior content are injected into agent system prompts. It ensures floop stays within LLM context limits while preserving the most important behaviors.

## How Tiering Works

Every behavior is assigned one of four **injection tiers** based on its activation level:

| Tier | Activation Threshold | Content |
|------|---------------------|---------|
| **Full** | >= 0.7 | Complete canonical content |
| **Summary** | >= 0.3 | One-line summary (or truncated canonical) |
| **Name Only** | >= 0.1 | `` `name` [kind] #tags `` |
| **Omitted** | < 0.1 | Not included |

Constraints receive special protection: they are never demoted below **Summary** tier, regardless of activation level. This ensures safety-critical behaviors remain visible.

## Budget Demotion

When the total token cost of all tiered behaviors exceeds the budget:

1. Sort behaviors by activation (ascending)
2. Demote the lowest-activation behavior one tier (Full -> Summary -> Name Only -> Omitted)
3. Recalculate total tokens
4. Repeat until within budget

Constraints are skipped during demotion (they stay at their assigned tier or the constraint minimum tier, whichever is higher).

## Configuration

Configure token budgets in `~/.floop/config.yaml`:

```yaml
token_budget:
  # Budget for MCP resource handlers and CLI default (init, upgrade, prompt)
  default: 2000

  # Budget for hook-triggered activate calls (dynamic context injection)
  dynamic_context: 500
```

### Environment Variable Overrides

| Variable | Overrides | Example |
|----------|-----------|---------|
| `FLOOP_TOKEN_BUDGET` | `token_budget.default` | `FLOOP_TOKEN_BUDGET=3000` |
| `FLOOP_TOKEN_BUDGET_DYNAMIC` | `token_budget.dynamic_context` | `FLOOP_TOKEN_BUDGET_DYNAMIC=800` |

Environment variables take precedence over config file values.

## CLI Flags

The `--token-budget` flag is available on several commands and always overrides both config file and environment variable values:

| Command | Flag Default Source | Description |
|---------|-------------------|-------------|
| `floop init` | `token_budget.default` (2000) | Budget written into hook scripts |
| `floop upgrade` | `token_budget.default` (2000) | Budget for upgraded hook scripts |
| `floop activate` | `token_budget.dynamic_context` (500) | Per-injection budget |
| `floop prompt --tiered` | `--token-budget` flag (0 = unlimited) | Budget for tiered prompt compilation |

**Precedence:** CLI flag > environment variable > config file > built-in default

## Session Tracking

The session state system provides additional budget controls for hook-triggered injections:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `MaxTokenBudget` | 3000 | Total tokens for the entire session |
| `MaxPerInjection` | 500 | Maximum tokens per single injection |
| `BackoffMultiplier` | 5 | Exponential backoff between re-injections |

After each injection, the session tracks tokens consumed. Once the session-wide budget is exhausted, no more injections occur. The backoff multiplier spaces out re-injections: 1st is immediate, 2nd waits 5 prompts, 3rd waits 10, etc.

## Architecture

```
Context (file, task, env)
         |
         v
  Spreading Activation Engine
         |
         v
  Results: [{behaviorID, activation}, ...]
         |
         v
  ActivationTierMapper
    - Map activation -> tier (thresholds: 0.7/0.3/0.1)
    - Enforce constraint minimum tier
    - Budget demotion (lowest activation first)
         |
         v
  InjectionPlan
    - FullBehaviors, SummarizedBehaviors, NameOnlyBehaviors, OmittedBehaviors
    - TotalTokens, TokenBudget
         |
         v
  Assembly Compiler
    - Compile tiered prompt text
    - Sections: Constraints, Directives, Procedures (full + summary + name-only)
         |
         v
  Prompt text injected into agent system prompt
```

## Token Estimation

Token counts are estimated using the heuristic **1 token ~ 4 characters** (`(len(text) + 3) / 4`). This is a rough approximation for English text. The canonical implementation lives in `internal/tokens/estimate.go`.

## Key Files

| File | Role |
|------|------|
| `internal/tokens/estimate.go` | Centralized token estimation |
| `internal/config/config.go` | `TokenBudgetConfig` (default + dynamic_context) |
| `internal/tiering/activation_tiers.go` | `ActivationTierMapper` (canonical tiering) |
| `internal/tiering/bridge.go` | Convert scored behaviors to activation results |
| `internal/assembly/compile.go` | Tiered prompt compilation |
| `internal/session/state.go` | Session-wide budget tracking and backoff |
| `internal/mcp/handlers.go` | MCP resource handler (uses config budget) |
| `cmd/floop/cmd_activate.go` | CLI activate (uses dynamic_context budget) |
