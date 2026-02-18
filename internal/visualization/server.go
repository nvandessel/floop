package visualization

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/nvandessel/feedback-loop/internal/spreading"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// Server serves the interactive graph HTML and handles activation API requests.
type Server struct {
	store      store.GraphStore
	enrichment *EnrichmentData
	engine     *spreading.Engine
	httpServer *http.Server
	listener   net.Listener
	mu         sync.Mutex
	addr       string
	cachedHTML []byte // cached index page (rendered once at startup)
}

// NewServer creates a new graph visualization server.
func NewServer(gs store.GraphStore, enrichment *EnrichmentData) *Server {
	cfg := spreading.DefaultConfig()
	return &Server{
		store:      gs,
		enrichment: enrichment,
		engine:     spreading.NewEngine(gs, cfg),
	}
}

// Addr returns the address the server is listening on (e.g., "localhost:PORT").
// Returns empty string if the server hasn't started yet.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// ListenAndServe starts the HTTP server on an OS-assigned port and blocks
// until the context is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/activate", s.handleActivate)

	// Let the OS pick a free port.
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	s.mu.Lock()
	s.listener = ln
	s.addr = ln.Addr().String()
	s.httpServer = &http.Server{Handler: mux}
	s.mu.Unlock()

	// Pre-render the index page now that we know the address.
	apiBaseURL := "http://" + s.Addr()
	html, err := RenderHTMLForServer(ctx, s.store, s.enrichment, apiBaseURL)
	if err != nil {
		ln.Close()
		return fmt.Errorf("pre-render HTML: %w", err)
	}
	s.mu.Lock()
	s.cachedHTML = html
	s.mu.Unlock()

	// Graceful shutdown when context is cancelled.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutdownCtx)
	}()

	err = s.httpServer.Serve(ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// handleIndex serves the cached graph HTML page.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	s.mu.Lock()
	html := s.cachedHTML
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(html)
}

// handleActivate runs spreading activation for a seed node and returns step snapshots.
func (s *Server) handleActivate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	seedID := r.URL.Query().Get("seed")
	if seedID == "" {
		http.Error(w, "missing 'seed' query parameter", http.StatusBadRequest)
		return
	}

	// Verify the seed node exists.
	node, err := s.store.GetNode(r.Context(), seedID)
	if err != nil || node == nil {
		http.Error(w, "seed node not found", http.StatusNotFound)
		return
	}

	seeds := []spreading.Seed{{
		BehaviorID: seedID,
		Activation: 1.0,
		Source:     "electric-mode",
	}}

	steps, err := s.engine.ActivateWithSteps(r.Context(), seeds)
	if err != nil {
		http.Error(w, "activation failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(steps)
}
