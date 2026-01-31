// Package mcp provides an MCP (Model Context Protocol) server for floop.
package mcp

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// Server wraps the MCP SDK server and provides floop-specific functionality.
type Server struct {
	server *sdk.Server
	store  store.GraphStore
	root   string
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
		server: mcpServer,
		store:  graphStore,
		root:   cfg.Root,
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

	return s, nil
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
