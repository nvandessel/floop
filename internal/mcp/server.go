// Package mcp provides an MCP (Model Context Protocol) server for floop.
package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"path/filepath"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/floop/internal/backup"
	"github.com/nvandessel/floop/internal/config"
	"github.com/nvandessel/floop/internal/constants"
	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/ranking"
	"github.com/nvandessel/floop/internal/ratelimit"
	"github.com/nvandessel/floop/internal/seed"
	"github.com/nvandessel/floop/internal/session"
	"github.com/nvandessel/floop/internal/setup"
	"github.com/nvandessel/floop/internal/spreading"
	"github.com/nvandessel/floop/internal/store"
	"github.com/nvandessel/floop/internal/vectorindex"
	"github.com/nvandessel/floop/internal/vectorsearch"
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

	// Vector embedding retrieval
	embedder  *vectorsearch.Embedder
	llmClient llm.Client // held for cleanup (Close)

	// Version info (from ldflags)
	floopVersion string

	// Vector index for fast ANN search over behavior embeddings
	vectorIndex vectorindex.VectorIndex

	// Structured logger for warnings and info
	logger *slog.Logger

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
	graphStore, err := store.NewMultiGraphStore(cfg.Root)
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
		floopVersion:         cfg.Version,
		floopConfig:          floopCfg,
		session:              session.NewState(session.DefaultConfig()),
		auditLogger:          NewAuditLogger(cfg.Root, homeDir),
		pageRankCache:        make(map[string]float64),
		toolLimiters:         ratelimit.NewToolLimiters(),
		backupConfig:         &floopCfg.Backup,
		retentionPolicy:      retPolicy,
		workerPool:           make(chan struct{}, maxBackgroundWorkers),
		confirmedThisSession: make(map[string]struct{}),
		coActivationTracker:  initCoActivationTracker(graphStore),
		hebbianConfig:        spreading.DefaultHebbianConfig(),
		logger:               slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
		done:                 make(chan struct{}),
	}

	// Initialize local embedding client.
	// Priority: explicit config > auto-detect from ~/.floop/
	if floopCfg.LLM.Provider == "local" {
		embModelPath := floopCfg.LLM.LocalEmbeddingModelPath
		if embModelPath == "" {
			embModelPath = floopCfg.LLM.LocalModelPath
		}
		if embModelPath != "" {
			localClient := llm.NewLocalClient(llm.LocalConfig{
				LibPath:            floopCfg.LLM.LocalLibPath,
				EmbeddingModelPath: embModelPath,
				GPULayers:          floopCfg.LLM.LocalGPULayers,
				ContextSize:        floopCfg.LLM.LocalContextSize,
			})
			if localClient.Available() {
				s.llmClient = localClient
				modelName := filepath.Base(embModelPath)
				s.embedder = vectorsearch.NewEmbedder(localClient.Embed, modelName)
			}
		}
	}
	// Auto-detect: if no embedder from config, check ~/.floop/ for installed deps
	if s.embedder == nil {
		detected := setup.DetectInstalled(setup.DefaultFloopDir())
		if detected.Available {
			localClient := llm.NewLocalClient(llm.LocalConfig{
				LibPath:            detected.LibPath,
				EmbeddingModelPath: detected.ModelPath,
			})
			if localClient.Available() {
				s.llmClient = localClient
				modelName := filepath.Base(detected.ModelPath)
				s.embedder = vectorsearch.NewEmbedder(localClient.Embed, modelName)
			}
		}
	}

	// Initialize vector index for fast ANN retrieval.
	if s.embedder != nil && s.embedder.Available() {
		// Clean up legacy HNSW index file.
		hnswPath := filepath.Join(cfg.Root, ".floop", "hnsw.bin")
		if _, err := os.Stat(hnswPath); err == nil {
			s.logger.Warn("removing legacy HNSW index (replaced by LanceDB)", "path", hnswPath)
			if err := os.Remove(hnswPath); err != nil {
				s.logger.Warn("failed to remove legacy HNSW index", "path", hnswPath, "error", err)
			}
		}

		vectorDir := filepath.Join(cfg.Root, ".floop", "vectors")
		if err := os.MkdirAll(vectorDir, 0o755); err != nil {
			s.logger.Warn("failed to create vector directory", "error", err)
		}
		s.vectorIndex = s.initVectorIndex(graphStore, vectorDir)
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
		s.logger.Warn("failed to compute initial PageRank", "error", err)
	}

	// Background backfill: embed behaviors that don't yet have vectors
	if s.embedder != nil && s.embedder.Available() {
		if ng, ok := s.store.(vectorsearch.NodeGetter); ok {
			s.runBackground("embedding-backfill", func() {
				n, err := s.embedder.BackfillMissing(context.Background(), ng)
				if err != nil {
					s.logger.Warn("embedding backfill failed", "error", err)
				} else if n > 0 {
					s.logger.Info("backfilled embeddings", "count", n)
				}
			})
		}
	}

	return s, nil
}

