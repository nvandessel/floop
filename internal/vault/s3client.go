package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Operations defines the interface for S3 file operations.
// Used for testability of components that depend on S3.
type S3Operations interface {
	Upload(ctx context.Context, key string, reader io.Reader, size int64) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Exists(ctx context.Context, key string) (bool, error)
	PutJSON(ctx context.Context, key string, v interface{}) error
	GetJSON(ctx context.Context, key string, v interface{}) error
}

// S3Client wraps the MinIO Go SDK for file upload/download to S3-compatible storage.
type S3Client struct {
	client *minio.Client
	bucket string
	prefix string
}

// NewS3Client creates an S3Client from vault remote config.
func NewS3Client(cfg VaultRemoteConfig) (*S3Client, error) {
	bucket, prefix := parseS3URI(cfg.URI)
	if bucket == "" {
		return nil, fmt.Errorf("invalid S3 URI: must contain a bucket name")
	}

	endpoint := cfg.Endpoint
	if endpoint == "" {
		return nil, fmt.Errorf("endpoint is required for S3Client")
	}

	// Parse endpoint to extract host (without scheme)
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing endpoint URL: %w", err)
	}
	host := u.Host
	if host == "" {
		host = endpoint // fallback if no scheme
	}
	useSSL := !cfg.AllowHTTP && u.Scheme != "http"

	client, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("creating MinIO client: %w", err)
	}

	return &S3Client{
		client: client,
		bucket: bucket,
		prefix: prefix,
	}, nil
}

// fullKey returns the full S3 object key by joining prefix and key.
func (c *S3Client) fullKey(key string) string {
	if c.prefix == "" {
		return key
	}
	return c.prefix + "/" + key
}

// Upload uploads data to the given key.
func (c *S3Client) Upload(ctx context.Context, key string, reader io.Reader, size int64) error {
	_, err := c.client.PutObject(ctx, c.bucket, c.fullKey(key), reader, size, minio.PutObjectOptions{})
	if err != nil {
		return fmt.Errorf("uploading %s: %w", key, err)
	}
	return nil
}

// Download downloads the object at the given key.
func (c *S3Client) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := c.client.GetObject(ctx, c.bucket, c.fullKey(key), minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", key, err)
	}
	// Verify the object exists by stat'ing it
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, fmt.Errorf("downloading %s: %w", key, err)
	}
	return obj, nil
}

// Exists checks if an object exists at the given key.
func (c *S3Client) Exists(ctx context.Context, key string) (bool, error) {
	_, err := c.client.StatObject(ctx, c.bucket, c.fullKey(key), minio.StatObjectOptions{})
	if err != nil {
		errResp := minio.ToErrorResponse(err)
		if errResp.Code == "NoSuchKey" || errResp.StatusCode == 404 {
			return false, nil
		}
		return false, fmt.Errorf("checking existence of %s: %w", key, err)
	}
	return true, nil
}

// PutJSON marshals v to JSON and uploads it.
func (c *S3Client) PutJSON(ctx context.Context, key string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON for %s: %w", key, err)
	}
	return c.Upload(ctx, key, bytes.NewReader(data), int64(len(data)))
}

// GetJSON downloads JSON from the given key and unmarshals it into v.
func (c *S3Client) GetJSON(ctx context.Context, key string, v interface{}) error {
	rc, err := c.Download(ctx, key)
	if err != nil {
		return err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("reading %s: %w", key, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parsing JSON from %s: %w", key, err)
	}
	return nil
}

// Bucket returns the bucket name.
func (c *S3Client) Bucket() string {
	return c.bucket
}

// Prefix returns the key prefix.
func (c *S3Client) Prefix() string {
	return c.prefix
}

// Verify S3Client satisfies S3Operations at compile time.
var _ S3Operations = (*S3Client)(nil)

// parseEndpointHost extracts the host:port from an endpoint URL, stripping the scheme.
func parseEndpointHost(endpoint string) string {
	if idx := strings.Index(endpoint, "://"); idx != -1 {
		return endpoint[idx+3:]
	}
	return endpoint
}
