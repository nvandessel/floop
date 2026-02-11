.PHONY: build test test-coverage lint lint-fix fmt fmt-check vet vuln ci clean docs-validate

VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build:
	go build -ldflags="$(LDFLAGS)" -o ./floop ./cmd/floop

test:
	go test -race ./...

test-coverage:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

lint:
	golangci-lint run --timeout=5m

lint-fix:
	golangci-lint run --fix --timeout=5m

fmt:
	go fmt ./...

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "Files need formatting:" && gofmt -l . && exit 1)

vet:
	go vet ./...

vuln:
	@which govulncheck > /dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

ci: fmt-check lint vet test build

clean:
	rm -f ./floop coverage.out coverage.html

docs-validate: build
	@echo "Validating CLI reference documentation..."
	@missing=""; \
	for cmd in $$(./floop --help 2>&1 | awk '/Available Commands:/{found=1; next} found && /^  [a-z]/{print $$1} found && /^$$/{exit}'); do \
		if ! grep -q "## $$cmd" docs/CLI_REFERENCE.md 2>/dev/null; then \
			missing="$$missing $$cmd"; \
		fi; \
	done; \
	if [ -n "$$missing" ]; then \
		echo "ERROR: Commands missing from docs/CLI_REFERENCE.md:$$missing"; \
		exit 1; \
	fi; \
	echo "All commands documented."
