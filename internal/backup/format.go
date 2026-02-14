package backup

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Format version constants.
const (
	FormatV1 = 1
	FormatV2 = 2
)

// MaxDecompressedSize is the maximum allowed size of decompressed backup data (200MB).
const MaxDecompressedSize = 200 * 1024 * 1024

// BackupHeader is the plain-text first line of a V2 backup file.
type BackupHeader struct {
	Version    int               `json:"version"`
	CreatedAt  time.Time         `json:"created_at"`
	Checksum   string            `json:"checksum"`
	NodeCount  int               `json:"node_count"`
	EdgeCount  int               `json:"edge_count"`
	Compressed bool              `json:"compressed"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// DetectFormat reads the first bytes of a file to determine V1 vs V2.
// V2 files have a header line with "version":2. V1 files are plain JSON starting with '{'.
func DetectFormat(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return 0, fmt.Errorf("reading first line: %w", err)
		}
		return 0, fmt.Errorf("file is empty")
	}

	firstLine := strings.TrimSpace(scanner.Text())
	if firstLine == "" {
		return 0, fmt.Errorf("first line is empty")
	}

	// Try to parse as a header
	var header BackupHeader
	if err := json.Unmarshal([]byte(firstLine), &header); err == nil {
		if header.Version == FormatV2 {
			return FormatV2, nil
		}
	}

	// If first line starts with '{', it's a V1 plain JSON file
	if firstLine[0] == '{' {
		return FormatV1, nil
	}

	return 0, fmt.Errorf("unrecognized backup format")
}

// WriteV2 writes a BackupFormat as a V2 file: header line + gzip-compressed payload.
func WriteV2(path string, b *BackupFormat) error {
	// Marshal the payload
	payload, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	// Compress the payload
	var compressed bytes.Buffer
	gzw, err := gzip.NewWriterLevel(&compressed, gzip.DefaultCompression)
	if err != nil {
		return fmt.Errorf("creating gzip writer: %w", err)
	}
	if _, err := gzw.Write(payload); err != nil {
		return fmt.Errorf("compressing payload: %w", err)
	}
	if err := gzw.Close(); err != nil {
		return fmt.Errorf("closing gzip writer: %w", err)
	}

	// Compute SHA-256 of the compressed data
	hash := sha256.Sum256(compressed.Bytes())
	checksum := "sha256:" + hex.EncodeToString(hash[:])

	// Build header
	header := BackupHeader{
		Version:    FormatV2,
		CreatedAt:  b.CreatedAt,
		Checksum:   checksum,
		NodeCount:  len(b.Nodes),
		EdgeCount:  len(b.Edges),
		Compressed: true,
	}

	headerBytes, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("marshaling header: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	// Write file: header line + newline + compressed payload
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer f.Close()

	// Write header line
	if _, err := f.Write(headerBytes); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		return fmt.Errorf("writing header newline: %w", err)
	}

	// Write compressed payload
	if _, err := f.Write(compressed.Bytes()); err != nil {
		return fmt.Errorf("writing compressed payload: %w", err)
	}

	return nil
}

// ReadV2 reads a V2 backup file, verifies the checksum, and decompresses the payload.
func ReadV2(path string) (*BackupFormat, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	// Read header line
	reader := bufio.NewReader(f)
	headerLine, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("reading header line: %w", err)
	}

	var header BackupHeader
	if err := json.Unmarshal(bytes.TrimSpace(headerLine), &header); err != nil {
		return nil, fmt.Errorf("parsing header: %w", err)
	}

	if header.Version != FormatV2 {
		return nil, fmt.Errorf("expected V2 format, got version %d", header.Version)
	}

	// Read the rest (compressed payload)
	compressedData, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading compressed payload: %w", err)
	}

	// Verify checksum
	hash := sha256.Sum256(compressedData)
	actualChecksum := "sha256:" + hex.EncodeToString(hash[:])
	if actualChecksum != header.Checksum {
		return nil, fmt.Errorf("checksum mismatch: expected %s, got %s", header.Checksum, actualChecksum)
	}

	// Decompress
	gzr, err := gzip.NewReader(bytes.NewReader(compressedData))
	if err != nil {
		return nil, fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gzr.Close()

	// Limit decompressed size
	limitedReader := io.LimitReader(gzr, MaxDecompressedSize+1)
	decompressed, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("decompressing payload: %w", err)
	}
	if int64(len(decompressed)) > MaxDecompressedSize {
		return nil, fmt.Errorf("decompressed payload exceeds maximum size of %d bytes", MaxDecompressedSize)
	}

	var backup BackupFormat
	if err := json.Unmarshal(decompressed, &backup); err != nil {
		return nil, fmt.Errorf("parsing backup data: %w", err)
	}

	return &backup, nil
}

// ReadV2Header reads only the header line from a V2 backup file without decompressing.
func ReadV2Header(path string) (*BackupHeader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	headerLine, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("reading header line: %w", err)
	}

	var header BackupHeader
	if err := json.Unmarshal(bytes.TrimSpace(headerLine), &header); err != nil {
		return nil, fmt.Errorf("parsing header: %w", err)
	}

	if header.Version != FormatV2 {
		return nil, fmt.Errorf("expected V2 format, got version %d", header.Version)
	}

	return &header, nil
}

// VerifyChecksum checks the integrity of a V2 backup file without full decompression.
func VerifyChecksum(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	headerLine, err := reader.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("reading header line: %w", err)
	}

	var header BackupHeader
	if err := json.Unmarshal(bytes.TrimSpace(headerLine), &header); err != nil {
		return fmt.Errorf("parsing header: %w", err)
	}

	if header.Version != FormatV2 {
		return fmt.Errorf("checksum verification only supported for V2 format (got version %d)", header.Version)
	}

	// Read compressed payload
	compressedData, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("reading compressed payload: %w", err)
	}

	// Verify checksum
	hash := sha256.Sum256(compressedData)
	actualChecksum := "sha256:" + hex.EncodeToString(hash[:])
	if actualChecksum != header.Checksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", header.Checksum, actualChecksum)
	}

	return nil
}
