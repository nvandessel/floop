package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lancedb/lancedb-go/pkg/contracts"

	"github.com/nvandessel/floop/internal/store"
)

const (
	sentinelKey      = "_floop_vault_initialized"
	schemaVersionKey = "_schema_version"
	syncStateKey     = "_sync_state.json"

	// CurrentSchemaVersion is the vault schema version.
	CurrentSchemaVersion = 1
)

// PushResult contains the results of a push operation.
type PushResult struct {
	Vectors  VectorSyncResult
	Graph    GraphSyncResult
	Duration time.Duration
}

// PullResult contains the results of a pull operation.
type PullResult struct {
	Vectors  VectorSyncResult
	Graph    GraphSyncResult
	Duration time.Duration
}

// SyncResult contains the results of a bidirectional sync.
type SyncResult struct {
	Pulled PullResult
	Pushed PushResult
}

// StatusResult contains vault status information.
type StatusResult struct {
	Configured       bool      `json:"configured"`
	URI              string    `json:"uri"`
	MachineID        string    `json:"machine_id"`
	LastPush         time.Time `json:"last_push,omitempty"`
	LastPull         time.Time `json:"last_pull,omitempty"`
	LocalVectorRows  int       `json:"local_vector_rows"`
	RemoteVectorRows int       `json:"remote_vector_rows"`
	LocalNodeCount   int       `json:"local_node_count"`
	Status           string    `json:"status"`
	Staleness        string    `json:"staleness"`
}

// PushOptions controls push behavior.
type PushOptions struct {
	Force  bool
	DryRun bool
	Scope  string // "global", "local", "both"
}

// PullOptions controls pull behavior.
type PullOptions struct {
	Force       bool
	DryRun      bool
	FromMachine string
	Scope       string
	Root        string
}

// SyncOptions controls sync behavior.
type SyncOptions struct {
	DryRun bool
	Scope  string
}

// VerifyOptions controls verify behavior.
type VerifyOptions struct {
	RemoteOnly bool
	LocalOnly  bool
}

// VerifyResult contains verification results.
type VerifyResult struct {
	OK               bool     `json:"ok"`
	Issues           []string `json:"issues,omitempty"`
	LocalVectorRows  int      `json:"local_vector_rows"`
	RemoteVectorRows int      `json:"remote_vector_rows"`
}

// VaultService orchestrates all vault sync operations.
type VaultService struct {
	cfg          *VaultConfig
	s3           *S3Client
	vectorDir    string
	floopVersion string
	statePath    string
	dims         int
}

// NewVaultService creates a VaultService.
func NewVaultService(cfg *VaultConfig, vectorDir string, floopVersion string, dims int) (*VaultService, error) {
	if !cfg.Configured() {
		return nil, fmt.Errorf("vault not configured — run 'floop vault init'")
	}

	s3, err := NewS3Client(cfg.Remote)
	if err != nil {
		return nil, fmt.Errorf("creating S3 client: %w", err)
	}

	homeDir, _ := os.UserHomeDir()
	statePath := filepath.Join(homeDir, ".floop", "vault-state.json")

	return &VaultService{
		cfg:          cfg,
		s3:           s3,
		vectorDir:    vectorDir,
		floopVersion: floopVersion,
		statePath:    statePath,
		dims:         dims,
	}, nil
}

// Init validates config, tests connectivity, and writes sentinel.
func (v *VaultService) Init(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, v.cfg.SyncTimeout())
	defer cancel()

	// Test connectivity by writing sentinel
	if err := v.s3.PutJSON(ctx, sentinelKey, map[string]interface{}{
		"initialized_at": time.Now().UTC(),
		"machine_id":     v.cfg.ResolveMachineID(),
	}); err != nil {
		return fmt.Errorf("cannot connect to remote: %w", err)
	}

	// Write schema version
	if err := v.s3.PutJSON(ctx, schemaVersionKey, map[string]interface{}{
		"version": CurrentSchemaVersion,
	}); err != nil {
		return fmt.Errorf("writing schema version: %w", err)
	}

	return nil
}