// initVectorIndex creates the vector index (LanceDB or BruteForce fallback)
// and populates it from stored embeddings.
func (s *Server) initVectorIndex(graphStore *store.MultiGraphStore, vectorDir string) vectorindex.VectorIndex {
	allEmb, loadErr := graphStore.GetAllEmbeddings(context.Background())
	if loadErr != nil {
		s.logger.Warn("failed to load embeddings for index", "error", loadErr)
	}

	// Default matches nomic-embed-text-v1.5 (768-dim), the only supported model.
	// On fresh installs (no embeddings yet), this creates the table with 768 dims.
	// If a future model has different dims, the first Add will fail with a clear
	// dimension mismatch error, and on restart the schema validation will catch the
	// mismatch and fall back to BruteForce until the user deletes .floop/vectors/.
	dims := 768
	for _, emb := range allEmb {
		if len(emb.Embedding) > 0 {
			dims = len(emb.Embedding)
			break
		}
	}

	idx, err := vectorindex.NewLanceDBIndex(vectorindex.LanceDBConfig{
		Dir:  vectorDir,
		Dims: dims,
	})
	if err != nil {
		s.logger.Warn("LanceDB init failed, falling back to brute-force", "error", err)
		bfIdx := vectorindex.NewBruteForceIndex()
		if loadErr == nil {
			var addErrs int
			for _, emb := range allEmb {
				if err := bfIdx.Add(context.Background(), emb.BehaviorID, emb.Embedding); err != nil {
					addErrs++
				}
			}
			if addErrs > 0 {
				s.logger.Warn("some embeddings failed to load into brute-force index", "errors", addErrs, "total", len(allEmb))
			}
		}
		return bfIdx
	}

	// Sync SQLite embeddings to LanceDB.
	// - Empty table (first run or after wipe): bulk add all embeddings.
	// - Count mismatch: some vectors are missing — re-add all. The upsert
	//   (delete+add) creates tombstones for existing entries, but this only
	//   happens on recovery, not on every restart.
	// - Counts match: skip sync entirely (no tombstone churn).
	if loadErr == nil {
		lanceCount := idx.Len()
		sqliteCount := len(allEmb)
		if lanceCount < sqliteCount {
			var addErrs int
			for _, emb := range allEmb {
				if err := idx.Add(context.Background(), emb.BehaviorID, emb.Embedding); err != nil {
					addErrs++
				}
			}
			if addErrs > 0 {
				s.logger.Warn("some embeddings failed to load into vector index",
					"errors", addErrs, "total", sqliteCount)
			}
			if lanceCount > 0 {
				s.logger.Info("recovered missing vectors from SQLite",
					"before", lanceCount, "after", idx.Len(), "sqlite_total", sqliteCount)
			}
		}
	}
	return idx
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
			s.logger.Warn("debounced PageRank refresh failed", "error", err)
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
		s.logger.Warn("background worker pool full, skipping task", "task", name)
	}
}

// Run starts the MCP server over stdio transport.
// This blocks until the client disconnects or the context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle OS signals (platform-specific: see signal_unix.go / signal_windows.go)
	sigChan := make(chan os.Signal, 1)
	notifySignals(sigChan)

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
		return &backup.CountPolicy{MaxCount: constants.MaxBackupRotation}
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

		if s.vectorIndex != nil {
			if err := s.vectorIndex.Close(); err != nil {
				s.logger.Warn("failed to close vector index", "error", err)
			}
		}

		if c, ok := s.llmClient.(llm.Closer); ok {
			c.Close()
		}

		closeErr = s.store.Close()
	})
	return closeErr
}
