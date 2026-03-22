package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFile(t *testing.T) {
	tests := []struct {
		name      string
		writeFn   func(f *os.File) error
		wantErr   bool
		wantData  string
		preCreate bool // create the target file before writing
	}{
		{
			name: "success writes content",
			writeFn: func(f *os.File) error {
				_, err := f.WriteString("hello\n")
				return err
			},
			wantData: "hello\n",
		},
		{
			name:      "overwrites existing file atomically",
			preCreate: true,
			writeFn: func(f *os.File) error {
				_, err := f.WriteString("new content\n")
				return err
			},
			wantData: "new content\n",
		},
		{
			name: "writeFn error leaves no partial file",
			writeFn: func(f *os.File) error {
				f.WriteString("partial")
				return errors.New("simulated write error")
			},
			wantErr: true,
		},
		{
			name: "writeFn error preserves existing file",
			writeFn: func(f *os.File) error {
				return errors.New("simulated write error")
			},
			preCreate: true,
			wantErr:   true,
			wantData:  "original\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "test.jsonl")

			if tt.preCreate {
				if err := os.WriteFile(target, []byte("original\n"), 0644); err != nil {
					t.Fatalf("failed to pre-create file: %v", err)
				}
			}

			err := atomicWriteFile(target, tt.writeFn)
			if (err != nil) != tt.wantErr {
				t.Errorf("atomicWriteFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantData != "" {
				got, err := os.ReadFile(target)
				if err != nil {
					t.Fatalf("failed to read target file: %v", err)
				}
				if string(got) != tt.wantData {
					t.Errorf("file content = %q, want %q", string(got), tt.wantData)
				}
			}

			if tt.wantErr && !tt.preCreate {
				// Target should not exist if there was no pre-existing file
				if _, err := os.Stat(target); !os.IsNotExist(err) {
					t.Errorf("expected target file to not exist after error, got err = %v", err)
				}
			}

			// Verify no temp files left behind
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("failed to read dir: %v", err)
			}
			for _, e := range entries {
				if e.Name() != "test.jsonl" {
					t.Errorf("unexpected leftover file: %s", e.Name())
				}
			}
		})
	}
}

func TestAtomicWriteFile_InvalidDir(t *testing.T) {
	err := atomicWriteFile("/nonexistent/dir/file.jsonl", func(f *os.File) error {
		return nil
	})
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}
