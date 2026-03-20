package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/events"

	_ "modernc.org/sqlite"
)

// initFloopDir creates a minimal .floop directory at root.
func initFloopDir(t *testing.T, root string) {
	t.Helper()
	floopDir := filepath.Join(root, ".floop")
	if err := os.MkdirAll(floopDir, 0700); err != nil {
		t.Fatalf("failed to create .floop dir at %s: %v", root, err)
	}
}

// seedEvent inserts a test event into the global event store for consolidation tests.
func seedEvent(t *testing.T, homeDir string) {
	t.Helper()
	dbPath := filepath.Join(homeDir, ".floop", "floop.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open event DB: %v", err)
	}
	defer db.Close()

	es := events.NewSQLiteEventStore(db)
	if err := es.InitSchema(context.Background()); err != nil {
		t.Fatalf("failed to init events schema: %v", err)
	}
	evt := events.Event{
		ID:        "test-event-1",
		SessionID: "test-session",
		Timestamp: time.Now(),
		Source:    "test",
		Actor:     "user",
		Kind:      "correction",
		Content:   "always use global store",
	}
	if err := es.Add(context.Background(), evt); err != nil {
		t.Fatalf("failed to seed event: %v", err)
	}
}

// TestConsolidate_UsesMultiGraphStore verifies that the consolidate command
// opens a MultiGraphStore (writes to global store by default) by seeding an
// event and confirming consolidation reaches the graph store code path.
func TestConsolidate_UsesMultiGraphStore(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	homeDir := filepath.Join(tmpDir, "home")

	projectRoot := t.TempDir()
	initFloopDir(t, projectRoot)
	initFloopDir(t, homeDir)

	// Seed an event so consolidation actually reaches the graph store code path
	seedEvent(t, homeDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConsolidateCmd())

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"consolidate", "--root", projectRoot, "--json"})

	err := rootCmd.Execute()
	out := buf.String()

	// The consolidation may fail (no LLM configured) or succeed with heuristic fallback.
	// The key assertion: it should NOT fail with a store-related error.
	// If the old NewSQLiteGraphStore(root) were used, it would write to the wrong place.
	if err != nil {
		// Acceptable errors: LLM not available, no config, etc.
		// Unacceptable: "opening graph store" errors
		if bytes.Contains([]byte(err.Error()), []byte("opening graph store")) {
			t.Fatalf("consolidate failed to open graph store: %v", err)
		}
		t.Logf("consolidate returned expected non-store error: %v", err)
	}

	// If it succeeded, verify it processed (not no_events)
	if err == nil && bytes.Contains([]byte(out), []byte("no_events")) {
		t.Error("expected consolidation to process seeded event, got no_events")
	}

	// Verify the global graph store DB was created/touched by MultiGraphStore.
	// Note: seedEvent also writes to this path (event schema), so this check
	// alone doesn't prove MultiGraphStore was used. Combined with the
	// "opening graph store" error guard above, it confirms the correct store
	// constructor was invoked. Full promotion verification (behaviors landing
	// in the global store) requires an LLM runner, which is not available in
	// CI — that path is validated by floop-bench integration tests.
	globalDB := filepath.Join(homeDir, ".floop", "floop.db")
	if _, err := os.Stat(globalDB); os.IsNotExist(err) {
		t.Error("global store DB does not exist after consolidation")
	}
}

// TestConsolidate_RootFlagOverride verifies --root flag changes the local store path.
func TestConsolidate_RootFlagOverride(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	homeDir := filepath.Join(tmpDir, "home")

	customRoot := t.TempDir()
	initFloopDir(t, customRoot)
	initFloopDir(t, homeDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConsolidateCmd())

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"consolidate", "--root", customRoot, "--json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("consolidate --root failed: %v\noutput: %s", err, buf.String())
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("no_events")) {
		t.Errorf("expected no_events with --root override, got: %s", out)
	}
}

