package vault

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lancedb/lancedb-go/pkg/contracts"
)

func TestVaultConfig_Validate(t *testing.T) {
	validConfig := func() VaultConfig {
		return VaultConfig{
			Remote: VaultRemoteConfig{
				URI:            "s3://floop-vault/brain",
				Endpoint:       "https://minio.example.com:9000",
				Region:         "us-east-1",
				AccessKeyID:    "AKIAIOSFODNN7EXAMPLE",
				SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				PathStyle:      true,
			},
			MachineID: "workstation",
			Sync: VaultSyncConfig{
				Timeout:         "30s",
				IncludeProjects: true,
			},
		}
	}

	t.Run("valid config passes", func(t *testing.T) {
		cfg := validConfig()
		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unconfigured vault is valid", func(t *testing.T) {
		cfg := VaultConfig{}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("empty config should be valid: %v", err)
		}
	})

	tests := []struct {
		name   string
		modify func(*VaultConfig)
		errStr string
	}{
		{
			name:   "invalid URI scheme",
			modify: func(c *VaultConfig) { c.Remote.URI = "http://bucket/prefix" },
			errStr: "must start with s3://",
		},
		{
			name:   "URI without bucket",
			modify: func(c *VaultConfig) { c.Remote.URI = "s3://" },
			errStr: "must contain a bucket name",
		},
		{
			name:   "empty region",
			modify: func(c *VaultConfig) { c.Remote.Region = "" },
			errStr: "vault.remote.region: required",
		},
		{
			name:   "empty access key",
			modify: func(c *VaultConfig) { c.Remote.AccessKeyID = "" },
			errStr: "vault.remote.access_key_id: required",
		},
		{
			name:   "empty secret key",
			modify: func(c *VaultConfig) { c.Remote.SecretAccessKey = "" },
			errStr: "vault.remote.secret_access_key: required",
		},
		{
			name:   "invalid machine_id with slash",
			modify: func(c *VaultConfig) { c.MachineID = "bad/id" },
			errStr: "must be alphanumeric",
		},
		{
			name:   "invalid machine_id with space",
			modify: func(c *VaultConfig) { c.MachineID = "bad id" },
			errStr: "must be alphanumeric",
		},
		{
			name:   "invalid timeout duration",
			modify: func(c *VaultConfig) { c.Sync.Timeout = "not-a-duration" },
			errStr: "invalid duration",
		},
		{
			name:   "timeout exceeds max",
			modify: func(c *VaultConfig) { c.Sync.Timeout = "15m" },
			errStr: "must be <= 10m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := err.Error(); !contains(got, tt.errStr) {
				t.Errorf("error %q should contain %q", got, tt.errStr)
			}
		})
	}
}

func TestVaultConfig_ValidMachineIDs(t *testing.T) {
	validIDs := []string{"my-host", "my.host.fqdn", "host_1", "UPPER", "host123"}
	for _, id := range validIDs {
		t.Run(id, func(t *testing.T) {
			if !machineIDRegex.MatchString(id) {
				t.Errorf("machine ID %q should be valid", id)
			}
		})
	}
}

func TestVaultConfig_EncryptionValidation(t *testing.T) {
	t.Run("encryption enabled without identity file", func(t *testing.T) {
		cfg := VaultConfig{
			Remote: VaultRemoteConfig{
				URI:            "s3://bucket/prefix",
				Region:         "us-east-1",
				AccessKeyID:    "key",
				SecretAccessKey: "secret",
			},
			MachineID: "test",
			Encryption: VaultEncryptionConfig{
				Enabled:   true,
				Recipient: "age1xyz...",
			},
		}
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for missing identity file")
		}
		if !contains(err.Error(), "identity_file") {
			t.Errorf("expected identity_file error, got: %v", err)
		}
	})

	t.Run("encryption enabled with nonexistent identity file", func(t *testing.T) {
		cfg := VaultConfig{
			Remote: VaultRemoteConfig{
				URI:            "s3://bucket/prefix",
				Region:         "us-east-1",
				AccessKeyID:    "key",
				SecretAccessKey: "secret",
			},
			MachineID: "test",
			Encryption: VaultEncryptionConfig{
				Enabled:      true,
				IdentityFile: "/nonexistent/key.txt",
				Recipient:    "age1xyz...",
			},
		}
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for nonexistent identity file")
		}
	})

	t.Run("encryption enabled without recipient", func(t *testing.T) {
		tmp := t.TempDir()
		keyFile := filepath.Join(tmp, "key.txt")
		os.WriteFile(keyFile, []byte("key"), 0600)

		cfg := VaultConfig{
			Remote: VaultRemoteConfig{
				URI:            "s3://bucket/prefix",
				Region:         "us-east-1",
				AccessKeyID:    "key",
				SecretAccessKey: "secret",
			},
			MachineID: "test",
			Encryption: VaultEncryptionConfig{
				Enabled:      true,
				IdentityFile: keyFile,
			},
		}
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for missing recipient")
		}
		if !contains(err.Error(), "recipient") {
			t.Errorf("expected recipient error, got: %v", err)
		}
	})
}

