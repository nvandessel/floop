package visualization

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/spreading"
	"github.com/nvandessel/feedback-loop/internal/store"
)

func TestServer_ServesHTML(t *testing.T) {
	gs := setupTestStore(t)
	addBehavior(t, gs, "b1", "test-behavior", "directive", 0.8)

	enrichment := &EnrichmentData{
		PageRank: map[string]float64{"b1": 0.5},
	}

	srv := NewServer(gs, enrichment)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	// Wait for server to start
	waitForServer(t, srv, 2*time.Second)

	resp, err := http.Get("http://" + srv.Addr() + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET / status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
}

func TestServer_ActivateEndpoint(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "behavior-a", "directive", 0.8)
	addBehavior(t, gs, "b2", "behavior-b", "constraint", 0.9)
	if err := gs.AddEdge(ctx, store.Edge{
		Source: "b1", Target: "b2", Kind: "requires",
		Weight: 0.8, CreatedAt: time.Now(),
		LastActivated: timePtr(time.Now()),
	}); err != nil {
		t.Fatalf("add edge: %v", err)
	}

	srv := NewServer(gs, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	waitForServer(t, srv, 2*time.Second)

	resp, err := http.Get("http://" + srv.Addr() + "/api/activate?seed=b1")
	if err != nil {
		t.Fatalf("GET /api/activate: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var steps []spreading.StepSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&steps); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}

	if len(steps) == 0 {
		t.Fatal("expected non-empty steps")
	}

	// First step should have seed activation
	if act, ok := steps[0].Activation["b1"]; !ok || act == 0 {
		t.Error("expected b1 to have activation in step 0")
	}

	// Last step should be final
	last := steps[len(steps)-1]
	if !last.Final {
		t.Error("last step should be marked Final")
	}
}

func TestServer_ActivateEndpoint_UnknownSeed(t *testing.T) {
	gs := setupTestStore(t)
	addBehavior(t, gs, "b1", "test-behavior", "directive", 0.8)

	srv := NewServer(gs, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	waitForServer(t, srv, 2*time.Second)

	resp, err := http.Get("http://" + srv.Addr() + "/api/activate?seed=nonexistent")
	if err != nil {
		t.Fatalf("GET /api/activate: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for unknown seed", resp.StatusCode)
	}
}

func TestServer_ActivateEndpoint_MissingSeed(t *testing.T) {
	gs := setupTestStore(t)

	srv := NewServer(gs, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	waitForServer(t, srv, 2*time.Second)

	resp, err := http.Get("http://" + srv.Addr() + "/api/activate")
	if err != nil {
		t.Fatalf("GET /api/activate: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for missing seed param", resp.StatusCode)
	}
}

func TestServer_CleanShutdown(t *testing.T) {
	gs := setupTestStore(t)

	srv := NewServer(gs, nil)
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	waitForServer(t, srv, 2*time.Second)

	// Cancel context to trigger shutdown
	cancel()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("unexpected error on shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not shut down within 3 seconds")
	}
}

// waitForServer polls the server until it's ready or the timeout is reached.
func waitForServer(t *testing.T, srv *Server, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		addr := srv.Addr()
		if addr == "" {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		resp, err := http.Get("http://" + addr + "/")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not start within timeout")
}

// timePtr is a test helper (also defined in spreading tests, but scoped to this package).
func timePtr(t time.Time) *time.Time { return &t }
