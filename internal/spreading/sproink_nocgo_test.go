//go:build !cgo

package spreading

import (
	"strings"
	"testing"
)

func TestNewNativeEngine_NoCGO(t *testing.T) {
	_, err := NewNativeEngine(nil, Config{})
	if err == nil {
		t.Fatal("expected error for non-CGO build")
	}
	if !strings.Contains(err.Error(), "CGO") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNativeEngine_Activate_NoCGO(t *testing.T) {
	e := &NativeEngine{}
	_, err := e.Activate(nil, nil)
	if err == nil {
		t.Fatal("expected error for non-CGO build")
	}
}

func TestNativeEngine_Rebuild_NoCGO(t *testing.T) {
	e := &NativeEngine{}
	err := e.Rebuild(nil)
	if err == nil {
		t.Fatal("expected error for non-CGO build")
	}
}

func TestNativeEngine_Close_NoCGO(t *testing.T) {
	e := &NativeEngine{}
	e.Close() // should not panic
}
