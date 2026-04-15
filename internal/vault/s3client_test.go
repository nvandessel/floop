package vault

import "testing"

func TestNewS3Client_URIParsing(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantBucket string
		wantPrefix string
		wantErr    bool
	}{
		{"bucket only", "s3://mybucket", "mybucket", "", false},
		{"bucket with prefix", "s3://mybucket/prefix", "mybucket", "prefix", false},
		{"bucket with deep prefix", "s3://mybucket/deep/prefix", "mybucket", "deep/prefix", false},
		{"empty bucket", "s3://", "", "", true},
		{"invalid scheme", "http://bucket", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := VaultRemoteConfig{
				URI:             tt.uri,
				Endpoint:        "http://localhost:9000",
				Region:          "us-east-1",
				AccessKeyID:     "minioadmin",
				SecretAccessKey: "minioadmin",
				AllowHTTP:       true,
			}

			client, err := NewS3Client(cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if client.Bucket() != tt.wantBucket {
				t.Errorf("bucket = %q, want %q", client.Bucket(), tt.wantBucket)
			}
			if client.Prefix() != tt.wantPrefix {
				t.Errorf("prefix = %q, want %q", client.Prefix(), tt.wantPrefix)
			}
		})
	}
}

func TestNewS3Client_MissingEndpoint(t *testing.T) {
	cfg := VaultRemoteConfig{
		URI:             "s3://bucket",
		Region:          "us-east-1",
		AccessKeyID:     "key",
		SecretAccessKey: "secret",
	}
	_, err := NewS3Client(cfg)
	if err == nil {
		t.Fatal("expected error for missing endpoint")
	}
}

func TestS3Client_FullKey(t *testing.T) {
	tests := []struct {
		prefix string
		key    string
		want   string
	}{
		{"", "file.txt", "file.txt"},
		{"brain", "file.txt", "brain/file.txt"},
		{"deep/prefix", "file.txt", "deep/prefix/file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.prefix+"/"+tt.key, func(t *testing.T) {
			c := &S3Client{prefix: tt.prefix}
			if got := c.fullKey(tt.key); got != tt.want {
				t.Errorf("fullKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}
