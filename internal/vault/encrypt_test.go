package vault

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	if _, err := exec.LookPath("age"); err != nil {
		t.Skip("age not installed")
	}
	if _, err := exec.LookPath("age-keygen"); err != nil {
		t.Skip("age-keygen not installed")
	}

	dir := t.TempDir()

	// Generate keypair
	keygenOut, err := exec.Command("age-keygen").Output()
	if err != nil {
		t.Fatalf("age-keygen failed: %v", err)
	}

	// Parse recipient from output (line starting with "public key:")
	var recipient string
	for _, line := range strings.Split(string(keygenOut), "\n") {
		if strings.HasPrefix(line, "# public key: ") {
			recipient = strings.TrimPrefix(line, "# public key: ")
			break
		}
	}
	if recipient == "" {
		t.Fatal("could not parse public key from age-keygen output")
	}

	// Write identity file
	identityFile := filepath.Join(dir, "key.txt")
	if err := os.WriteFile(identityFile, keygenOut, 0600); err != nil {
		t.Fatalf("writing identity file: %v", err)
	}

	// Write test file
	srcFile := filepath.Join(dir, "test.txt")
	want := "hello vault encryption"
	if err := os.WriteFile(srcFile, []byte(want), 0600); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	// Encrypt
	encFile := filepath.Join(dir, "test.txt.age")
	if err := EncryptFile(recipient, srcFile, encFile); err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}

	// Verify encrypted file exists and differs from original
	encData, err := os.ReadFile(encFile)
	if err != nil {
		t.Fatalf("reading encrypted file: %v", err)
	}
	if string(encData) == want {
		t.Error("encrypted file should differ from plaintext")
	}

	// Decrypt
	decFile := filepath.Join(dir, "test.decrypted.txt")
	if err := DecryptFile(identityFile, encFile, decFile); err != nil {
		t.Fatalf("DecryptFile: %v", err)
	}

	got, err := os.ReadFile(decFile)
	if err != nil {
		t.Fatalf("reading decrypted file: %v", err)
	}
	if string(got) != want {
		t.Errorf("decrypted content = %q, want %q", string(got), want)
	}
}

func TestEncryptFile_BadRecipient(t *testing.T) {
	if _, err := exec.LookPath("age"); err != nil {
		t.Skip("age not installed")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "test.txt")
	os.WriteFile(src, []byte("test"), 0600)
	dst := filepath.Join(dir, "test.age")

	err := EncryptFile("not-a-valid-recipient", src, dst)
	if err == nil {
		t.Fatal("expected error for bad recipient")
	}
}

func TestDecryptFile_WrongKey(t *testing.T) {
	if _, err := exec.LookPath("age"); err != nil {
		t.Skip("age not installed")
	}
	if _, err := exec.LookPath("age-keygen"); err != nil {
		t.Skip("age-keygen not installed")
	}

	dir := t.TempDir()

	// Generate two keypairs
	keygen1, _ := exec.Command("age-keygen").Output()
	keygen2, _ := exec.Command("age-keygen").Output()

	var recipient1 string
	for _, line := range strings.Split(string(keygen1), "\n") {
		if strings.HasPrefix(line, "# public key: ") {
			recipient1 = strings.TrimPrefix(line, "# public key: ")
			break
		}
	}

	// Write identity file for key 2
	idFile2 := filepath.Join(dir, "key2.txt")
	os.WriteFile(idFile2, keygen2, 0600)

	// Encrypt with key 1
	src := filepath.Join(dir, "test.txt")
	os.WriteFile(src, []byte("secret"), 0600)
	enc := filepath.Join(dir, "test.age")
	EncryptFile(recipient1, src, enc)

	// Try to decrypt with key 2
	dec := filepath.Join(dir, "test.dec")
	err := DecryptFile(idFile2, enc, dec)
	if err == nil {
		t.Fatal("expected error when decrypting with wrong key")
	}
}
