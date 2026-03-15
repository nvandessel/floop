# Cross-Platform CGO Release Pipeline Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship CGO-enabled floop binaries with LanceDB statically linked for all supported platforms on every push to main.

**Architecture:** Replace goreleaser's build step with a per-platform GitHub Actions matrix (5 native CGO builds + 1 fallback), then use goreleaser in pre-built binary mode for packaging, checksums, changelog, homebrew cask, and GitHub release creation. PRs #203 and #204 land first to establish the cross-platform CI foundation.

**Tech Stack:** Go 1.24+, GoReleaser v2.14.1, GitHub Actions (ubuntu-latest, ubuntu-24.04-arm, macos-latest, macos-13, windows-latest), LanceDB Go SDK v0.1.2

**Spec:** `docs/superpowers/specs/2026-03-14-cgo-release-pipeline-design.md`

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `.github/workflows/ci.yml` | Modify | PR #203/#204: cross-platform test/build matrix |
| `internal/llm/subagent.go` | Modify | PR #203: symlink eval fix |
| `.goreleaser.yml` | Rewrite | Switch from `builder: go` to `builder: prebuilt` |
| `.github/workflows/auto-release.yml` | Rewrite | Add build-matrix job, restructure release job |
| `.github/workflows/test-release.yml` | Rewrite | Validate prebuilt goreleaser config in snapshot mode |
| `docs/EMBEDDINGS.md` | Modify | Add build-from-source guide |
| `Makefile` | Modify | Add `build-cgo` target for local CGO builds |

---

## Chunk 1: Land PRs #203 and #204

### Task 1: Rebase PR #203 on main and resolve conflicts

PR #203 was created before PR #208 (LanceDB) merged. Main now has `CGO_ENABLED: "0"` on test/build/lint/security jobs, plus `test-cgo` and `test-cgo-macos` jobs. PR #203 converts test/build to a matrix — the rebase must preserve `CGO_ENABLED: "0"` in the matrixed jobs.

**Files:**
- Modify: `.github/workflows/ci.yml` (conflict resolution on rebase)

- [ ] **Step 1: Fetch and checkout PR #203's branch**

```bash
gh pr checkout 203
```

- [ ] **Step 2: Rebase on main**

```bash
git rebase main
```

Expected: conflicts in `.github/workflows/ci.yml`. The test and build jobs have diverged — main has `CGO_ENABLED: "0"` env vars, PR #203 converts to matrix strategy.

- [ ] **Step 3: Resolve conflicts**

The merged result for the `test` job should be:

```yaml
  test:
    name: Test
    needs: changes
    if: needs.changes.outputs.code == 'true'
    runs-on: ${{ matrix.os }}
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    continue-on-error: ${{ matrix.os == 'windows-latest' }}
    env:
      CGO_ENABLED: "0"
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v6
        with:
          go-version-file: "go.mod"
      - run: go test -race ./...
        shell: bash
```

The `build` job should be similarly matrixed with `CGO_ENABLED: "0"` preserved and the Windows cross-compile step removed (native matrix replaces it). Keep the `test-cgo` and `test-cgo-macos` jobs exactly as they are on main — these are separate from the matrix.

Lint and security jobs stay ubuntu-only with `CGO_ENABLED: "0"` (unchanged from main).

- [ ] **Step 4: Continue rebase and force push**

```bash
git rebase --continue
git push --force-with-lease
```

- [ ] **Step 5: Request Greptile review and wait for CI**

Comment `@greptile review` on PR #203. Wait for CI to pass (Windows may fail with `continue-on-error`).

- [ ] **Step 6: Merge PR #203**

```bash
gh pr merge 203 --squash --delete-branch
```

---

### Task 2: Rebase and merge PR #204

PR #204 targets PR #203's branch. After #203 merges to main, #204's base needs updating.

**Files:**
- Modify: `.github/workflows/ci.yml` (adds `continue-on-error` to build job)
- Modify: `.gitignore` (adds `__pycache__/`)

- [ ] **Step 1: Retarget PR #204 to main**

```bash
gh pr edit 204 --base main
```

- [ ] **Step 2: Checkout and rebase on main**

```bash
gh pr checkout 204
git rebase main
```

Resolve any conflicts — likely minimal since #203 already merged.

- [ ] **Step 3: Force push**

```bash
git push --force-with-lease
```

- [ ] **Step 4: Wait for CI, then merge**

```bash
gh pr merge 204 --squash --delete-branch
```

