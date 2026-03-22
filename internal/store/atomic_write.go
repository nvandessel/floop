package store

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
)

// atomicWriteFile writes to targetPath crash-safely by writing to a temp file
// in the same directory, calling fsync, then atomically renaming over the target.
func atomicWriteFile(targetPath string, writeFn func(f *os.File) error) error {
	dir := filepath.Dir(targetPath)
	// Use os.OpenFile with O_EXCL and mode 0666 so the kernel applies umask
	// at creation time, matching the permissions os.Create produces (typically 0644).
	// Random suffix ensures uniqueness across concurrent calls and PID reuse.
	var rnd [4]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return fmt.Errorf("failed to generate random suffix: %w", err)
	}
	tmpPath := filepath.Join(dir, fmt.Sprintf("%s.tmp.%x", filepath.Base(targetPath), rnd))
	tmp, err := os.OpenFile(tmpPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	// Clean up temp file on any error
	success := false
	tmpClosed := false
	defer func() {
		if !success {
			if !tmpClosed {
				tmp.Close()
			}
			os.Remove(tmpPath)
		}
	}()

	if err := writeFn(tmp); err != nil {
		return err
	}

	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("failed to fsync temp file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	tmpClosed = true

	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	success = true
	return nil
}
