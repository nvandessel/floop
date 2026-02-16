# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **Release reliability** — Pin GoReleaser to `v2.8.0` in release workflows and keep `test-release` path triggers aligned with active workflow files
- **Release docs** — Clarify that GitHub release notes are auto-generated from commits; `CHANGELOG.md` is optional and manually curated

## [0.2.0] - 2026-02-12

### Added

- **Graph visualization** — Interactive force-directed HTML graph rendering
- **Local model foundations** — Embedded/local LLM foundations with yzma-ready configuration
- **Token budget controls** — Budget enforcement wired into activation output
- **Decision logging** — Structured decision traces across learning, dedup, and LLM subagent paths
- **Tag-aware learning** — Internal tagging package, extraction pipeline wiring, and backfill support
- **Graph intelligence** — Feature-affinity virtual edges and activation tracking enhancements
- **Release automation** — GoReleaser-based cross-platform release pipeline

### Fixed

- **Release execution** — Consolidated version bump and publishing into one workflow to avoid token-triggered downstream workflow gaps
- **CLI/memory correctness** — Learn-path behavior alignment and multiple edge-case safety fixes

### Changed

- **Documentation** — Expanded CLI reference and refreshed usage/integration guides

## [0.1.0] - 2026-02-08

Initial public release.

### Added

- **Core learning system** — Capture corrections, extract reusable behaviors, place them in a graph
- **Spreading activation** — Brain-inspired memory retrieval using activation propagation, lateral inhibition, and hybrid scoring (based on SYNAPSE paper)
- **MCP server** — Model Context Protocol integration for AI tool interoperability (`floop_active`, `floop_learn`, `floop_list`, `floop_connect`, `floop_deduplicate`, `floop_validate`, `floop_backup`, `floop_restore`)
- **CLI** — Full command suite: `learn`, `active`, `list`, `show`, `why`, `graph`, `connect`, `stats`, `summarize`, `forget`, `deprecate`, `restore`, `merge`, `deduplicate`, `validate`, `backup`, `restore-backup`, `prompt`, `config`, `init`, `version`
- **Token budget optimization** — Behavior summarization, budget tracking, utilization reporting via `floop stats`
- **Curation commands** — `forget`, `deprecate`, `restore`, `merge` for managing behavior lifecycle
- **Graph management** — `connect` for creating edges, `deduplicate` for merging similar behaviors, `validate` for consistency checks
- **Backup/restore** — Full graph state export and import
- **Hook support** — `detect-correction` and `activate` commands for CI/editor hook integration
- **Security hardening** — Input sanitization, path traversal prevention, rate limiting, audit logging, YAML bomb protection, concurrent access safety
- **CI/CD** — GitHub Actions for test, lint, security scanning, and cross-platform release builds
- **Integration guides** — Documentation for Claude Code, MCP server, and 6 other AI tools
- **Self-dogfooding** — 38 behaviors learned from building floop with floop

[Unreleased]: https://github.com/nvandessel/feedback-loop/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/nvandessel/feedback-loop/releases/tag/v0.2.0
[0.1.0]: https://github.com/nvandessel/feedback-loop/releases/tag/v0.1.0
