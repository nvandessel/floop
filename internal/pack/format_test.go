package pack

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/backup"
	"github.com/nvandessel/floop/internal/store"
	"gopkg.in/yaml.v3"

	"github.com/nvandessel/floop/internal/config"
)

// makeTestBackupData creates a minimal BackupFormat for testing.
func makeTestBackupData() *backup.BackupFormat {
	return &backup.BackupFormat{
		Version:   backup.FormatV2,
		CreatedAt: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
		Nodes: []backup.BackupNode{
			{Node: store.Node{
				ID:   "test-behavior-1",
				Kind: "behavior",
				Content: map[string]interface{}{
					"name": "test behavior",
				},
				Metadata: map[string]interface{}{
					"confidence": 0.9,
				},
			}},
		},
		Edges: []store.Edge{
			{
				Source: "test-behavior-1",
				Target: "test-behavior-2",
				Kind:   store.EdgeKindSimilarTo,
				Weight: 0.8,
			},
		},
	}
}

func TestWriteReadPackFile(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "test.fpack")

	data := makeTestBackupData()
	manifest := PackManifest{
		ID:          "my-org/my-pack",
		Version:     "1.0.0",
		Description: "A test skill pack",
		Author:      "Test Author",
		Tags:        []string{"testing", "example"},
		Source:      "https://github.com/example/pack",
	}

	// Write
	if err := WritePackFile(packPath, data, manifest, nil); err != nil {
		t.Fatalf("WritePackFile() error = %v", err)
	}

	// Read back
	readData, readManifest, err := ReadPackFile(packPath)
	if err != nil {
		t.Fatalf("ReadPackFile() error = %v", err)
	}

	// Verify manifest fields
	if readManifest.ID != manifest.ID {
		t.Errorf("ID = %q, want %q", readManifest.ID, manifest.ID)
	}
	if readManifest.Version != manifest.Version {
		t.Errorf("Version = %q, want %q", readManifest.Version, manifest.Version)
	}
	if readManifest.Description != manifest.Description {
		t.Errorf("Description = %q, want %q", readManifest.Description, manifest.Description)
	}
	if readManifest.Author != manifest.Author {
		t.Errorf("Author = %q, want %q", readManifest.Author, manifest.Author)
	}
	if readManifest.Source != manifest.Source {
		t.Errorf("Source = %q, want %q", readManifest.Source, manifest.Source)
	}
	if !reflect.DeepEqual(readManifest.Tags, manifest.Tags) {
		t.Errorf("Tags = %v, want %v", readManifest.Tags, manifest.Tags)
	}

	// Verify backup data round-trips
	if len(readData.Nodes) != len(data.Nodes) {
		t.Errorf("Nodes count = %d, want %d", len(readData.Nodes), len(data.Nodes))
	}
	if len(readData.Edges) != len(data.Edges) {
		t.Errorf("Edges count = %d, want %d", len(readData.Edges), len(data.Edges))
	}
	if readData.Nodes[0].ID != data.Nodes[0].ID {
		t.Errorf("Node ID = %q, want %q", readData.Nodes[0].ID, data.Nodes[0].ID)
	}
}

func TestWritePackFile_InvalidID(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "test.fpack")

	data := makeTestBackupData()
	manifest := PackManifest{
		ID:      "INVALID",
		Version: "1.0.0",
	}

	err := WritePackFile(packPath, data, manifest, nil)
	if err == nil {
		t.Fatal("WritePackFile() expected error for invalid pack ID")
	}
}

func TestWritePackFile_WithWriteOptions(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "test.fpack")

	data := makeTestBackupData()
	manifest := PackManifest{
		ID:      "my-org/my-pack",
		Version: "1.0.0",
	}
	writeOpts := &backup.WriteOptions{
		FloopVersion: "0.6.0",
		Metadata: map[string]string{
			"custom_key": "custom_value",
		},
	}

	if err := WritePackFile(packPath, data, manifest, writeOpts); err != nil {
		t.Fatalf("WritePackFile() error = %v", err)
	}

	// Read header to verify both pack metadata and user metadata
	header, err := backup.ReadV2Header(packPath)
	if err != nil {
		t.Fatalf("ReadV2Header() error = %v", err)
	}

	if header.Metadata[MetaKeyType] != PackFileType {
		t.Errorf("type = %q, want %q", header.Metadata[MetaKeyType], PackFileType)
	}
	if header.Metadata["custom_key"] != "custom_value" {
		t.Errorf("custom_key = %q, want %q", header.Metadata["custom_key"], "custom_value")
	}
	if header.Metadata["floop_version"] != "0.6.0" {
		t.Errorf("floop_version = %q, want %q", header.Metadata["floop_version"], "0.6.0")
	}
}