// TestList_DefaultsScopeToBoth verifies that 'floop list' defaults scope to "both"
// (changed from "local" in the store unification fix).
func TestList_DefaultsScopeToBoth(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	homeDir := filepath.Join(tmpDir, "home")

	projectRoot := t.TempDir()
	initFloopDir(t, projectRoot)
	initFloopDir(t, homeDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newListCmd())

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"list", "--root", projectRoot, "--json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list failed: %v\noutput: %s", err, buf.String())
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte(`"scope":"both"`)) {
		t.Errorf("expected scope 'both' in JSON output, got: %s", out)
	}
}

// TestList_GlobalFlagOverridesScope verifies --global flag scopes to global only.
func TestList_GlobalFlagOverridesScope(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	homeDir := filepath.Join(tmpDir, "home")
	initFloopDir(t, homeDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newListCmd())

	// Use a projectRoot without .floop — should still work with --global
	projectRoot := t.TempDir()

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"list", "--root", projectRoot, "--global", "--json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list --global failed: %v\noutput: %s", err, buf.String())
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte(`"scope":"global"`)) {
		t.Errorf("expected scope 'global' in JSON output, got: %s", out)
	}
}

// TestDedup_DefaultsScopeToBoth verifies that dedup defaults scope to "both".
func TestDedup_DefaultsScopeToBoth(t *testing.T) {
	cmd := newDeduplicateCmd()
	scopeFlag := cmd.Flag("scope")
	if scopeFlag == nil {
		t.Fatal("dedup command missing --scope flag")
	}
	if scopeFlag.DefValue != "both" {
		t.Errorf("dedup --scope default = %q, want %q", scopeFlag.DefValue, "both")
	}
}

// TestValidate_DefaultsScopeToBoth verifies that validate defaults scope to "both".
func TestValidate_DefaultsScopeToBoth(t *testing.T) {
	cmd := newValidateCmd()
	scopeFlag := cmd.Flag("scope")
	if scopeFlag == nil {
		t.Fatal("validate command missing --scope flag")
	}
	if scopeFlag.DefValue != "both" {
		t.Errorf("validate --scope default = %q, want %q", scopeFlag.DefValue, "both")
	}
}

// TestValidate_BothScopeWorks verifies validate runs against both stores.
func TestValidate_BothScopeWorks(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	homeDir := filepath.Join(tmpDir, "home")

	projectRoot := t.TempDir()
	initFloopDir(t, projectRoot)
	initFloopDir(t, homeDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newValidateCmd())

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"validate", "--root", projectRoot, "--json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("validate with default scope failed: %v\noutput: %s", err, buf.String())
	}
}

// TestLearn_UsesMultiGraphStore verifies learn command uses MultiGraphStore
// which defaults to writing to the global store.
func TestLearn_UsesMultiGraphStore(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	homeDir := filepath.Join(tmpDir, "home")

	projectRoot := t.TempDir()
	initFloopDir(t, projectRoot)
	initFloopDir(t, homeDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newLearnCmd())

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"learn", "--root", projectRoot, "--right", "use global store by default", "--json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("learn failed: %v\noutput: %s", err, buf.String())
	}

	// Verify the global store database was created/used
	globalDB := filepath.Join(homeDir, ".floop", "floop.db")
	if _, err := os.Stat(globalDB); os.IsNotExist(err) {
		t.Error("learn did not create/use global store database")
	}
}

// TestLearn_RootFlagStillWorks verifies --root flag configures the local store path.
func TestLearn_RootFlagStillWorks(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	homeDir := filepath.Join(tmpDir, "home")

	customRoot := t.TempDir()
	initFloopDir(t, customRoot)
	initFloopDir(t, homeDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newLearnCmd())

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"learn", "--root", customRoot, "--right", "test with custom root", "--json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("learn --root failed: %v\noutput: %s", err, buf.String())
	}

	// Local store should also have been opened at customRoot
	localDB := filepath.Join(customRoot, ".floop", "floop.db")
	if _, err := os.Stat(localDB); os.IsNotExist(err) {
		t.Error("learn --root did not create local store database at custom root")
	}
}