// Push pushes local state to remote.
func (v *VaultService) Push(ctx context.Context, graphStore store.GraphStore, root string, opts PushOptions) (*PushResult, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, v.cfg.SyncTimeout())
	defer cancel()

	machineID := v.cfg.ResolveMachineID()
	result := &PushResult{}

	scope := normalizeScope(opts.Scope)

	if opts.DryRun {
		return v.dryRunPush(ctx, graphStore, root, scope)
	}

	if scope == "global" || scope == "both" {
		if err := v.pushScope(ctx, graphStore, machineID, v.vectorDir, v.globalCorrectionsPath(), result); err != nil {
			return nil, err
		}
	}

	if (scope == "local" || scope == "both") && root != "" {
		localVectorDir := filepath.Join(root, ".floop", "vectors")
		localCorrectionsPath := filepath.Join(root, ".floop", "corrections.jsonl")
		if err := v.pushScope(ctx, graphStore, machineID, localVectorDir, localCorrectionsPath, result); err != nil {
			return nil, err
		}
	}

	// Update state
	state, err := LoadState(v.statePath)
	if err != nil || state == nil {
		state = &SyncState{}
	}
	state.MachineID = machineID
	state.LastPush = time.Now().UTC()
	state.LocalVectorRows = result.Vectors.RowsPushed
	state.PushCount++
	state.PendingPush = false
	if err := SaveState(v.statePath, state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: saving vault state: %v\n", err)
	}

	// Write remote sync state (best-effort, don't fail the push)
	if err := v.s3.PutJSON(ctx, fmt.Sprintf("machines/%s/%s", machineID, syncStateKey), state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: writing remote sync state: %v\n", err)
	}

	result.Duration = time.Since(start)
	return result, nil
}

// Pull pulls remote state to local.
func (v *VaultService) Pull(ctx context.Context, graphStore store.GraphStore, opts PullOptions) (*PullResult, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, v.cfg.SyncTimeout())
	defer cancel()

	machineID := v.cfg.ResolveMachineID()
	fromMachine := opts.FromMachine
	if fromMachine == "" {
		fromMachine = machineID
	}

	result := &PullResult{}

	if opts.DryRun {
		return v.dryRunPull(ctx, graphStore, fromMachine, opts.Scope, opts.Root)
	}

	scope := normalizeScope(opts.Scope)

	if scope == "global" || scope == "both" {
		if err := v.pullScope(ctx, graphStore, fromMachine, v.vectorDir, v.globalCorrectionsPath(), result); err != nil {
			return nil, err
		}
	}

	if (scope == "local" || scope == "both") && opts.Root != "" {
		localVectorDir := filepath.Join(opts.Root, ".floop", "vectors")
		localCorrectionsPath := filepath.Join(opts.Root, ".floop", "corrections.jsonl")
		if err := v.pullScope(ctx, graphStore, fromMachine, localVectorDir, localCorrectionsPath, result); err != nil {
			return nil, err
		}
	}

	// Update state
	state, err := LoadState(v.statePath)
	if err != nil || state == nil {
		state = &SyncState{}
	}
	state.MachineID = machineID
	state.LastPull = time.Now().UTC()
	state.PullCount++
	if err := SaveState(v.statePath, state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: saving vault state: %v\n", err)
	}

	result.Duration = time.Since(start)
	return result, nil
}

// Sync performs bidirectional sync (pull first, then push).
func (v *VaultService) Sync(ctx context.Context, graphStore store.GraphStore, root string, opts SyncOptions) (*SyncResult, error) {
	result := &SyncResult{}

	pullResult, err := v.Pull(ctx, graphStore, PullOptions{
		DryRun: opts.DryRun,
		Scope:  opts.Scope,
		Root:   root,
	})
	if err != nil {
		return nil, fmt.Errorf("pull phase: %w", err)
	}
	result.Pulled = *pullResult

	pushResult, err := v.Push(ctx, graphStore, root, PushOptions{
		DryRun: opts.DryRun,
		Scope:  opts.Scope,
	})
	if err != nil {
		return nil, fmt.Errorf("push phase: %w", err)
	}
	result.Pushed = *pushResult

	return result, nil
}