- [ ] **Step 5: Pull main**

```bash
git checkout main && git pull
```

---

## Chunk 2: Static Linking Spike

### Task 3: Validate static linking locally on linux/amd64

Before writing any CI changes, prove that LanceDB static linking produces a self-contained binary. This task runs entirely locally.

**Files:**
- None modified (local experiment only)

- [ ] **Step 1: Verify native libs exist**

```bash
ls -la lib/linux_amd64/liblancedb_go.a include/lancedb.h
```

If missing, download them:

```bash
go mod download
LANCE_VERSION=$(go list -m -f '{{.Version}}' github.com/lancedb/lancedb-go)
bash "$(go env GOMODCACHE)/github.com/lancedb/lancedb-go@${LANCE_VERSION}/scripts/download-artifacts.sh" "${LANCE_VERSION}"
```

- [ ] **Step 2: Attempt static build**

```bash
CGO_ENABLED=1 \
CGO_CFLAGS="-I$(pwd)/include" \
CGO_LDFLAGS="-L$(pwd)/lib/linux_amd64 -llancedb_go -lm -ldl -lstdc++" \
go build -ldflags "-s -w -X main.version=spike -X main.commit=$(git rev-parse HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
-o floop-cgo ./cmd/floop
```

Expected: binary compiles. If it fails with missing symbols, adjust `CGO_LDFLAGS` — the Rust static lib may need additional system deps (e.g., `-lpthread`, `-lgcc_s`).

- [ ] **Step 3: Verify binary is self-contained**

```bash
ldd floop-cgo
```

Expected: only system libraries (libc, libm, libdl, libpthread, ld-linux). No `liblancedb_go.so`. If the shared lib appears, add `-static` or `-extldflags '-static'` to ldflags (may require musl on Linux).

Note: Full static linking (no libc dep) isn't required — just no LanceDB runtime dep.

- [ ] **Step 4: Verify LanceDB is functional**

```bash
./floop-cgo --version
```

Then start the MCP server and check logs for "LanceDB" (not "brute-force fallback"). If the BruteForce fallback is used, the CGO stub wasn't compiled in — check that `CGO_ENABLED=1` was actually set.

- [ ] **Step 5: Record the exact linker flags that work**

Write down the exact `CGO_CFLAGS` and `CGO_LDFLAGS` that produced a working binary. These will be used in the CI matrix.

- [ ] **Step 6: Clean up**

```bash
rm floop-cgo
```

---

## Chunk 3: CGO Release Pipeline

### Task 4: Rewrite `.goreleaser.yml` for pre-built binary mode

**Files:**
- Modify: `.goreleaser.yml`

- [ ] **Step 1: Switch builds to prebuilt mode**

Replace the entire `builds` section:

```yaml
builds:
  - id: floop
    builder: prebuilt
    prebuilt:
      path: "dist/floop_{{ .Os }}_{{ .Arch }}{{ with .Amd64 }}_{{ . }}{{ end }}/floop{{ .Ext }}"
    binary: floop
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
```

Note: The `{{ with .Amd64 }}_{{ . }}{{ end }}` template handles goreleaser's `_v1` suffix for amd64 targets. The "Arrange binaries" step in `auto-release.yml` (Task 5) must create directories matching this pattern (e.g., `dist/floop_linux_amd64_v1/`).

- [ ] **Step 2: Remove before.hooks**

Delete the `before.hooks` section (lines 6-9). goreleaser no longer compiles, so `go mod tidy` is unnecessary and would fail if Go isn't installed on the release runner.

- [ ] **Step 3: Update release footer**

Replace the `footer` in the `release` section. Remove the `go install` instruction:

```yaml
  footer: |
    ## Installation

    ### Homebrew (macOS/Linux)
    ```
    brew install nvandessel/tap/floop
    ```

    ### Manual
    Download the appropriate archive for your platform below, extract it, and move the `floop` binary to your PATH.

    ### Building from source
    See [docs/EMBEDDINGS.md](https://github.com/nvandessel/floop/blob/main/docs/EMBEDDINGS.md#building-from-source) for instructions on building with LanceDB support.

    **Full changelog**: https://github.com/nvandessel/floop/compare/{{ .PreviousTag }}...{{ .Tag }}
```

- [ ] **Step 4: Verify YAML is valid**

```bash
python3 -c "import yaml; yaml.safe_load(open('.goreleaser.yml'))"
```

- [ ] **Step 5: Commit**

