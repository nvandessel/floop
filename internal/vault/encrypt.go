package vault

import (
	"fmt"
	"os"
	"os/exec"
)

// EncryptFile encrypts src to dst using the given age recipient (public key).
func EncryptFile(recipient string, src, dst string) error {
	agePath, err := exec.LookPath("age")
	if err != nil {
		return fmt.Errorf("age binary not found: install from https://age-encryption.org/")
	}

	cmd := exec.Command(agePath, "-r", recipient, "-o", dst, src)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Remove(dst) // clean up on failure
		return fmt.Errorf("encrypting %s: %w", src, err)
	}
	return nil
}

// DecryptFile decrypts src to dst using the given age identity file.
func DecryptFile(identityFile string, src, dst string) error {
	agePath, err := exec.LookPath("age")
	if err != nil {
		return fmt.Errorf("age binary not found: install from https://age-encryption.org/")
	}

	cmd := exec.Command(agePath, "-d", "-i", identityFile, "-o", dst, src)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Remove(dst) // clean up on failure
		return fmt.Errorf("decrypting %s: %w", src, err)
	}
	return nil
}