func TestVaultRemoteConfig_StorageOptions(t *testing.T) {
	cfg := VaultRemoteConfig{
		URI:            "s3://floop-vault/brain",
		Endpoint:       "https://minio.example.com:9000",
		Region:         "us-east-1",
		AccessKeyID:    "AKID",
		SecretAccessKey: "SECRET",
		PathStyle:      true,
		AllowHTTP:      false,
	}

	opts := cfg.StorageOptions()

	expected := map[string]string{
		contracts.StorageAccessKeyID:              "AKID",
		contracts.StorageSecretAccessKey:           "SECRET",
		contracts.StorageRegion:                    "us-east-1",
		contracts.StorageEndpoint:                  "https://minio.example.com:9000",
		contracts.StorageVirtualHostedStyleRequest: "false", // PathStyle=true → VirtualHosted=false
		contracts.StorageAllowHTTP:                 "false",
	}

	for k, want := range expected {
		if got := opts[k]; got != want {
			t.Errorf("StorageOptions[%q] = %q, want %q", k, got, want)
		}
	}
}

func TestVaultRemoteConfig_StorageOptions_NoEndpoint(t *testing.T) {
	cfg := VaultRemoteConfig{
		URI:            "s3://bucket",
		Region:         "eu-west-1",
		AccessKeyID:    "key",
		SecretAccessKey: "secret",
	}
	opts := cfg.StorageOptions()
	if _, ok := opts[contracts.StorageEndpoint]; ok {
		t.Error("endpoint should not be set when VaultRemoteConfig.Endpoint is empty")
	}
}

func TestVaultRemoteConfig_String_RedactsSecret(t *testing.T) {
	cfg := VaultRemoteConfig{
		URI:            "s3://bucket/prefix",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}
	s := cfg.String()
	if contains(s, "wJalrXUtnFEMI") {
		t.Errorf("String() should not contain the raw secret key: %s", s)
	}
	if !contains(s, "wJal") {
		t.Errorf("String() should show first 4 chars of secret: %s", s)
	}
}

func TestVaultRemoteConfig_RedactedSecretKey(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		want   string
	}{
		{"empty", "", ""},
		{"short", "abc", "(set)"},
		{"normal", "wJalrXUtnFEMI/K7MDENG", "wJal...DENG"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := VaultRemoteConfig{SecretAccessKey: tt.key}
			got := cfg.RedactedSecretKey()
			if got != tt.want {
				t.Errorf("RedactedSecretKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseS3URI(t *testing.T) {
	tests := []struct {
		uri        string
		wantBucket string
		wantPrefix string
	}{
		{"s3://bucket", "bucket", ""},
		{"s3://bucket/prefix", "bucket", "prefix"},
		{"s3://bucket/deep/prefix", "bucket", "deep/prefix"},
		{"s3://", "", ""},
		{"http://bucket", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			bucket, prefix := parseS3URI(tt.uri)
			if bucket != tt.wantBucket {
				t.Errorf("bucket = %q, want %q", bucket, tt.wantBucket)
			}
			if prefix != tt.wantPrefix {
				t.Errorf("prefix = %q, want %q", prefix, tt.wantPrefix)
			}
		})
	}
}

func TestVaultConfig_ExpandEnvVars(t *testing.T) {
	t.Setenv("TEST_VAULT_KEY", "my-access-key")
	t.Setenv("TEST_VAULT_SECRET", "my-secret-key")

	cfg := VaultConfig{
		Remote: VaultRemoteConfig{
			AccessKeyID:    "${TEST_VAULT_KEY}",
			SecretAccessKey: "${TEST_VAULT_SECRET}",
		},
	}
	cfg.ExpandEnvVars()

	if cfg.Remote.AccessKeyID != "my-access-key" {
		t.Errorf("AccessKeyID = %q, want %q", cfg.Remote.AccessKeyID, "my-access-key")
	}
	if cfg.Remote.SecretAccessKey != "my-secret-key" {
		t.Errorf("SecretAccessKey = %q, want %q", cfg.Remote.SecretAccessKey, "my-secret-key")
	}
}

func TestVaultConfig_ResolveMachineID(t *testing.T) {
	t.Run("explicit ID", func(t *testing.T) {
		cfg := VaultConfig{MachineID: "my-machine"}
		if got := cfg.ResolveMachineID(); got != "my-machine" {
			t.Errorf("got %q, want %q", got, "my-machine")
		}
	})

	t.Run("falls back to hostname", func(t *testing.T) {
		cfg := VaultConfig{}
		got := cfg.ResolveMachineID()
		if got == "" {
			t.Error("ResolveMachineID should not return empty string")
		}
	})
}

func TestVaultConfig_SyncTimeout(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		cfg := VaultConfig{}
		if got := cfg.SyncTimeout(); got.String() != "30s" {
			t.Errorf("default timeout = %s, want 30s", got)
		}
	})

	t.Run("custom", func(t *testing.T) {
		cfg := VaultConfig{Sync: VaultSyncConfig{Timeout: "1m"}}
		if got := cfg.SyncTimeout(); got.String() != "1m0s" {
			t.Errorf("timeout = %s, want 1m0s", got)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
