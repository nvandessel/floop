# Golang Best Practices

This guide outlines the best practices for Go development within this project, modeled after high-quality TUI applications.

## 1. Project Structure
- **cmd/**: Contains the main applications. Each subdirectory should be a main package (e.g., `cmd/g4d/main.go`).
- **internal/**: Private application and library code. This project uses `internal/` for all domain logic:
  - **internal/platform/**: OS/distro detection and package manager abstraction.
  - **internal/config/**: Configuration loading, validation, and schema.
  - **internal/stow/**: GNU Stow wrapper and symlink management.
  - **internal/deps/**: Dependency checking and installation logic.
  - **internal/doctor/**: Health checking and reporting.
  - **internal/ui/**: TUI components (Bubble Tea models, views, styles).
  - **internal/machine/**: Machine-specific configuration and templates.
  - **internal/state/**: Installation state tracking.
- **pkg/**: Public library code (currently unused/reserved).
- **test/**: End-to-end and integration tests.

## 2. Code Style
- **Formatting**: Always use `gofmt` (or `goimports`).
- **Naming**:
  - Use `CamelCase` for exported identifiers.
  - Use `camelCase` for unexported identifiers.
  - Keep variable names short but descriptive (e.g., `i` for index, `ctx` for context).
  - Package names should be short, lowercase, and singular (e.g., `platform`, `ui`, `config`).
- **Error Handling**:
  - Return errors as the last return value.
  - Check errors immediately.
  - Use `fmt.Errorf` with `%w` to wrap errors for context.
  - Don't panic unless it's a truly unrecoverable initialization error.

## 3. TUI Development (Charmbracelet Stack)
- **Architecture**: Follow The Elm Architecture (Model, View, Update) via `bubbletea`.
- **Components**: Break down complex UIs into smaller, reusable `tea.Model` components (e.g., `ConfigList`, `Spinner`, `Progress`).
- **Styling**: Use `lipgloss` for all styling. Define a central `styles.go` in `internal/ui` to maintain consistency.
- **State**: Keep the main model clean. Delegate update logic to sub-models.

## 4. Configuration & Data
- **Config**: Use struct-based configuration (`internal/config`). Load from YAML files.
- **Data Access**: Separate data loading (Loader/Repository pattern) from the UI logic. The UI should receive data, not fetch it directly if possible.

## 5. Testing
- Write unit tests for logic-heavy packages.
- Use table-driven tests for parser/validator logic.
- Run tests with `go test ./...` or `make test`.

## 6. Dependencies
- Use `go mod` for dependency management.
- Specific versions should be pinned in `go.mod`.

## 7. Documentation
- Add comments to exported functions and types (`// TypeName represents...`).
- Maintain a `README.md` with installation and usage instructions.

## 8. Concurrency
- Use channels for communication between goroutines.
- Use `sync.Mutex` for protecting shared state if not using channels.
- Avoid global state where possible.
