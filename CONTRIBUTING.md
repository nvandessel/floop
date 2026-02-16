# Contributing to floop

Thank you for your interest in contributing to floop! This guide will help you get started.

## Prerequisites

- **Go 1.25+** ([install](https://go.dev/dl/))
- **make**
- **golangci-lint** ([install](https://golangci-lint.run/welcome/install/))

## Development Setup

```bash
# Clone the repository
git clone https://github.com/nvandessel/feedback-loop.git
cd feedback-loop

# Build
make build

# Run tests
make test

# Run the full CI suite (format check + lint + vet + test + build)
make ci
```

## Workflow

1. **Find or create an issue** — Check existing issues or open a new one describing the change
2. **Fork and branch** — Create a feature branch from `main` (`feat/description` or `fix/description`)
3. **Write code** — Follow the [Go coding standards](docs/GO_GUIDELINES.md)
4. **Write tests** — All changes need tests (see Testing below)
5. **Update docs** — If adding or changing CLI commands or flags, update `docs/CLI_REFERENCE.md`
6. **Run CI locally** — `make ci` must pass
7. **Submit a PR** — Reference the related issue

## Code Standards

See [docs/GO_GUIDELINES.md](docs/GO_GUIDELINES.md) for the full guide. Key points:

- Run `go fmt ./...` before committing
- Use `fmt.Errorf("context: %w", err)` for error wrapping
- Keep interfaces small (1-3 methods)
- Pass `context.Context` as the first parameter

## Testing

- **Table-driven tests** with `t.Run()` for all functions with multiple input cases
- Test both success and error paths
- Use `go test -race ./...` to catch race conditions

```bash
make test              # Run all tests
make test-coverage     # Generate coverage report
```

## Commit Messages

Use [conventional commits](https://www.conventionalcommits.org/):

- `feat:` new features
- `fix:` bug fixes
- `docs:` documentation changes
- `test:` test additions or changes
- `chore:` maintenance

## Release Process

Releases are automated using GoReleaser and GitHub Actions. Only maintainers can trigger releases.

**To create a new release:**

1. Ensure `main` branch is clean and all tests pass
2. Trigger the version bump workflow:
   ```bash
   gh workflow run version-bump.yml -f bump=<patch|minor|major>
   ```
3. Monitor the release workflow: `gh run watch`
4. Verify the release on GitHub: `gh release view <version>`
5. Confirm `CHANGELOG.md` was auto-updated by the workflow commit for that release

**Version semantics:**
- `patch` (0.0.X) — Bug fixes, minor improvements
- `minor` (0.X.0) — New features, backwards-compatible changes
- `major` (X.0.0) — Breaking changes, major architectural shifts

For detailed instructions, see [docs/RELEASE_PROCESS.md](docs/RELEASE_PROCESS.md).

## Pull Request Expectations

- PRs should be focused — one logical change per PR
- Include a description of what changed and why
- Reference the related issue (`Fixes #123` or `Closes #123`)
- All CI checks must pass
- Maintain or improve test coverage

## Reporting Issues

- **Bugs**: Use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.yml)
- **Features**: Use the [feature request template](.github/ISSUE_TEMPLATE/feature_request.yml)
- **Security**: See [SECURITY.md](SECURITY.md)

## License

By contributing, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).