```bash
git add .goreleaser.yml
git commit -m "feat(release): switch goreleaser to pre-built binary mode"
```

---

### Task 5: Rewrite `auto-release.yml` with build matrix

**Files:**
- Modify: `.github/workflows/auto-release.yml`

- [ ] **Step 1: Write the new workflow**

Replace the entire file. Key changes:
- `check-skip` and `tag-version` are separate jobs (tag-version was previously inline in the release job)
- New `build` job: matrix of 6 entries, 5 CGO + 1 fallback
- New `release` job: downloads artifacts, arranges into dist/, runs goreleaser

```yaml
name: Auto Release

on:
  push:
    branches: [main]
    paths-ignore:
      - "**.md"
      - "docs/**"
      - ".beads/**"
      - ".floop/**"
      - ".github/**"
      - "LICENSE"

permissions:
  contents: read
  pull-requests: read

concurrency:
  group: release
  cancel-in-progress: false

jobs:
  check-skip:
    name: Check Skip Conditions
    runs-on: ubuntu-latest
    outputs:
      should_release: ${{ steps.check.outputs.should_release }}
    steps:
      - name: Check skip conditions
        id: check
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          COMMIT_MSG: ${{ github.event.head_commit.message }}
        run: |
          if [ "${{ github.actor }}" = "github-actions[bot]" ]; then
            echo "Skipping: pushed by github-actions[bot]"
            echo "should_release=false" >> "$GITHUB_OUTPUT"
            exit 0
          fi

          if echo "$COMMIT_MSG" | grep -qi '\[skip release\]'; then
            echo "Skipping: commit message contains [skip release]"
            echo "should_release=false" >> "$GITHUB_OUTPUT"
            exit 0
          fi

          PR_NUM=$(echo "$COMMIT_MSG" | grep -oP '\(#\K\d+' | head -1 || true)
          if [ -z "$PR_NUM" ]; then
            PR_NUM=$(gh api "repos/${{ github.repository }}/commits/${{ github.sha }}/pulls" \
              --jq '.[0].number // empty' 2>&1) || true
            if echo "$PR_NUM" | grep -qiE 'error|not found'; then
              PR_NUM=""
            fi
          fi

          if [ -n "$PR_NUM" ]; then
            LABELS=$(gh pr view "$PR_NUM" --json labels --jq '.labels[].name' 2>&1) || true
            if echo "$LABELS" | grep -q 'skip-release'; then
              echo "Skipping: PR #$PR_NUM has skip-release label"
              echo "should_release=false" >> "$GITHUB_OUTPUT"
              exit 0
            fi
          fi

          echo "should_release=true" >> "$GITHUB_OUTPUT"

  tag-version:
    name: Tag Version
    needs: check-skip
    if: needs.check-skip.outputs.should_release == 'true'
    runs-on: ubuntu-latest
    permissions:
      contents: write
    outputs:
      version: ${{ steps.tag.outputs.version }}
      should_release: ${{ steps.tag.outputs.should_release }}
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0
          token: ${{ secrets.GITHUB_TOKEN }}
      - uses: actions/setup-go@v6
        with:
          go-version-file: "go.mod"
      - name: Install svu
        run: go install github.com/caarlos0/svu/v3@latest
      - name: Tag next version
        id: tag
        run: |
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          CURRENT=$(svu current || echo "v0.0.0")
          NEXT=$(svu next)
          if [ "$CURRENT" = "$NEXT" ]; then
            echo "No version bump needed ($CURRENT)"
            echo "should_release=false" >> "$GITHUB_OUTPUT"
            exit 0
          fi
          echo "$CURRENT → $NEXT"
          git tag -a "$NEXT" -m "Release $NEXT"
          git push origin "$NEXT"
          echo "version=$NEXT" >> "$GITHUB_OUTPUT"
          echo "should_release=true" >> "$GITHUB_OUTPUT"

  build:
    name: Build (${{ matrix.goos }}/${{ matrix.goarch }})
    needs: tag-version
    if: needs.tag-version.outputs.should_release == 'true'
    strategy:
      fail-fast: false
      matrix:
        include:
          - goos: linux
            goarch: amd64
            runner: ubuntu-latest
            cgo: true
          - goos: linux
            goarch: arm64
            runner: ubuntu-24.04-arm
            cgo: true
          - goos: darwin
            goarch: arm64
            runner: macos-latest
            cgo: true
          - goos: darwin
            goarch: amd64
            runner: macos-13
            cgo: true
          - goos: windows
            goarch: amd64
            runner: windows-latest
            cgo: true
          - goos: windows
            goarch: arm64
            runner: windows-latest
            cgo: false
    runs-on: ${{ matrix.runner }}
    env:
      GOOS: ${{ matrix.goos }}
      GOARCH: ${{ matrix.goarch }}
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v6
        with:
          go-version-file: "go.mod"

      - name: Download Go modules
        run: go mod download

      - name: Download LanceDB native libraries
        if: matrix.cgo
        shell: bash
        run: |
          LANCE_VERSION=$(go list -m -f '{{.Version}}' github.com/lancedb/lancedb-go)
          bash "$(go env GOMODCACHE)/github.com/lancedb/lancedb-go@${LANCE_VERSION}/scripts/download-artifacts.sh" "${LANCE_VERSION}"

      - name: Build with CGO (LanceDB)
        if: matrix.cgo
        id: cgo_build
        shell: bash
        continue-on-error: true
        run: |
          EXT=""
          if [ "${{ matrix.goos }}" = "windows" ]; then EXT=".exe"; fi

          PLATFORM="${{ matrix.goos }}_${{ matrix.goarch }}"
          VERSION="${{ needs.tag-version.outputs.version }}"

          CGO_ENABLED=1 \
          CGO_CFLAGS="-I$(pwd)/include" \
          CGO_LDFLAGS="-L$(pwd)/lib/${PLATFORM} -llancedb_go" \
          go build \
            -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${{ github.sha }} -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
            -o "floop${EXT}" \
            ./cmd/floop

          echo "cgo_success=true" >> "$GITHUB_OUTPUT"

      - name: Build without CGO (fallback)
        if: matrix.cgo == false || steps.cgo_build.outcome == 'failure'
        shell: bash
        run: |
          EXT=""
          if [ "${{ matrix.goos }}" = "windows" ]; then EXT=".exe"; fi

          VERSION="${{ needs.tag-version.outputs.version }}"

          CGO_ENABLED=0 \
          go build \
            -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${{ github.sha }} -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
            -o "floop${EXT}" \
            ./cmd/floop

      - name: Upload binary
        uses: actions/upload-artifact@v4
        with:
          name: floop-${{ matrix.goos }}-${{ matrix.goarch }}
          path: floop${{ matrix.goos == 'windows' && '.exe' || '' }}
          retention-days: 1

  release:
    name: Release
    needs: [tag-version, build]
    if: needs.tag-version.outputs.should_release == 'true'
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0

      - name: Download all artifacts
        uses: actions/download-artifact@v4
        with:
          path: artifacts/

      - name: Arrange binaries for goreleaser
        shell: bash
        run: |
          mkdir -p dist
          for dir in artifacts/floop-*; do
            # Extract os-arch from artifact name (e.g., floop-linux-amd64)
            name=$(basename "$dir")
            os_arch="${name#floop-}"
            os="${os_arch%-*}"
            arch="${os_arch##*-}"

            # goreleaser adds _v1 suffix for amd64 targets
            suffix=""
            if [ "$arch" = "amd64" ]; then suffix="_v1"; fi

            target="dist/floop_${os}_${arch}${suffix}"
            mkdir -p "$target"

            # Move binary (handles .exe for windows)
            if [ -f "$dir/floop.exe" ]; then
              mv "$dir/floop.exe" "$target/floop.exe"
            else
              mv "$dir/floop" "$target/floop"
            fi

            echo "✅ ${os}/${arch} → ${target}"
          done

          echo "Final layout:"
          find dist -type f

      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v7
        with:
          distribution: goreleaser
          version: v2.14.1
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_GITHUB_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}
          GORELEASER_CURRENT_TAG: ${{ needs.tag-version.outputs.version }}
```

