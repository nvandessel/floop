// Package mcp provides an MCP (Model Context Protocol) server for floop.
package mcp

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/feedback-loop/internal/ranking"
	"github.com/nvandessel/feedback-loop/internal/session"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// Server wraps the MCP SDK server and provides floop-specific functionality.
type Server struct {
	server        *sdk.Server
	store         store.GraphStore
	root          string
	session       *session.State
	pageRankMu    sync.RWMutex
	pageRankCache map[string]float64
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

	// Clean up
	s.store.Close()

	return err
}

// Close closes the server and releases resources.
func (s *Server) Close() error {
	return s.store.Close()
}
