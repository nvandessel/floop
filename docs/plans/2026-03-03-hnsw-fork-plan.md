# HNSW Fork Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

> **NOTE (2026-03-07):** The original plan described using `natefinch/atomic`
> with `io.Pipe` + `bufio`. During implementation, a simpler pure-stdlib approach
> was chosen instead: `os.CreateTemp` + `bufio.NewWriter` + `os.Rename`. This
> avoids adding any new dependencies. The plan text below has been updated to
> reflect the actual implementation. See the design doc for the rationale.

**Goal:** Replace `google/renameio` with pure stdlib atomic writes (`os.CreateTemp` + `os.Rename`) in a fork of `coder/hnsw`, enabling native Windows HNSW support and removing the brute-force fallback.

**Architecture:** Fork `coder/hnsw` to `nvandessel/hnsw`. The only code change in the fork is the `Save()` method in `encode.go` — replacing `renameio` with `os.CreateTemp` + `bufio.NewWriter` + `os.Rename` for cross-platform atomic writes. Zero new dependencies. In floop, wire in via `go.mod` replace directive and delete the Windows build-tag workaround.

**Tech Stack:** Go stdlib (`os`, `bufio`)

**Design doc:** `docs/plans/2026-03-03-hnsw-fork-design.md`

---

### Task 1: Create the fork on GitHub

**Step 1: Fork coder/hnsw**

Go to https://github.com/coder/hnsw and fork it to `nvandessel/hnsw` via the GitHub UI or CLI:

```bash
gh repo fork coder/hnsw --clone=false --org="" --fork-name=hnsw
```

**Step 2: Clone the fork locally**

```bash
cd /tmp
git clone git@github.com:nvandessel/hnsw.git
cd hnsw
```

**Step 3: Create a feature branch**

```bash
git checkout -b fix/windows-atomic-save
```

**Step 4: Commit (nothing yet, just verify)**

```bash
git status
```

Expected: clean working tree on `fix/windows-atomic-save` branch.

---

### Task 2: Replace renameio with stdlib atomic saves in the fork

**Files:**
- Modify: `encode.go` (imports and Save method)
- Modify: `go.mod`

**Step 1: Remove renameio dependency**

```bash
go get -d github.com/google/renameio@none || true
```

**Step 2: Replace the Save() method in encode.go**

Replace the import block — remove `github.com/google/renameio`, add `"path/filepath"` to stdlib imports (if not already present). No third-party imports needed.

Replace the `Save()` method — change:
```go
// Save writes the graph to the file.
func (g *SavedGraph[K]) Save() error {
	tmp, err := renameio.TempFile("", g.Path)
	if err != nil {
		return err
	}
	defer tmp.Cleanup()

	wr := bufio.NewWriter(tmp)
	err = g.Export(wr)
	if err != nil {
		return fmt.Errorf("exporting: %w", err)
	}

	err = wr.Flush()
	if err != nil {
		return fmt.Errorf("flushing: %w", err)
	}

	err = tmp.CloseAtomicallyReplace()
	if err != nil {
		return fmt.Errorf("closing atomically: %w", err)
	}

	return nil
}
```

To:
```go
// Save writes the graph to the file.
func (g *SavedGraph[K]) Save() error {
	dir := filepath.Dir(g.Path)
	tmp, err := os.CreateTemp(dir, ".hnsw-save-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // clean up on failure

	wr := bufio.NewWriter(tmp)
	if err := g.Export(wr); err != nil {
		tmp.Close()
		return fmt.Errorf("exporting: %w", err)
	}
	if err := wr.Flush(); err != nil {
		tmp.Close()
		return fmt.Errorf("flushing: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpPath, g.Path); err != nil {
		return fmt.Errorf("renaming temp file: %w", err)
	}

	return nil
}
```

This approach:
- Uses `os.CreateTemp` in the same directory as the target to ensure same-filesystem rename
- `bufio.NewWriter` batches small writes for efficiency
- `os.Rename` atomically replaces the target file (uses `rename(2)` on Unix, `MoveFileExW` on Windows)
- Deferred `os.Remove` ensures temp file cleanup on any error path
- Zero new dependencies — pure stdlib
- Cross-platform: works on Linux, macOS, and Windows

**Step 3: Run go mod tidy**

```bash
go mod tidy
```

Expected: `google/renameio` removed from go.mod/go.sum. No new dependencies added.

**Step 5: Run existing tests**

```bash
go test ./...
```

Expected: all tests pass. The existing `encode_test.go` / integration tests exercise `Save()` and `LoadSavedGraph()`.

**Step 6: Cross-compile to verify Windows builds**

```bash
GOOS=windows GOARCH=amd64 go build ./...
```

Expected: builds successfully (previously failed due to renameio).