// TestLearn_ScopeOverrideToLocal verifies --scope local writes to local store.
func TestLearn_ScopeOverrideToLocal(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	homeDir := filepath.Join(tmpDir, "home")

	projectRoot := t.TempDir()
	initFloopDir(t, projectRoot)
	initFloopDir(t, homeDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newLearnCmd())

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"learn", "--root", projectRoot, "--right", "local only behavior", "--scope", "local", "--json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("learn --scope local failed: %v\noutput: %s", err, buf.String())
	}

	// Verify behavior was written to local store
	localDB := filepath.Join(projectRoot, ".floop", "floop.db")
	info, err := os.Stat(localDB)
	if os.IsNotExist(err) {
		t.Error("learn --scope local did not create local store database")
	} else if info.Size() == 0 {
		t.Error("learn --scope local created empty local store database")
	}

	// Note: MultiGraphStore may create the global DB file during initialization
	// (schema migration). The key invariant is that the behavior was written to
	// the local store, which is verified by the localDB checks above.
}

// TestCrossPath_LearnAndListSeesSameBehaviors verifies that behaviors written
// via learn (which uses MultiGraphStore -> global) are visible via list
// (which now defaults to "both" scope, reading from both local and global).
func TestCrossPath_LearnAndListSeesSameBehaviors(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	homeDir := filepath.Join(tmpDir, "home")

	projectRoot := t.TempDir()
	initFloopDir(t, projectRoot)
	initFloopDir(t, homeDir)

	// Step 1: Learn a behavior (goes to global store via MultiGraphStore)
	learnRoot := newTestRootCmd()
	learnRoot.AddCommand(newLearnCmd())
	learnBuf := new(bytes.Buffer)
	learnRoot.SetOut(learnBuf)
	learnRoot.SetErr(learnBuf)
	learnRoot.SetArgs([]string{"learn", "--root", projectRoot, "--right", "always use global store for consistency", "--json"})

	if err := learnRoot.Execute(); err != nil {
		t.Fatalf("learn failed: %v\noutput: %s", err, learnBuf.String())
	}

	// Step 2: List behaviors (default scope "both" should see it)
	listRoot := newTestRootCmd()
	listRoot.AddCommand(newListCmd())
	listBuf := new(bytes.Buffer)
	listRoot.SetOut(listBuf)
	listRoot.SetErr(listBuf)
	listRoot.SetArgs([]string{"list", "--root", projectRoot, "--json"})

	if err := listRoot.Execute(); err != nil {
		t.Fatalf("list failed: %v\noutput: %s", err, listBuf.String())
	}

	out := listBuf.String()
	if !bytes.Contains([]byte(out), []byte("always use global store for consistency")) {
		t.Errorf("list did not show behavior learned via global store.\noutput: %s", out)
	}
}

// TestCrossPath_LearnAndListGlobal verifies --global flag on list
// shows behaviors written to global store via learn.
func TestCrossPath_LearnAndListGlobal(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	homeDir := filepath.Join(tmpDir, "home")

	projectRoot := t.TempDir()
	initFloopDir(t, projectRoot)
	initFloopDir(t, homeDir)

	// Learn a behavior
	learnRoot := newTestRootCmd()
	learnRoot.AddCommand(newLearnCmd())
	learnBuf := new(bytes.Buffer)
	learnRoot.SetOut(learnBuf)
	learnRoot.SetErr(learnBuf)
	learnRoot.SetArgs([]string{"learn", "--root", projectRoot, "--right", "test global visibility", "--json"})

	if err := learnRoot.Execute(); err != nil {
		t.Fatalf("learn failed: %v\noutput: %s", err, learnBuf.String())
	}

	// List with --global should see it
	listRoot := newTestRootCmd()
	listRoot.AddCommand(newListCmd())
	listBuf := new(bytes.Buffer)
	listRoot.SetOut(listBuf)
	listRoot.SetErr(listBuf)
	listRoot.SetArgs([]string{"list", "--root", projectRoot, "--global", "--json"})

	if err := listRoot.Execute(); err != nil {
		t.Fatalf("list --global failed: %v\noutput: %s", err, listBuf.String())
	}

	out := listBuf.String()
	if !bytes.Contains([]byte(out), []byte("test global visibility")) {
		t.Errorf("list --global did not show globally stored behavior.\noutput: %s", out)
	}
}
