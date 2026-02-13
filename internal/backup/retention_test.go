package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCountPolicy_KeepsN(t *testing.T) {
	now := time.Now()
	backups := []BackupInfo{
		{Path: "/b/backup-5.json.gz", CreatedAt: now, Size: 100},
		{Path: "/b/backup-4.json.gz", CreatedAt: now.Add(-1 * time.Hour), Size: 100},
		{Path: "/b/backup-3.json.gz", CreatedAt: now.Add(-2 * time.Hour), Size: 100},
		{Path: "/b/backup-2.json.gz", CreatedAt: now.Add(-3 * time.Hour), Size: 100},
		{Path: "/b/backup-1.json.gz", CreatedAt: now.Add(-4 * time.Hour), Size: 100},
	}

	policy := &CountPolicy{MaxCount: 3}
	keep := policy.Apply(backups)

	if len(keep) != 3 {
		t.Errorf("CountPolicy.Apply() kept %d, want 3", len(keep))
	}
	// Should keep the 3 newest
	if keep[0].Path != "/b/backup-5.json.gz" {
		t.Errorf("first kept = %s, want backup-5", keep[0].Path)
	}
	if keep[2].Path != "/b/backup-3.json.gz" {
		t.Errorf("last kept = %s, want backup-3", keep[2].Path)
	}
}

func TestCountPolicy_FewerThanN(t *testing.T) {
	backups := []BackupInfo{
		{Path: "/b/a.json.gz", CreatedAt: time.Now(), Size: 100},
	}
	policy := &CountPolicy{MaxCount: 5}
	keep := policy.Apply(backups)
	if len(keep) != 1 {
		t.Errorf("CountPolicy.Apply() kept %d, want 1", len(keep))
	}
}

func TestAgePolicy_RemovesOld(t *testing.T) {
	now := time.Now()
	backups := []BackupInfo{
		{Path: "/b/new.json.gz", CreatedAt: now.Add(-1 * time.Hour), Size: 100},
		{Path: "/b/recent.json.gz", CreatedAt: now.Add(-12 * time.Hour), Size: 100},
		{Path: "/b/old.json.gz", CreatedAt: now.Add(-48 * time.Hour), Size: 100},
		{Path: "/b/ancient.json.gz", CreatedAt: now.Add(-720 * time.Hour), Size: 100},
	}

	policy := &AgePolicy{MaxAge: 24 * time.Hour}
	keep := policy.Apply(backups)

	if len(keep) != 2 {
		t.Errorf("AgePolicy.Apply() kept %d, want 2", len(keep))
	}
}

func TestSizePolicy_FitsUnderLimit(t *testing.T) {
	now := time.Now()
	backups := []BackupInfo{
		{Path: "/b/1.json.gz", CreatedAt: now, Size: 500},
		{Path: "/b/2.json.gz", CreatedAt: now.Add(-1 * time.Hour), Size: 500},
		{Path: "/b/3.json.gz", CreatedAt: now.Add(-2 * time.Hour), Size: 500},
		{Path: "/b/4.json.gz", CreatedAt: now.Add(-3 * time.Hour), Size: 500},
	}

	policy := &SizePolicy{MaxTotalBytes: 1200}
	keep := policy.Apply(backups)

	if len(keep) != 2 {
		t.Errorf("SizePolicy.Apply() kept %d, want 2", len(keep))
	}
	// Should keep newest first
	if keep[0].Path != "/b/1.json.gz" {
		t.Errorf("first kept = %s, want 1.json.gz", keep[0].Path)
	}
}

func TestCompositePolicy_UnionKeep(t *testing.T) {
	now := time.Now()
	backups := []BackupInfo{
		{Path: "/b/1.json.gz", CreatedAt: now, Size: 100},
		{Path: "/b/2.json.gz", CreatedAt: now.Add(-1 * time.Hour), Size: 100},
		{Path: "/b/3.json.gz", CreatedAt: now.Add(-48 * time.Hour), Size: 100}, // old but within count
		{Path: "/b/4.json.gz", CreatedAt: now.Add(-72 * time.Hour), Size: 100}, // old and over count
	}

	// CountPolicy keeps 3, AgePolicy keeps 2 (24h)
	// Union: 1, 2, 3 (count) + 1, 2 (age) = 1, 2, 3
	composite := &CompositePolicy{
		Policies: []RetentionPolicy{
			&CountPolicy{MaxCount: 3},
			&AgePolicy{MaxAge: 24 * time.Hour},
		},
	}
	keep := composite.Apply(backups)

	if len(keep) != 3 {
		t.Errorf("CompositePolicy.Apply() kept %d, want 3", len(keep))
	}
}

func TestListBackups_MixedFormats(t *testing.T) {
	dir := t.TempDir()

	// Create mixed V1 and V2 files
	files := map[string]string{
		"floop-backup-20260201-120000.json":    `{"version":1,"created_at":"2026-02-01T12:00:00Z","nodes":[],"edges":[]}`,
		"floop-backup-20260202-120000.json.gz": "fake", // will be detected by name only
		"floop-backup-20260203-120000.json":    `{"version":1,"created_at":"2026-02-03T12:00:00Z","nodes":[],"edges":[]}`,
		"not-a-backup.txt":                     "ignore this",
	}
	for name, content := range files {
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0600)
	}

	backups, err := ListBackups(dir)
	if err != nil {
		t.Fatalf("ListBackups() error = %v", err)
	}

	if len(backups) != 3 {
		t.Errorf("ListBackups() found %d, want 3", len(backups))
	}

	// Should be sorted newest first
	if len(backups) > 0 && backups[0].Path != filepath.Join(dir, "floop-backup-20260203-120000.json") {
		t.Errorf("first backup = %s, want floop-backup-20260203", filepath.Base(backups[0].Path))
	}
}

func TestApplyRetention_DeletesCorrectFiles(t *testing.T) {
	dir := t.TempDir()

	// Create 5 backup files
	for i := 1; i <= 5; i++ {
		name := filepath.Join(dir, "floop-backup-2026020"+string(rune('0'+i))+"-120000.json.gz")
		os.WriteFile(name, []byte("data"), 0600)
	}

	policy := &CountPolicy{MaxCount: 2}
	deleted, err := ApplyRetention(dir, policy)
	if err != nil {
		t.Fatalf("ApplyRetention() error = %v", err)
	}

	if len(deleted) != 3 {
		t.Errorf("deleted %d files, want 3", len(deleted))
	}

	// Verify only 2 remain
	remaining, _ := ListBackups(dir)
	if len(remaining) != 2 {
		t.Errorf("remaining = %d, want 2", len(remaining))
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"30d", 30 * 24 * time.Hour, false},
		{"2w", 14 * 24 * time.Hour, false},
		{"720h", 720 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"", 0, true},
		{"abc", 0, true},
		{"0d", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDuration(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseDuration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		{"100MB", 100 * 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"500KB", 500 * 1024, false},
		{"1024B", 1024, false},
		{"", 0, true},
		{"abc", 0, true},
		{"0MB", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseSize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