**Step 7: Commit**

```bash
git add -A
git commit -m "fix: replace renameio with stdlib os.CreateTemp + os.Rename

Use os.CreateTemp + bufio.NewWriter + os.Rename for atomic saves.
Pure stdlib, zero new dependencies, cross-platform.

Closes coder/hnsw#9"
```

**Step 8: Push and tag**

```bash
git push -u origin fix/windows-atomic-save
```

---

### Task 3: Wire floop to use the fork

**Files:**
- Modify: `go.mod` (add replace directive)

**Step 1: Add replace directive to go.mod**

Add this line after the first `require` block:

```
replace github.com/coder/hnsw v0.6.1 => github.com/nvandessel/hnsw fix/windows-atomic-save
```

Note: if the branch ref doesn't resolve cleanly, use the specific commit hash instead:

```
replace github.com/coder/hnsw v0.6.1 => github.com/nvandessel/hnsw <commit-hash>
```

**Step 2: Run go mod tidy**

```bash
GOWORK=off go mod tidy
```

Expected: go.sum updated with nvandessel/hnsw entries. `google/renameio` should be removed from go.sum (no longer transitively needed). No `natefinch/atomic` entries — the fork uses pure stdlib.

**Step 3: Verify build**

```bash
GOWORK=off go build ./...
```

Expected: builds successfully.

**Step 4: Run tests**

```bash
GOWORK=off go test ./internal/vectorindex/...
```

Expected: all 11 HNSW tests + 7 tiered tests + brute-force tests pass.

**Step 5: Commit**

```bash
git add go.mod go.sum
git commit -m "build: point coder/hnsw to nvandessel/hnsw fork

The fork replaces google/renameio with pure stdlib atomic saves
(os.CreateTemp + os.Rename) for cross-platform support. This is a
prerequisite for removing the Windows brute-force fallback.

Ref #174"
```

---

### Task 4: Remove Windows build-tag workaround

**Files:**
- Delete: `internal/vectorindex/hnsw_windows.go`
- Modify: `internal/vectorindex/hnsw.go` (remove build tag on line 1)
- Modify: `internal/vectorindex/hnsw_test.go` (remove build tag on line 1)

**Step 1: Remove the build tag from hnsw.go**

Delete line 1 (`//go:build !windows`) and the blank line after it from `internal/vectorindex/hnsw.go`.

Before:
```go
//go:build !windows

package vectorindex
```

After:
```go
package vectorindex
```

**Step 2: Remove the build tag from hnsw_test.go**

Delete line 1 (`//go:build !windows`) and the blank line after it from `internal/vectorindex/hnsw_test.go`.

Before:
```go
//go:build !windows

package vectorindex
```

After:
```go
package vectorindex
```

**Step 3: Delete hnsw_windows.go**

```bash
rm internal/vectorindex/hnsw_windows.go
```

**Step 4: Verify it compiles for all platforms**

```bash
GOWORK=off go build ./...
GOWORK=off GOOS=windows GOARCH=amd64 go build ./...
GOWORK=off GOOS=darwin GOARCH=arm64 go build ./...
```

Expected: all three pass. No more build-tag split.

**Step 5: Run the full test suite**

```bash
GOWORK=off go test -race ./...
```

Expected: all tests pass including HNSW persistence tests.

**Step 6: Commit**

```bash
git add internal/vectorindex/hnsw.go internal/vectorindex/hnsw_test.go
git rm internal/vectorindex/hnsw_windows.go
git commit -m "feat: remove Windows HNSW fallback, enable native HNSW on all platforms

With the nvandessel/hnsw fork replacing google/renameio with stdlib
atomic saves (os.CreateTemp + os.Rename), the library now builds on
Windows. Remove the brute-force fallback and build tags.

Closes #174"
```

---

### Task 5: Final validation

**Step 1: Run full CI-equivalent checks**

```bash
GOWORK=off go vet ./...
GOWORK=off go test -race -count=1 ./...
```

**Step 2: Verify no renameio references remain**

```bash
grep -r "renameio" . --include="*.go" || echo "Clean: no renameio references"
grep -r "hnsw_windows" . --include="*.go" || echo "Clean: no hnsw_windows references"
grep -r "go:build.*windows" internal/vectorindex/ || echo "Clean: no windows build tags in vectorindex"
```

Expected: all three print "Clean" messages.

**Step 3: Verify the EMBEDDINGS.md docs are still accurate**

Check `docs/EMBEDDINGS.md` for any references to "Windows falls back to brute-force" and update if present.

**Step 4: Commit docs update if needed**

```bash
git add docs/EMBEDDINGS.md
git commit -m "docs: update EMBEDDINGS.md to reflect cross-platform HNSW support"
```