Note: The exact `CGO_LDFLAGS` in the "Build with CGO" step may need additional flags discovered during the spike (Task 3). The plan uses the minimal `-L -l` flags — if the spike reveals additional system deps, add them here.

- [ ] **Step 2: Verify YAML is valid**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/auto-release.yml'))"
```

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/auto-release.yml
git commit -m "feat(release): add build matrix with CGO for all platforms"
```

---

### Task 6: Update `test-release.yml`

Validates the goreleaser prebuilt config on PRs that touch release files. Builds linux/amd64 with CGO as a smoke test, remaining targets with CGO_ENABLED=0.

**Files:**
- Modify: `.github/workflows/test-release.yml`

- [ ] **Step 1: Rewrite the workflow**

```yaml
name: Test Release

on:
  pull_request:
    paths:
      - '.goreleaser.yml'
      - '.github/workflows/auto-release.yml'
      - '.github/workflows/test-release.yml'
      - 'Makefile'
      - 'cmd/floop/main.go'
      - 'cmd/floop/cmd_version.go'

permissions:
  contents: read

env:
  GORELEASER_VERSION: v2.14.1

jobs:
  build-binaries:
    name: Build Test Binaries
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v6
        with:
          go-version-file: "go.mod"

      - name: Download Go modules
        run: go mod download

      - name: Download LanceDB native libraries
        run: |
          LANCE_VERSION=$(go list -m -f '{{.Version}}' github.com/lancedb/lancedb-go)
          bash "$(go env GOMODCACHE)/github.com/lancedb/lancedb-go@${LANCE_VERSION}/scripts/download-artifacts.sh" "${LANCE_VERSION}"

      - name: Build linux/amd64 with CGO
        run: |
          mkdir -p dist/floop_linux_amd64_v1
          CGO_ENABLED=1 \
          CGO_CFLAGS="-I$(pwd)/include" \
          CGO_LDFLAGS="-L$(pwd)/lib/linux_amd64 -llancedb_go" \
          go build -ldflags "-s -w -X main.version=snapshot -X main.commit=${{ github.sha }} -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
            -o dist/floop_linux_amd64_v1/floop ./cmd/floop

      - name: Build remaining targets without CGO
        run: |
          for target in linux_arm64 darwin_amd64 darwin_arm64 windows_amd64 windows_arm64; do
            os="${target%_*}"
            arch="${target#*_}"
            ext=""
            suffix=""
            if [ "$os" = "windows" ]; then ext=".exe"; fi
            if [ "$arch" = "amd64" ]; then suffix="_v1"; fi

            mkdir -p "dist/floop_${target}${suffix}"
            CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
            go build -ldflags "-s -w -X main.version=snapshot -X main.commit=${{ github.sha }} -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
              -o "dist/floop_${target}${suffix}/floop${ext}" ./cmd/floop
          done

      - name: Test linux/amd64 binary
        run: ./dist/floop_linux_amd64_v1/floop --version

      - name: Run GoReleaser in snapshot mode
        uses: goreleaser/goreleaser-action@v7
        with:
          distribution: goreleaser
          version: ${{ env.GORELEASER_VERSION }}
          args: release --snapshot --clean --skip=publish
        env:
          GORELEASER_CURRENT_TAG: v0.0.0-snapshot

      - name: Verify archives created
        run: |
          echo "Archives:"
          ls -lh dist/*.tar.gz dist/*.zip 2>/dev/null || true
          echo "Checksums:"
          cat dist/checksums.txt 2>/dev/null || echo "No checksums (expected in snapshot)"
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/test-release.yml
git commit -m "feat(release): update test-release for prebuilt goreleaser validation"
```

