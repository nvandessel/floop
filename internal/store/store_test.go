package store

import (
	"errors"
	"testing"
)

func TestDuplicateContentError_Error(t *testing.T) {
	err := &DuplicateContentError{ExistingID: "bhv-123"}
	want := "duplicate content: behavior bhv-123 has identical canonical content"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestDuplicateContentError_Is(t *testing.T) {
	err := &DuplicateContentError{ExistingID: "bhv-456"}
	if !errors.Is(err, ErrDuplicateContent) {
		t.Error("expected errors.Is(err, ErrDuplicateContent) to be true")
	}
}

func TestDuplicateContentError_As(t *testing.T) {
	var wrapped error = &DuplicateContentError{ExistingID: "bhv-789"}
	var dupErr *DuplicateContentError
	if !errors.As(wrapped, &dupErr) {
		t.Fatal("expected errors.As to succeed")
	}
	if dupErr.ExistingID != "bhv-789" {
		t.Errorf("ExistingID = %q, want %q", dupErr.ExistingID, "bhv-789")
	}
}
