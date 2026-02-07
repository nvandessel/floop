// Package mcp provides an MCP (Model Context Protocol) server for floop.
package mcp

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/feedback-loop/internal/ranking"
	"github.com/nvandessel/feedback-loop/internal/ratelimit"
	"github.com/nvandessel/feedback-loop/internal/session"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// maxBackgroundWorkers is the maximum number of concurrent background goroutines.
const maxBackgroundWorkers = 5

// Server wraps the MCP SDK server and provides floop-specific functionality.
type Server struct {
	server        *sdk.Server
	store         store.GraphStore
	root          string
	session       *session.State
	pageRankMu    sync.RWMutex
	pageRankCache map[string]float64

	// Rate limiting
	toolLimiters ratelimit.ToolLimiters

	// PageRank debounce
	pageRankDebounce   *time.Timer
	pageRankDebounceMu sync.Mutex

	// Bounded worker pool for background goroutines
	workerPool chan struct{}

	// Shutdown coordination
	done      chan struct{} // closed on shutdown
	closeOnce sync.Once
}

// Config holds server configuration.
type Config struct {
	Name    string // Server name (e.g., "floop")
	Version string // Server version
	Root    string // Project root directory
}

// NewServer creates a new MCP server with floop tools.
func NewServer(cfg *Config) (*Server, error) {
	// Create multi-graph store (local + global)
	graphStore, err := store.NewMultiGraphStore(cfg.Root, store.ScopeBoth)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph store: %w", err)
	}

	// Create MCP server
	mcpServer := sdk.NewServer(&sdk.Implementation{
		Name:    cfg.Name,
		Version: cfg.Version,
	}, &sdk.ServerOptions{
		InitializedHandler: func(ctx context.Context, req *sdk.InitializedRequest) {
			// Client initialized, ready to serve
		},
	})

	s := &Server{
		server:        mcpServer,
		store:         graphStore,
		root:          cfg.Root,
		session:       session.NewState(session.DefaultConfig()),
		pageRankCache: make(map[string]float64),
		toolLimiters:  ratelimit.NewToolLimiters(),
		workerPool:    make(chan struct{}, maxBackgroundWorkers),
		done:          make(chan struct{}),
	}

	// Register tools
	if err := s.registerTools(); err != nil {
		graphStore.Close()
		return nil, fmt.Errorf("failed to register tools: %w", err)
	}

	// Register resources for auto-loading into context
	if err := s.registerResources(); err != nil {
		graphStore.Close()
		return nil, fmt.Errorf("failed to register resources: %w", err)
	}

	// Compute initial PageRank cache
	if err := s.refreshPageRank(context.Background()); err != nil {
		// Non-fatal: log but don't fail startup
		fmt.Fprintf(os.Stderr, "warning: failed to compute initial PageRank: %v\n", err)
	}

	return s, nil
}

// refreshPageRank recomputes the PageRank cache from the current graph state.
// This should be called after any operation that modifies the behavior graph
// (e.g., floop_learn, floop_deduplicate).
func (s *Server) refreshPageRank(ctx context.Context) error {
	scores, err := ranking.ComputePageRank(ctx, s.store, ranking.DefaultPageRankConfig())
	if err != nil {
		return fmt.Errorf("refreshing pagerank: %w", err)
	}

	s.pageRankMu.Lock()
	s.pageRankCache = scores
	s.pageRankMu.Unlock()

	return nil
}

// getPageRankScores returns a copy of the current PageRank cache.
func (s *Server) getPageRankScores() map[string]float64 {
	s.pageRankMu.RLock()
	defer s.pageRankMu.RUnlock()

	scores := make(map[string]float64, len(s.pageRankCache))
	for k, v := range s.pageRankCache {
		scores[k] = v
	}
	return scores
}

// debouncedRefreshPageRank schedules a PageRank refresh after a short delay.
// Multiple rapid calls coalesce into a single recomputation.
func (s *Server) debouncedRefreshPageRank() {
	s.pageRankDebounceMu.Lock()
	defer s.pageRankDebounceMu.Unlock()

	if s.pageRankDebounce != nil {
		s.pageRankDebounce.Stop()
	}
	s.pageRankDebounce = time.AfterFunc(2*time.Second, func() {
		select {
		case <-s.done:
			return // server is shutting down
		default:
		}
		if err := s.refreshPageRank(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "warning: debounced PageRank refresh failed: %v\n", err)
		}
	})
}

// runBackground executes fn in a bounded goroutine pool.
// If the pool is full, the task is dropped with a warning.
// If the server is shutting down, the task is not started.
func (s *Server) runBackground(name string, fn func()) {
	select {
	case <-s.done:
		return // server is shutting down
	case s.workerPool <- struct{}{}:
		go func() {
			defer func() { <-s.workerPool }()
			select {
			case <-s.done:
				return
			default:
			}
			fn()
		}()
	default:
		fmt.Fprintf(os.Stderr, "warning: background worker pool full, skipping %s\n", name)
	}
}

// Run starts the MCP server over stdio transport.
// This blocks until the client disconnects or the context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle OS signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	// Run server (blocks)
	err := s.server.Run(ctx, &sdk.StdioTransport{})

	// Clean up (idempotent — safe if Close() was already called)
	s.Close()

	return err
}

// Close closes the server and releases resources.
// It is safe to call Close multiple times; only the first call takes effect.
func (s *Server) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		close(s.done)

		s.pageRankDebounceMu.Lock()
		if s.pageRankDebounce != nil {
			s.pageRankDebounce.Stop()
		}
		s.pageRankDebounceMu.Unlock()

		closeErr = s.store.Close()
	})
	return closeErr
}
