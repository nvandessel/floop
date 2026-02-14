package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// BackupInfo holds metadata for retention decisions.
type BackupInfo struct {
	Path      string
	Size      int64
	CreatedAt time.Time
	Version   int
}

// RetentionPolicy decides which backups to keep.
type RetentionPolicy interface {
	Apply(backups []BackupInfo) (keep []BackupInfo)
}

// CountPolicy keeps the N most recent backups.
type CountPolicy struct {
	MaxCount int
}

// Apply keeps the first MaxCount backups (assumed sorted newest-first).
func (p *CountPolicy) Apply(backups []BackupInfo) []BackupInfo {
	if len(backups) <= p.MaxCount {
		return backups
	}
	return backups[:p.MaxCount]
}

// AgePolicy keeps backups newer than MaxAge.
type AgePolicy struct {
	MaxAge time.Duration
}

// Apply keeps backups whose CreatedAt is within MaxAge of now.
func (p *AgePolicy) Apply(backups []BackupInfo) []BackupInfo {
	cutoff := time.Now().Add(-p.MaxAge)
	var keep []BackupInfo
	for _, b := range backups {
		if b.CreatedAt.After(cutoff) {
			keep = append(keep, b)
		}
	}
	return keep
}

// SizePolicy keeps backups until total size exceeds MaxTotalBytes.
type SizePolicy struct {
	MaxTotalBytes int64
}

// Apply keeps backups (newest-first) until adding the next would exceed the limit.
func (p *SizePolicy) Apply(backups []BackupInfo) []BackupInfo {
	var keep []BackupInfo
	var total int64
	for _, b := range backups {
		if total+b.Size > p.MaxTotalBytes && len(keep) > 0 {
			break
		}
		keep = append(keep, b)
		total += b.Size
	}
	return keep
}

// CompositePolicy keeps a backup if ANY sub-policy wants it (union).
type CompositePolicy struct {
	Policies []RetentionPolicy
}

// Apply returns the union of backups kept by any sub-policy.
func (p *CompositePolicy) Apply(backups []BackupInfo) []BackupInfo {
	kept := make(map[string]bool)
	for _, policy := range p.Policies {
		for _, b := range policy.Apply(backups) {
			kept[b.Path] = true
		}
	}

	var result []BackupInfo
	for _, b := range backups {
		if kept[b.Path] {
			result = append(result, b)
		}
	}
	return result
}

// ListBackups scans dir for floop-backup-* files and returns them sorted newest-first.
func ListBackups(dir string) ([]BackupInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading backup directory: %w", err)
	}

	var backups []BackupInfo
	for _, e := range entries {
		if e.IsDir() || !isBackupFile(e.Name()) {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		bi := BackupInfo{
			Path:      filepath.Join(dir, e.Name()),
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
		}

		// Try to detect version from file content
		version, verErr := DetectFormat(bi.Path)
		if verErr == nil {
			bi.Version = version
		}

		backups = append(backups, bi)
	}

	// Sort newest first by filename (timestamp is embedded)
	sort.Slice(backups, func(i, j int) bool {
		return filepath.Base(backups[i].Path) > filepath.Base(backups[j].Path)
	})

	return backups, nil
}

// ApplyRetention deletes backups not kept by the policy.
func ApplyRetention(dir string, policy RetentionPolicy) (deleted []string, err error) {
	backups, err := ListBackups(dir)
	if err != nil {
		return nil, err
	}

	keep := policy.Apply(backups)
	keepSet := make(map[string]bool, len(keep))
	for _, b := range keep {
		keepSet[b.Path] = true
	}

	for _, b := range backups {
		if !keepSet[b.Path] {
			if err := os.Remove(b.Path); err != nil {
				return deleted, fmt.Errorf("removing %s: %w", filepath.Base(b.Path), err)
			}
			deleted = append(deleted, b.Path)
		}
	}

	return deleted, nil
}

// ParseDuration parses duration strings like "30d", "2w", "720h".
func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration string")
	}

	// Try standard Go duration first (e.g., "720h")
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Parse custom suffixes: d (days), w (weeks)
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}

	suffix := s[len(s)-1]
	numStr := s[:len(s)-1]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}

	switch suffix {
	case 'd':
		return time.Duration(num) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(num) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown duration suffix %q in %q", string(suffix), s)
	}
}

// ParseSize parses size strings like "100MB", "1GB", "500KB" into bytes.
func ParseSize(s string) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty size string")
	}

	s = strings.TrimSpace(s)

	// Check longer suffixes first to avoid "MB" matching "B"
	type sizeSuffix struct {
		suffix     string
		multiplier int64
	}
	suffixes := []sizeSuffix{
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
		{"B", 1},
	}

	for _, ss := range suffixes {
		if strings.HasSuffix(s, ss.suffix) {
			numStr := strings.TrimSuffix(s, ss.suffix)
			num, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid size: %q", s)
			}
			return num * ss.multiplier, nil
		}
	}

	return 0, fmt.Errorf("invalid size: %q (expected suffix: B, KB, MB, GB)", s)
}
