# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |

## Reporting a Vulnerability

Please report security vulnerabilities through GitHub's **private vulnerability reporting**.

1. Go to the [Security tab](https://github.com/nvandessel/feedback-loop/security) of this repository
2. Click **"Report a vulnerability"**
3. Fill in the details

**Please do not open public issues for security vulnerabilities.**

## Response Timeline

- **Acknowledgment**: Within 48 hours
- **Initial assessment**: Within 1 week
- **Fix or mitigation**: Dependent on severity, targeting 30 days for critical issues

## Security Features

floop includes several security measures:

- **Input sanitization** — All user inputs are validated and sanitized before processing
- **Path validation** — File operations are restricted to expected directories with traversal prevention
- **Rate limiting** — Protection against resource exhaustion
- **Audit logging** — Operations are logged to `.floop/audit.jsonl` for traceability
- **Dependency scanning** — CI runs `govulncheck` on every build

## Scope

This policy covers the floop CLI tool and its MCP server component. Third-party integrations (Claude Code, Cursor, etc.) are governed by their own security policies.