func TestReadPackFile_NotAPack(t *testing.T) {
	tmpDir := t.TempDir()
	backupPath := filepath.Join(tmpDir, "regular.json.gz")

	// Write a regular backup (not a pack)
	data := makeTestBackupData()
	if err := backup.WriteV2(backupPath, data, nil); err != nil {
		t.Fatalf("WriteV2() error = %v", err)
	}

	_, _, err := ReadPackFile(backupPath)
	if err == nil {
		t.Fatal("ReadPackFile() expected error for non-pack file")
	}
	if !contains(err.Error(), "not a skill pack") {
		t.Errorf("error = %q, want to contain 'not a skill pack'", err.Error())
	}
}

func TestReadPackHeader(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "test.fpack")

	data := makeTestBackupData()
	manifest := PackManifest{
		ID:          "my-org/header-test",
		Version:     "2.0.0",
		Description: "Header test pack",
		Author:      "Header Author",
		Tags:        []string{"header", "test"},
		Source:      "https://example.com",
	}

	if err := WritePackFile(packPath, data, manifest, nil); err != nil {
		t.Fatalf("WritePackFile() error = %v", err)
	}

	// Read only header
	readManifest, err := ReadPackHeader(packPath)
	if err != nil {
		t.Fatalf("ReadPackHeader() error = %v", err)
	}

	if readManifest.ID != manifest.ID {
		t.Errorf("ID = %q, want %q", readManifest.ID, manifest.ID)
	}
	if readManifest.Version != manifest.Version {
		t.Errorf("Version = %q, want %q", readManifest.Version, manifest.Version)
	}
	if readManifest.Description != manifest.Description {
		t.Errorf("Description = %q, want %q", readManifest.Description, manifest.Description)
	}
	if readManifest.Author != manifest.Author {
		t.Errorf("Author = %q, want %q", readManifest.Author, manifest.Author)
	}
	if !reflect.DeepEqual(readManifest.Tags, manifest.Tags) {
		t.Errorf("Tags = %v, want %v", readManifest.Tags, manifest.Tags)
	}
}

func TestReadPackHeader_NotAPack(t *testing.T) {
	tmpDir := t.TempDir()
	backupPath := filepath.Join(tmpDir, "regular.json.gz")

	data := makeTestBackupData()
	if err := backup.WriteV2(backupPath, data, nil); err != nil {
		t.Fatalf("WriteV2() error = %v", err)
	}

	_, err := ReadPackHeader(backupPath)
	if err == nil {
		t.Fatal("ReadPackHeader() expected error for non-pack file")
	}
}

func TestIsPackFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a pack file
	packPath := filepath.Join(tmpDir, "test.fpack")
	data := makeTestBackupData()
	manifest := PackManifest{
		ID:      "my-org/is-pack-test",
		Version: "1.0.0",
	}
	if err := WritePackFile(packPath, data, manifest, nil); err != nil {
		t.Fatalf("WritePackFile() error = %v", err)
	}

	// Create a regular backup
	backupPath := filepath.Join(tmpDir, "regular.json.gz")
	if err := backup.WriteV2(backupPath, data, nil); err != nil {
		t.Fatalf("WriteV2() error = %v", err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"pack file", packPath, true},
		{"regular backup", backupPath, false},
		{"non-existent file", filepath.Join(tmpDir, "does-not-exist"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPackFile(tt.path)
			if got != tt.want {
				t.Errorf("IsPackFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestPackManifest_Tags(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "tags-test.fpack")

	data := makeTestBackupData()
	tags := []string{"go", "testing", "ci-cd", "multi-word"}
	manifest := PackManifest{
		ID:      "my-org/tags-test",
		Version: "1.0.0",
		Tags:    tags,
	}

	if err := WritePackFile(packPath, data, manifest, nil); err != nil {
		t.Fatalf("WritePackFile() error = %v", err)
	}

	readManifest, err := ReadPackHeader(packPath)
	if err != nil {
		t.Fatalf("ReadPackHeader() error = %v", err)
	}

	if !reflect.DeepEqual(readManifest.Tags, tags) {
		t.Errorf("Tags = %v, want %v", readManifest.Tags, tags)
	}
}

func TestPackManifest_EmptyOptionalFields(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "minimal.fpack")

	data := makeTestBackupData()
	manifest := PackManifest{
		ID:      "my-org/minimal",
		Version: "1.0.0",
		// No Author, Description, Tags, Source
	}

	if err := WritePackFile(packPath, data, manifest, nil); err != nil {
		t.Fatalf("WritePackFile() error = %v", err)
	}

	readManifest, err := ReadPackHeader(packPath)
	if err != nil {
		t.Fatalf("ReadPackHeader() error = %v", err)
	}

	if readManifest.Author != "" {
		t.Errorf("Author = %q, want empty", readManifest.Author)
	}
	if readManifest.Description != "" {
		t.Errorf("Description = %q, want empty", readManifest.Description)
	}
	if readManifest.Source != "" {
		t.Errorf("Source = %q, want empty", readManifest.Source)
	}
	if readManifest.Tags != nil {
		t.Errorf("Tags = %v, want nil", readManifest.Tags)
	}
}

func TestPacksConfig_YAML(t *testing.T) {
	cfg := config.PacksConfig{
		Installed: []config.InstalledPack{
			{
				ID:            "floop/core",
				Version:       "1.0.0",
				InstalledAt:   time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
				Source:        "https://registry.floop.dev/floop/core",
				BehaviorCount: 9,
				EdgeCount:     3,
			},
			{
				ID:            "my-org/my-pack",
				Version:       "2.1.0",
				InstalledAt:   time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC),
				BehaviorCount: 5,
				EdgeCount:     0,
			},
		},
		Registries: []config.Registry{
			{
				Name: "default",
				URL:  "https://registry.floop.dev",
			},
		},
	}

	// Marshal to YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	// Unmarshal back
	var decoded config.PacksConfig
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	// Verify round-trip
	if len(decoded.Installed) != 2 {
		t.Fatalf("Installed count = %d, want 2", len(decoded.Installed))
	}
	if decoded.Installed[0].ID != "floop/core" {
		t.Errorf("Installed[0].ID = %q, want %q", decoded.Installed[0].ID, "floop/core")
	}
	if decoded.Installed[0].Version != "1.0.0" {
		t.Errorf("Installed[0].Version = %q, want %q", decoded.Installed[0].Version, "1.0.0")
	}
	if decoded.Installed[0].BehaviorCount != 9 {
		t.Errorf("Installed[0].BehaviorCount = %d, want 9", decoded.Installed[0].BehaviorCount)
	}
	if decoded.Installed[0].EdgeCount != 3 {
		t.Errorf("Installed[0].EdgeCount = %d, want 3", decoded.Installed[0].EdgeCount)
	}
	if decoded.Installed[0].Source != "https://registry.floop.dev/floop/core" {
		t.Errorf("Installed[0].Source = %q, want %q", decoded.Installed[0].Source, "https://registry.floop.dev/floop/core")
	}
	if decoded.Installed[1].ID != "my-org/my-pack" {
		t.Errorf("Installed[1].ID = %q, want %q", decoded.Installed[1].ID, "my-org/my-pack")
	}

	if len(decoded.Registries) != 1 {
		t.Fatalf("Registries count = %d, want 1", len(decoded.Registries))
	}
	if decoded.Registries[0].Name != "default" {
		t.Errorf("Registries[0].Name = %q, want %q", decoded.Registries[0].Name, "default")
	}
	if decoded.Registries[0].URL != "https://registry.floop.dev" {
		t.Errorf("Registries[0].URL = %q, want %q", decoded.Registries[0].URL, "https://registry.floop.dev")
	}
}

func TestPacksConfig_InFloopConfig_YAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
packs:
  installed:
    - id: floop/core
      version: "1.0.0"
      installed_at: 2025-06-15T10:00:00Z
      behavior_count: 9
      edge_count: 3
  registries:
    - name: default
      url: https://registry.floop.dev
`
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if len(cfg.Packs.Installed) != 1 {
		t.Fatalf("Installed count = %d, want 1", len(cfg.Packs.Installed))
	}
	if cfg.Packs.Installed[0].ID != "floop/core" {
		t.Errorf("Installed[0].ID = %q, want %q", cfg.Packs.Installed[0].ID, "floop/core")
	}
	if cfg.Packs.Installed[0].BehaviorCount != 9 {
		t.Errorf("Installed[0].BehaviorCount = %d, want 9", cfg.Packs.Installed[0].BehaviorCount)
	}
	if len(cfg.Packs.Registries) != 1 {
		t.Fatalf("Registries count = %d, want 1", len(cfg.Packs.Registries))
	}
	if cfg.Packs.Registries[0].URL != "https://registry.floop.dev" {
		t.Errorf("Registries[0].URL = %q, want %q", cfg.Packs.Registries[0].URL, "https://registry.floop.dev")
	}
}

// contains is a simple string containment helper for test assertions.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