---

## Chunk 4: Documentation and Cleanup

### Task 7: Add build-from-source docs and Makefile target

**Files:**
- Modify: `docs/EMBEDDINGS.md`
- Modify: `Makefile`

- [ ] **Step 1: Add build-from-source section to EMBEDDINGS.md**

Add before the "## Troubleshooting" section (after line 97):

```markdown
## Building from Source

Pre-built release binaries include LanceDB support. If building from source (e.g., via `go install`), the binary uses BruteForce vector search instead — functionally equivalent but without persistence across restarts.

To build from source with LanceDB:

### Prerequisites

- Go 1.24+
- C compiler (gcc, clang, or MSVC)
- LanceDB native libraries

### Steps

```bash
# Download LanceDB native libraries for your platform
go mod download
LANCE_VERSION=$(go list -m -f '{{.Version}}' github.com/lancedb/lancedb-go)
bash "$(go env GOMODCACHE)/github.com/lancedb/lancedb-go@${LANCE_VERSION}/scripts/download-artifacts.sh" "${LANCE_VERSION}"

# Build with CGO
make build-cgo

# Or manually:
CGO_ENABLED=1 \
CGO_CFLAGS="-I$(pwd)/include" \
CGO_LDFLAGS="-L$(pwd)/lib/$(go env GOOS)_$(go env GOARCH) -llancedb_go" \
go build -o floop ./cmd/floop
```

### Verify LanceDB is linked

```bash
# Linux
ldd ./floop | grep -v lancedb  # Should show only system libs

