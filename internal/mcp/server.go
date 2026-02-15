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
	"github.com/nvandessel/feedback-loop/internal/backup"
	"github.com/nvandessel/feedback-loop/internal/config"
	"github.com/nvandessel/feedback-loop/internal/ranking"
	"github.com/nvandessel/feedback-loop/internal/ratelimit"
	"github.com/nvandessel/feedback-loop/internal/seed"
	"github.com/nvandessel/feedback-loop/internal/session"
	"github.com/nvandessel/feedback-loop/internal/spreading"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// maxBackgroundWorkers is the maximum number of concurrent background goroutines.
const maxBackgroundWorkers = 5

// Server wraps the MCP SDK server and provides floop-specific functionality.
type Server struct {
	server        *sdk.Server
	store         store.GraphStore
	root          string
	floopConfig   *config.FloopConfig
	session       *session.State
	pageRankMu    sync.RWMutex
	pageRankCache map[string]float64

	// Audit logging
	auditLogger *AuditLogger

	// Rate limiting
	toolLimiters ratelimit.ToolLimiters

	// PageRank debounce
	pageRankDebounce   *time.Timer
	pageRankDebounceMu sync.Mutex

	// Backup configuration and retention policy
	backupConfig    *config.BackupConfig
	retentionPolicy backup.RetentionPolicy

	// Bounded worker pool for background goroutines
	workerPool chan struct{}

	// Session-scoped implicit confirmation tracking.
	// Each behavior gets at most 1 implicit confirmation per session (not per
	// floop_active call). This measures "how many distinct work sessions
	// involved this behavior" — a far better usefulness proxy than per-call counting.
	confirmedSessionMu   sync.Mutex
	confirmedThisSession map[string]struct{}

	// Hebbian co-activation learning
	coActivationTracker *coActivationTracker
	hebbianConfig       spreading.HebbianConfig

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

	// Determine home directory for global audit log
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot determine home directory for global audit log: %v\n", err)
		homeDir = "" // NewAuditLogger handles empty dir gracefully
	}

	// Load floop config (non-fatal: use defaults on error)
	floopCfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to load config, using defaults: %v\n", err)
		floopCfg = config.Default()
	}
	retPolicy := buildRetentionPolicy(&floopCfg.Backup)

	s := &Server{
		server:               mcpServer,
		store:                graphStore,
		root:                 cfg.Root,
		floopConfig:          floopCfg,
		session:              session.NewState(session.DefaultConfig()),
		auditLogger:          NewAuditLogger(cfg.Root, homeDir),
		pageRankCache:        make(map[string]float64),
		toolLimiters:         ratelimit.NewToolLimiters(),
		backupConfig:         &floopCfg.Backup,
		retentionPolicy:      retPolicy,
		workerPool:           make(chan struct{}, maxBackgroundWorkers),
		confirmedThisSession: make(map[string]struct{}),
		coActivationTracker:  newCoActivationTracker(),
		hebbianConfig:        spreading.DefaultHebbianConfig(),
		done:                 make(chan struct{}),
	}

	// Auto-seed meta-behaviors into global store (non-fatal)
	autoSeedGlobalStore(graphStore)

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

// autoSeedGlobalStore seeds meta-behaviors into the global store.
// This is non-fatal: errors are logged to stderr but do not block startup.
func autoSeedGlobalStore(graphStore *store.MultiGraphStore) {
	globalStore := graphStore.GlobalStore()
	result, err := seed.NewSeeder(globalStore).SeedGlobalStore(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to auto-seed global store: %v\n", err)
		return
	}
	if len(result.Added) > 0 || len(result.Updated) > 0 {
		fmt.Fprintf(os.Stderr, "floop: seeded %d, updated %d meta-behavior(s)\n", len(result.Added), len(result.Updated))
	}
}

// buildRetentionPolicy constructs a retention policy from backup config.
func buildRetentionPolicy(cfg *config.BackupConfig) backup.RetentionPolicy {
	var policies []backup.RetentionPolicy

	if cfg.Retention.MaxCount > 0 {
		policies = append(policies, &backup.CountPolicy{MaxCount: cfg.Retention.MaxCount})
	}

	if cfg.Retention.MaxAge != "" {
		if d, err := backup.ParseDuration(cfg.Retention.MaxAge); err == nil {
			policies = append(policies, &backup.AgePolicy{MaxAge: d})
		}
	}

	if cfg.Retention.MaxTotalSize != "" {
		if s, err := backup.ParseSize(cfg.Retention.MaxTotalSize); err == nil {
			policies = append(policies, &backup.SizePolicy{MaxTotalBytes: s})
		}
	}

	if len(policies) == 0 {
		return &backup.CountPolicy{MaxCount: 10}
	}

	if len(policies) == 1 {
		return policies[0]
	}

	return &backup.CompositePolicy{Policies: policies}
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

		if s.auditLogger != nil {
			s.auditLogger.Close()
		}

		closeErr = s.store.Close()
	})
	return closeErr
}