// Status returns the current vault status.
func (v *VaultService) Status(ctx context.Context, graphStore store.GraphStore) (*StatusResult, error) {
	machineID := v.cfg.ResolveMachineID()
	state, err := LoadState(v.statePath)
	if err != nil || state == nil {
		state = &SyncState{}
	}

	sr := &StatusResult{
		Configured: true,
		URI:        v.cfg.Remote.URI,
		MachineID:  machineID,
		LastPush:   state.LastPush,
		LastPull:   state.LastPull,
		Staleness:  state.Staleness(time.Now()),
	}

	// Count local nodes
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{})
	if err == nil {
		sr.LocalNodeCount = len(nodes)
	}

	// Count local vector rows
	remoteURI := v.remoteVectorURI(machineID)
	opts := v.connectionOptions()
	syncer := NewVectorSyncer(v.vectorDir, remoteURI, opts, v.dims)

	localCount, err := syncer.LocalRowCount(ctx)
	if err == nil {
		sr.LocalVectorRows = localCount
	}

	remoteCount, err := syncer.RemoteRowCount(ctx)
	if err == nil {
		sr.RemoteVectorRows = remoteCount
	}

	// Determine status
	switch {
	case sr.LocalVectorRows > sr.RemoteVectorRows:
		sr.Status = "local_ahead"
	case sr.RemoteVectorRows > sr.LocalVectorRows:
		sr.Status = "remote_ahead"
	default:
		sr.Status = "in_sync"
	}

	return sr, nil
}

// Verify checks the integrity of local and/or remote vault data.
func (v *VaultService) Verify(ctx context.Context, graphStore store.GraphStore, opts VerifyOptions) (*VerifyResult, error) {
	ctx, cancel := context.WithTimeout(ctx, v.cfg.SyncTimeout())
	defer cancel()

	machineID := v.cfg.ResolveMachineID()
	result := &VerifyResult{
		OK:               true,
		LocalVectorRows:  -1,
		RemoteVectorRows: -1,
	}

	remoteURI := v.remoteVectorURI(machineID)
	connOpts := v.connectionOptions()
	syncer := NewVectorSyncer(v.vectorDir, remoteURI, connOpts, v.dims)

	if !opts.RemoteOnly {
		localCount, err := syncer.LocalRowCount(ctx)
		if err != nil {
			result.OK = false
			result.Issues = append(result.Issues, fmt.Sprintf("local vectors unreadable: %v", err))
		} else {
			result.LocalVectorRows = localCount
		}

		nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{})
		if err != nil {
			result.OK = false
			result.Issues = append(result.Issues, fmt.Sprintf("local graph unreadable: %v", err))
		} else if len(nodes) == 0 {
			result.Issues = append(result.Issues, "local graph is empty")
		}
	}

	if !opts.LocalOnly {
		exists, err := v.s3.Exists(ctx, sentinelKey)
		if err != nil {
			result.OK = false
			result.Issues = append(result.Issues, fmt.Sprintf("cannot reach remote: %v", err))
		} else if !exists {
			result.OK = false
			result.Issues = append(result.Issues, "remote vault not initialized (sentinel missing)")
		}

		remoteCount, err := syncer.RemoteRowCount(ctx)
		if err != nil {
			result.OK = false
			result.Issues = append(result.Issues, fmt.Sprintf("remote vectors unreadable: %v", err))
		} else {
			result.RemoteVectorRows = remoteCount
		}
	}

	return result, nil
}

// pushScope pushes vectors and graph for a single scope.
func (v *VaultService) pushScope(ctx context.Context, graphStore store.GraphStore, machineID, vectorDir, correctionsPath string, result *PushResult) error {
	// Push vectors
	remoteURI := v.remoteVectorURI(machineID)
	opts := v.connectionOptions()
	vectorSyncer := NewVectorSyncer(vectorDir, remoteURI, opts, v.dims)

	vecResult, err := vectorSyncer.Push(ctx)
	if err != nil {
		return fmt.Errorf("pushing vectors: %w", err)
	}
	result.Vectors.RowsPushed += vecResult.RowsPushed
	result.Vectors.RowsSkipped += vecResult.RowsSkipped

	// Push graph
	graphSyncer := NewGraphSyncer(v.s3, machineID, &v.cfg.Encryption)
	graphResult, err := graphSyncer.Push(ctx, graphStore, correctionsPath, v.floopVersion)
	if err != nil {
		return fmt.Errorf("pushing graph: %w", err)
	}
	result.Graph.NodeCount += graphResult.NodeCount
	result.Graph.EdgeCount += graphResult.EdgeCount
	result.Graph.CorrectionsSize += graphResult.CorrectionsSize

	return nil
}

