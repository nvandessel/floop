//go:build integration

package vault

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

// Integration tests require MinIO running on localhost:9000.
// Run: docker compose -f docker-compose.vault-test.yml up -d
// Then: go test -tags integration -v ./internal/vault/...

var testEndpoint = "http://localhost:9000"
var testBucket = "floop-vault-test"

func testRemoteConfig(prefix string) VaultRemoteConfig {
	return VaultRemoteConfig{
		URI:             fmt.Sprintf("s3://%s/%s", testBucket, prefix),
		Endpoint:        testEndpoint,
		Region:          "us-east-1",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		PathStyle:       true,
		AllowHTTP:       true,
	}
}

func TestIntegration_S3ClientRoundTrip(t *testing.T) {
	prefix := fmt.Sprintf("test-s3-%d", time.Now().UnixNano())
	cfg := testRemoteConfig(prefix)

	// Ensure bucket exists
	ensureTestBucket(t)

	client, err := NewS3Client(cfg)
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	ctx := context.Background()

	// Upload
	testData := []byte("hello vault integration test")
	if err := client.Upload(ctx, "test-file.txt", newReader(testData), int64(len(testData))); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// Exists
	exists, err := client.Exists(ctx, "test-file.txt")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("file should exist after upload")
	}

	// Download
	rc, err := client.Download(ctx, "test-file.txt")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	defer rc.Close()

	downloaded, err := readAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(downloaded) != string(testData) {
		t.Errorf("downloaded = %q, want %q", string(downloaded), string(testData))
	}

	// PutJSON / GetJSON
	type TestStruct struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	want := TestStruct{Name: "test", Count: 42}
	if err := client.PutJSON(ctx, "test.json", want); err != nil {
		t.Fatalf("PutJSON: %v", err)
	}

	var got TestStruct
	if err := client.GetJSON(ctx, "test.json", &got); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if got != want {
		t.Errorf("GetJSON = %+v, want %+v", got, want)
	}
}

func TestIntegration_VaultInitConnectsToMinIO(t *testing.T) {
	prefix := fmt.Sprintf("test-init-%d", time.Now().UnixNano())
	ensureTestBucket(t)

	cfg := &VaultConfig{
		Remote:    testRemoteConfig(prefix),
		MachineID: "test-machine",
		Sync:      VaultSyncConfig{Timeout: "30s"},
	}

	svc, err := NewVaultService(cfg, t.TempDir(), "test", 4)
	if err != nil {
		t.Fatalf("NewVaultService: %v", err)
	}

	ctx := context.Background()
	if err := svc.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Verify sentinel was written
	s3Client, _ := NewS3Client(cfg.Remote)
	exists, _ := s3Client.Exists(ctx, "_floop_vault_initialized")
	if !exists {
		t.Error("sentinel should exist after Init")
	}
}

func TestIntegration_VaultPushAndPull(t *testing.T) {
	prefix := fmt.Sprintf("test-pushpull-%d", time.Now().UnixNano())
	ensureTestBucket(t)

	cfg := &VaultConfig{
		Remote:    testRemoteConfig(prefix),
		MachineID: "machineA",
		Sync:      VaultSyncConfig{Timeout: "30s"},
	}

	// Create a graph store with some data
	graphStore := newTestGraphStore(t)

	tmpDir := t.TempDir()
	svc, err := NewVaultService(cfg, filepath.Join(tmpDir, "vectors"), "test", 4)
	if err != nil {
		t.Fatalf("NewVaultService: %v", err)
	}

	ctx := context.Background()

	// Push
	_, err = svc.Push(ctx, graphStore, tmpDir, PushOptions{Scope: "global"})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	// Pull to a fresh store
	graphStoreB := newTestGraphStore(t)
	cfgB := *cfg
	cfgB.MachineID = "machineB"
	svcB, _ := NewVaultService(&cfgB, filepath.Join(t.TempDir(), "vectors"), "test", 4)

	_, err = svcB.Pull(ctx, graphStoreB, PullOptions{
		FromMachine: "machineA",
		Scope:       "global",
	})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
}

// ensureTestBucket creates the test bucket via the MinIO client.
func ensureTestBucket(t *testing.T) {
	t.Helper()
	cfg := VaultRemoteConfig{
		URI:             fmt.Sprintf("s3://%s", testBucket),
		Endpoint:        testEndpoint,
		Region:          "us-east-1",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		PathStyle:       true,
		AllowHTTP:       true,
	}
	client, err := NewS3Client(cfg)
	if err != nil {
		t.Skipf("cannot connect to MinIO: %v (run docker compose -f docker-compose.vault-test.yml up -d)", err)
	}

	ctx := context.Background()
	err = client.client.MakeBucket(ctx, testBucket, minio.MakeBucketOptions{})
	if err != nil {
		// Bucket may already exist, that's fine
		exists, existErr := client.client.BucketExists(ctx, testBucket)
		if existErr != nil || !exists {
			t.Skipf("cannot create test bucket: %v", err)
		}
	}
}

func newReader(data []byte) io.Reader {
	return bytes.NewReader(data)
}

func readAll(rc io.ReadCloser) ([]byte, error) {
	return io.ReadAll(rc)
}