# macOS
otool -L ./floop | grep -v lancedb
```

If LanceDB is statically linked, it won't appear in the output — that's correct. The binary is self-contained.
```

- [ ] **Step 2: Add build-cgo Makefile target**

Add to `Makefile` after the existing `build` target:

```makefile
build-cgo:
	CGO_ENABLED=1 \
	CGO_CFLAGS="-I$(CURDIR)/include" \
	CGO_LDFLAGS="-L$(CURDIR)/lib/$$(go env GOOS)_$$(go env GOARCH) -llancedb_go" \
	go build -ldflags="$(LDFLAGS)" -o ./floop ./cmd/floop
```

Also add `build-cgo` to the `.PHONY` declaration on line 1 of the Makefile.

- [ ] **Step 3: Commit**

```bash
git add docs/EMBEDDINGS.md Makefile
git commit -m "docs: add build-from-source guide and build-cgo Makefile target"
```

---

### Task 8: Create PR and validate

- [ ] **Step 1: Push branch**

```bash
git push -u origin feat/cgo-release-pipeline
```

- [ ] **Step 2: Create PR**

```bash
gh pr create --title "feat: CGO release pipeline with LanceDB for all platforms" --body "$(cat <<'EOF'
## Summary
- Switch goreleaser to pre-built binary mode
- Add build matrix in auto-release.yml: 5 native CGO builds (linux/darwin/windows x amd64/arm64) + 1 CGO_ENABLED=0 fallback (windows/arm64)
- Each platform downloads LanceDB native libs and statically links them
- CGO build failure on any platform falls back to CGO_ENABLED=0 automatically
- Update test-release.yml for prebuilt goreleaser validation
- Add build-from-source documentation and Makefile target
- Remove `go install` from release notes (replaced with build-from-source guide)

## Depends on
- PR #203 (cross-platform CI matrix) — must merge first
- PR #204 (Windows test fixes) — must merge first

## Test plan
- [ ] test-release.yml validates goreleaser prebuilt config in snapshot mode
- [ ] Local CGO build produces working binary with LanceDB
- [ ] First real release after merge produces 6 platform archives
- [ ] `brew install nvandessel/tap/floop` still works on macOS
- [ ] BruteForce fallback works when CGO build fails

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 3: Wait for test-release.yml to validate**

The PR touches `.goreleaser.yml` and `auto-release.yml`, so `test-release.yml` will run automatically. Verify it passes.

- [ ] **Step 4: Request Greptile review**

Comment `@greptile review` on the PR. Iterate on feedback.

---

## Execution Order

```
1. Task 1: Rebase + merge PR #203          ← prerequisite
2. Task 2: Retarget + merge PR #204        ← prerequisite
3. Task 3: Static linking spike (local)    ← validates approach
4. Task 4: Rewrite .goreleaser.yml         ← uses spike results
5. Task 5: Rewrite auto-release.yml        ← uses spike results
6. Task 6: Update test-release.yml
7. Task 7: Docs + Makefile
8. Task 8: PR + validate
```

Tasks 4-7 are on a single feature branch (`feat/cgo-release-pipeline`) and can be committed sequentially. Task 3 (spike) should be done before Tasks 4-5 since the exact `CGO_LDFLAGS` may need adjustment based on findings.

## Known Unknowns

1. **Exact `CGO_LDFLAGS` per platform** — The spike (Task 3) determines these. The plan uses minimal flags; additional system deps (`-lm`, `-ldl`, `-lstdc++`, `-framework Security`, etc.) may be needed.
2. **`ubuntu-24.04-arm` runner availability** — Confirm this runner is available for public repos. If not, linux/arm64 falls back to `CGO_ENABLED=0`.
3. **goreleaser `prebuilt` path template `_v1` suffix** — Handled via `{{ with .Amd64 }}_{{ . }}{{ end }}` in the template and `_v1` suffix in the arrange step. Verify with goreleaser snapshot during test-release validation.
4. **macOS Gatekeeper** — Monitor the first macOS release for quarantine issues beyond what the existing xattr hook handles.
5. **Windows CGO static linking** — The spike validates linux/amd64 only. Windows may need different linker flags or may not have `.a` files from `download-artifacts.sh`. The `continue-on-error: true` on the CGO build step provides a safety net, falling back to `CGO_ENABLED=0`.