// pullScope pulls vectors and graph for a single scope.
func (v *VaultService) pullScope(ctx context.Context, graphStore store.GraphStore, fromMachine, vectorDir, correctionsPath string, result *PullResult) error {
	// Pull vectors
	remoteURI := v.remoteVectorURI(fromMachine)
	opts := v.connectionOptions()
	vectorSyncer := NewVectorSyncer(vectorDir, remoteURI, opts, v.dims)

	vecResult, err := vectorSyncer.Pull(ctx)
	if err != nil {
		return fmt.Errorf("pulling vectors: %w", err)
	}
	result.Vectors.RowsPulled += vecResult.RowsPushed // syncRows reports pushed from source's perspective
	result.Vectors.RowsSkipped += vecResult.RowsSkipped

	// Pull graph
	graphSyncer := NewGraphSyncer(v.s3, v.cfg.ResolveMachineID(), &v.cfg.Encryption)
	graphResult, err := graphSyncer.Pull(ctx, graphStore, fromMachine, correctionsPath)
	if err != nil {
		return fmt.Errorf("pulling graph: %w", err)
	}
	result.Graph.NodeCount += graphResult.NodeCount
	result.Graph.EdgeCount += graphResult.EdgeCount
	result.Graph.CorrectionsSize += graphResult.CorrectionsSize

	return nil
}

// dryRunPush returns what would be pushed without pushing.
func (v *VaultService) dryRunPush(ctx context.Context, graphStore store.GraphStore, root, scope string) (*PushResult, error) {
	result := &PushResult{}
	machineID := v.cfg.ResolveMachineID()
	remoteURI := v.remoteVectorURI(machineID)
	connOpts := v.connectionOptions()

	if scope == "global" || scope == "both" {
		syncer := NewVectorSyncer(v.vectorDir, remoteURI, connOpts, v.dims)
		localCount, err := syncer.LocalRowCount(ctx)
		if err == nil {
			result.Vectors.RowsPushed += localCount
		}
	}

	if (scope == "local" || scope == "both") && root != "" {
		localVectorDir := filepath.Join(root, ".floop", "vectors")
		syncer := NewVectorSyncer(localVectorDir, remoteURI, connOpts, v.dims)
		localCount, err := syncer.LocalRowCount(ctx)
		if err == nil {
			result.Vectors.RowsPushed += localCount
		}
	}

	// Count graph nodes
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{})
	if err == nil {
		result.Graph.NodeCount = len(nodes)
	}

	return result, nil
}

// dryRunPull returns what would be pulled without pulling.
func (v *VaultService) dryRunPull(ctx context.Context, graphStore store.GraphStore, fromMachine, scope, root string) (*PullResult, error) {
	result := &PullResult{}

	remoteURI := v.remoteVectorURI(fromMachine)
	connOpts := v.connectionOptions()

	if scope == "global" || scope == "both" {
		syncer := NewVectorSyncer(v.vectorDir, remoteURI, connOpts, v.dims)
		remoteCount, err := syncer.RemoteRowCount(ctx)
		if err == nil {
			result.Vectors.RowsPulled += remoteCount
		}
	}

	if (scope == "local" || scope == "both") && root != "" {
		localVectorDir := filepath.Join(root, ".floop", "vectors")
		syncer := NewVectorSyncer(localVectorDir, remoteURI, connOpts, v.dims)
		remoteCount, err := syncer.RemoteRowCount(ctx)
		if err == nil {
			result.Vectors.RowsPulled += remoteCount
		}
	}

	return result, nil
}

// remoteVectorURI builds the S3 URI for the vectors table.
func (v *VaultService) remoteVectorURI(machineID string) string {
	return fmt.Sprintf("%s/machines/%s/vectors", v.cfg.Remote.URI, machineID)
}

// connectionOptions builds lancedb-go connection options from config.
func (v *VaultService) connectionOptions() *contracts.ConnectionOptions {
	storageOpts := v.cfg.Remote.StorageOptions()
	return &contracts.ConnectionOptions{
		StorageOptions: storageOpts,
	}
}

// globalCorrectionsPath returns the path to the global corrections.jsonl.
func (v *VaultService) globalCorrectionsPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".floop", "corrections.jsonl")
}

// normalizeScope normalizes the scope string, defaulting to "global".
func normalizeScope(scope string) string {
	scope = strings.ToLower(strings.TrimSpace(scope))
	switch scope {
	case "global", "local", "both":
		return scope
	default:
		return "global"
	}
}
