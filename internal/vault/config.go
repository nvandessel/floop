// Package vault implements Lance-native S3 backup and sync for floop's
// behavioral memory store.
package vault

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lancedb/lancedb-go/pkg/contracts"
)

// VaultConfig configures vault sync.
type VaultConfig struct {
	Remote     VaultRemoteConfig     `json:"remote" yaml:"remote"`
	MachineID  string                `json:"machine_id" yaml:"machine_id"`
	Sync       VaultSyncConfig       `json:"sync" yaml:"sync"`
	Encryption VaultEncryptionConfig `json:"encryption" yaml:"encryption"`
}

// VaultRemoteConfig configures the S3-compatible remote endpoint.
type VaultRemoteConfig struct {
	URI             string `json:"uri" yaml:"uri"`
	Endpoint        string `json:"endpoint" yaml:"endpoint"`
	Region          string `json:"region" yaml:"region"`
	AccessKeyID     string `json:"access_key_id" yaml:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key" yaml:"secret_access_key"`
	PathStyle       bool   `json:"path_style" yaml:"path_style"`
	AllowHTTP       bool   `json:"allow_http" yaml:"allow_http"`
}

// VaultSyncConfig configures sync behavior.
type VaultSyncConfig struct {
	AutoPush        bool   `json:"auto_push" yaml:"auto_push"`
	IncludeProjects bool   `json:"include_projects" yaml:"include_projects"`
	Timeout         string `json:"timeout" yaml:"timeout"`
}

// VaultEncryptionConfig configures client-side encryption with age.
type VaultEncryptionConfig struct {
	Enabled      bool   `json:"enabled" yaml:"enabled"`
	IdentityFile string `json:"identity_file" yaml:"identity_file"`
	Recipient    string `json:"recipient" yaml:"recipient"`
}

// machineIDRegex validates machine IDs as safe path components.
var machineIDRegex = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// Configured returns true if the vault remote is configured (URI is set).
func (c *VaultConfig) Configured() bool {
	return c.Remote.URI != ""
}

// Validate checks that the vault configuration is valid.
// Only validates if the vault is configured (URI is set).
func (c *VaultConfig) Validate() error {
	if !c.Configured() {
		return nil // unconfigured vault is valid (just unused)
	}

	// Validate remote
	if !strings.HasPrefix(c.Remote.URI, "s3://") {
		return fmt.Errorf("vault.remote.uri: must start with s3://, got %q", c.Remote.URI)
	}
	bucket, _ := parseS3URI(c.Remote.URI)
	if bucket == "" {
		return fmt.Errorf("vault.remote.uri: must contain a bucket name")
	}

	if c.Remote.Endpoint != "" {
		if _, err := url.Parse(c.Remote.Endpoint); err != nil {
			return fmt.Errorf("vault.remote.endpoint: invalid URL: %w", err)
		}
	}

	if c.Remote.Region == "" {
		return fmt.Errorf("vault.remote.region: required")
	}

	if c.Remote.AccessKeyID == "" {
		return fmt.Errorf("vault.remote.access_key_id: required")
	}

	if c.Remote.SecretAccessKey == "" {
		return fmt.Errorf("vault.remote.secret_access_key: required")
	}

	// Validate machine_id
	machineID := c.ResolveMachineID()
	if !machineIDRegex.MatchString(machineID) {
		return fmt.Errorf("vault.machine_id: must be alphanumeric with hyphens, underscores, or dots; got %q", machineID)
	}

	// Validate sync timeout
	if c.Sync.Timeout != "" {
		d, err := time.ParseDuration(c.Sync.Timeout)
		if err != nil {
			return fmt.Errorf("vault.sync.timeout: invalid duration: %w", err)
		}
		if d > 10*time.Minute {
			return fmt.Errorf("vault.sync.timeout: must be <= 10m, got %s", c.Sync.Timeout)
		}
	}

	// Validate encryption
	if c.Encryption.Enabled {
		if c.Encryption.IdentityFile == "" {
			return fmt.Errorf("vault.encryption.identity_file: required when encryption is enabled")
		}
		if _, err := os.Stat(c.Encryption.IdentityFile); err != nil {
			return fmt.Errorf("vault.encryption.identity_file: %w", err)
		}
		if c.Encryption.Recipient == "" {
			return fmt.Errorf("vault.encryption.recipient: required when encryption is enabled")
		}
	}

	return nil
}

// ResolveMachineID returns the effective machine ID.
// Falls back to os.Hostname(), then "localhost".
func (c *VaultConfig) ResolveMachineID() string {
	if c.MachineID != "" {
		return c.MachineID
	}
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return "localhost"
	}
	return hostname
}

// SyncTimeout returns the parsed sync timeout duration, defaulting to 30s.
func (c *VaultConfig) SyncTimeout() time.Duration {
	if c.Sync.Timeout == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(c.Sync.Timeout)
	if err != nil {
		return 30 * time.Second
	}
	return d
}

// StorageOptions builds the lancedb-go ConnectionOptions storage map.
func (c *VaultRemoteConfig) StorageOptions() map[string]string {
	opts := map[string]string{
		contracts.StorageAccessKeyID:               c.AccessKeyID,
		contracts.StorageSecretAccessKey:           c.SecretAccessKey,
		contracts.StorageRegion:                    c.Region,
		contracts.StorageVirtualHostedStyleRequest: strconv.FormatBool(!c.PathStyle),
		contracts.StorageAllowHTTP:                 strconv.FormatBool(c.AllowHTTP),
	}
	if c.Endpoint != "" {
		opts[contracts.StorageEndpoint] = c.Endpoint
	}
	return opts
}

// RedactedSecretKey returns the secret key with most characters masked.
func (c *VaultRemoteConfig) RedactedSecretKey() string {
	if c.SecretAccessKey == "" {
		return ""
	}
	if len(c.SecretAccessKey) < 12 {
		return "(set)"
	}
	return c.SecretAccessKey[:4] + "..." + c.SecretAccessKey[len(c.SecretAccessKey)-4:]
}

// String implements fmt.Stringer with secret redaction.
func (c VaultRemoteConfig) String() string {
	return fmt.Sprintf("VaultRemoteConfig{URI:%s, Endpoint:%s, Region:%s, AccessKeyID:%s, SecretAccessKey:%s}",
		c.URI, c.Endpoint, c.Region, c.AccessKeyID, c.RedactedSecretKey())
}

// parseS3URI extracts bucket and prefix from an s3:// URI.
// Returns bucket="" if the URI is invalid.
func parseS3URI(uri string) (bucket, prefix string) {
	if !strings.HasPrefix(uri, "s3://") {
		return "", ""
	}
	rest := strings.TrimPrefix(uri, "s3://")
	if rest == "" {
		return "", ""
	}
	parts := strings.SplitN(rest, "/", 2)
	bucket = parts[0]
	if len(parts) > 1 {
		prefix = parts[1]
	}
	return bucket, prefix
}

// ExpandEnvVars expands ${VAR} patterns in vault credential fields.
func (c *VaultConfig) ExpandEnvVars() {
	c.Remote.AccessKeyID = expandEnvVars(c.Remote.AccessKeyID)
	c.Remote.SecretAccessKey = expandEnvVars(c.Remote.SecretAccessKey)
}

// expandEnvVars expands ${VAR} patterns in a string.
func expandEnvVars(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return os.Expand(s, os.Getenv)
}
