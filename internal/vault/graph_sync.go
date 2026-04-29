package vault

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nvandessel/floop/internal/backup"
	"github.com/nvandessel/floop/internal/store"
)

// GraphSyncer syncs graph data (V2 backup + corrections.jsonl) via S3.
type GraphSyncer struct {
	s3         S3Operations
	machineID  string
	encryption *VaultEncryptionConfig
}

// GraphSyncResult contains the results of a graph sync operation.
type GraphSyncResult struct {
	NodeCount       int
	EdgeCount       int
	CorrectionsSize int64
}

// NewGraphSyncer creates a GraphSyncer.
func NewGraphSyncer(s3 S3Operations, machineID string, enc *VaultEncryptionConfig) *GraphSyncer {
	gs := &GraphSyncer{
		s3:        s3,
		machineID: machineID,
	}
	if enc != nil && enc.Enabled {
		gs.encryption = enc
	}
	return gs
}

// Push exports the graph as a V2 backup and uploads it along with corrections.jsonl.
func (g *GraphSyncer) Push(ctx context.Context, graphStore store.GraphStore, correctionsPath string, floopVersion string) (*GraphSyncResult, error) {
	result := &GraphSyncResult{}

	// Export V2 backup
	tmpDir, err := os.MkdirTemp("", "floop-vault-push-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	backupPath := filepath.Join(tmpDir, "floop-backup.json.gz")
	bf, err := backup.BackupWithOptions(ctx, graphStore, backupPath, backup.BackupOptions{
		Compress:     true,
		FloopVersion: floopVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("creating backup: %w", err)
	}
	result.NodeCount = len(bf.Nodes)
	result.EdgeCount = len(bf.Edges)

	uploadPath := backupPath
	if g.encryption != nil {
		encPath := backupPath + ".age"
		if err := EncryptFile(g.encryption.Recipient, backupPath, encPath); err != nil {
			return nil, fmt.Errorf("encrypting backup: %w", err)
		}
		uploadPath = encPath
	}

	// Upload backup
	backupKey := fmt.Sprintf("machines/%s/graph/floop-backup.json.gz", g.machineID)
	if err := g.uploadFile(ctx, backupKey, uploadPath); err != nil {
		return nil, fmt.Errorf("uploading backup: %w", err)
	}

	// Upload corrections.jsonl if it exists
	if correctionsPath != "" {
		if info, statErr := os.Stat(correctionsPath); statErr == nil {
			result.CorrectionsSize = info.Size()

			corrUploadPath := correctionsPath
			if g.encryption != nil {
				encCorrPath := filepath.Join(tmpDir, "corrections.jsonl.age")
				if err := EncryptFile(g.encryption.Recipient, correctionsPath, encCorrPath); err != nil {
					return nil, fmt.Errorf("encrypting corrections: %w", err)
				}
				corrUploadPath = encCorrPath
			}

			corrKey := fmt.Sprintf("machines/%s/graph/corrections.jsonl", g.machineID)
			if err := g.uploadFile(ctx, corrKey, corrUploadPath); err != nil {
				return nil, fmt.Errorf("uploading corrections: %w", err)
			}
		}
	}

	return result, nil
}

// Pull downloads the V2 backup and corrections from another machine and restores them.
func (g *GraphSyncer) Pull(ctx context.Context, graphStore store.GraphStore, fromMachineID string, correctionsPath string) (*GraphSyncResult, error) {
	result := &GraphSyncResult{}

	tmpDir, err := os.MkdirTemp("", "floop-vault-pull-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Download backup
	backupKey := fmt.Sprintf("machines/%s/graph/floop-backup.json.gz", fromMachineID)
	downloadPath := filepath.Join(tmpDir, "floop-backup.json.gz")

	if err := g.downloadFile(ctx, backupKey, downloadPath); err != nil {
		return nil, fmt.Errorf("downloading backup: %w", err)
	}

	restorePath := downloadPath
	if g.encryption != nil {
		decPath := filepath.Join(tmpDir, "floop-backup-decrypted.json.gz")
		if err := DecryptFile(g.encryption.IdentityFile, downloadPath, decPath); err != nil {
			return nil, fmt.Errorf("decrypting backup: %w", err)
		}
		restorePath = decPath
	}

	// Restore with merge semantics
	restoreResult, err := backup.Restore(ctx, graphStore, restorePath, backup.RestoreMerge)
	if err != nil {
		return nil, fmt.Errorf("restoring backup: %w", err)
	}
	result.NodeCount = restoreResult.NodesRestored
	result.EdgeCount = restoreResult.EdgesRestored

	// Download and merge corrections
	if correctionsPath != "" {
		corrKey := fmt.Sprintf("machines/%s/graph/corrections.jsonl", fromMachineID)
		corrDownloadPath := filepath.Join(tmpDir, "corrections.jsonl")

		if err := g.downloadFile(ctx, corrKey, corrDownloadPath); err == nil {
			corrRestorePath := corrDownloadPath
			if g.encryption != nil {
				decCorrPath := filepath.Join(tmpDir, "corrections-decrypted.jsonl")
				if decErr := DecryptFile(g.encryption.IdentityFile, corrDownloadPath, decCorrPath); decErr != nil {
					return nil, fmt.Errorf("decrypting corrections: %w", decErr)
				}
				corrRestorePath = decCorrPath
			}
			size, mergeErr := mergeCorrections(corrRestorePath, correctionsPath)
			if mergeErr != nil {
				return nil, fmt.Errorf("merging corrections: %w", mergeErr)
			}
			result.CorrectionsSize = size
		}
		// If corrections don't exist remotely, that's fine — skip silently
	}

	return result, nil
}

// uploadFile opens a file and uploads it via S3.
func (g *GraphSyncer) uploadFile(ctx context.Context, key, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	return g.s3.Upload(ctx, key, f, info.Size())
}

// downloadFile downloads from S3 and writes to a local file.
func (g *GraphSyncer) downloadFile(ctx context.Context, key, path string) error {
	rc, err := g.s3.Download(ctx, key)
	if err != nil {
		return err
	}
	defer rc.Close()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", path, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, rc); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// mergeCorrections appends lines from remotePath that don't exist in localPath.
// Uses content-based deduplication to handle cross-machine pulls safely.
func mergeCorrections(remotePath, localPath string) (int64, error) {
	// Build set of existing local lines for dedup
	existing := make(map[string]struct{})
	if f, err := os.Open(localPath); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			existing[scanner.Text()] = struct{}{}
		}
		f.Close()
	}

	// Read remote lines, collect those not already in local
	remoteFile, err := os.Open(remotePath)
	if err != nil {
		return 0, fmt.Errorf("opening remote corrections: %w", err)
	}
	defer remoteFile.Close()

	var newLines []string
	scanner := bufio.NewScanner(remoteFile)
	for scanner.Scan() {
		line := scanner.Text()
		if _, ok := existing[line]; !ok {
			newLines = append(newLines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scanning remote corrections: %w", err)
	}

	if len(newLines) == 0 {
		info, _ := os.Stat(localPath)
		if info != nil {
			return info.Size(), nil
		}
		return 0, nil
	}

	// Append new lines to local file
	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return 0, fmt.Errorf("creating corrections directory: %w", err)
	}

	f, err := os.OpenFile(localPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return 0, fmt.Errorf("opening local corrections for append: %w", err)
	}
	defer f.Close()

	for _, line := range newLines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			return 0, fmt.Errorf("appending correction: %w", err)
		}
	}

	info, _ := os.Stat(localPath)
	if info != nil {
		return info.Size(), nil
	}
	return 0, nil
}
