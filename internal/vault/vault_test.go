package vault

import (
	"testing"
)

func TestNewVaultService_UnconfiguredReturnsError(t *testing.T) {
	cfg := &VaultConfig{}
	_, err := NewVaultService(cfg, "/tmp/vectors", "1.0.0", 384)
	if err == nil {
		t.Fatal("expected error for unconfigured vault")
	}
	if !contains(err.Error(), "not configured") {
		t.Errorf("error should mention 'not configured': %v", err)
	}
}

func TestNormalizeScope(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"global", "global"},
		{"local", "local"},
		{"both", "both"},
		{"GLOBAL", "global"},
		{"", "global"},
		{"invalid", "global"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeScope(tt.input)
			if got != tt.want {
				t.Errorf("normalizeScope(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRemoteVectorURI(t *testing.T) {
	cfg := &VaultConfig{
		Remote: VaultRemoteConfig{
			URI:             "s3://floop-vault/brain",
			Endpoint:        "https://minio.example.com:9000",
			Region:          "us-east-1",
			AccessKeyID:     "key",
			SecretAccessKey: "secret",
		},
	}

	svc := &VaultService{cfg: cfg}
	got := svc.remoteVectorURI("workstation")
	want := "s3://floop-vault/brain/machines/workstation/vectors"
	if got != want {
		t.Errorf("remoteVectorURI = %q, want %q", got, want)
	}
}
