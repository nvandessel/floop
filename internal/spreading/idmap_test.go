package spreading

import (
	"fmt"
	"testing"
)

func TestIDMap_Roundtrip(t *testing.T) {
	m := NewIDMap()
	id := m.GetOrAssign("abc-123")
	got := m.ToUUID(id)
	if got != "abc-123" {
		t.Fatalf("ToUUID(%d) = %q, want %q", id, got, "abc-123")
	}
}

func TestIDMap_Contiguous(t *testing.T) {
	m := NewIDMap()
	uuids := []string{"alpha", "bravo", "charlie", "delta"}
	for i, uuid := range uuids {
		id := m.GetOrAssign(uuid)
		if id != uint32(i) {
			t.Fatalf("GetOrAssign(%q) = %d, want %d", uuid, id, i)
		}
	}
}

func TestIDMap_Idempotent(t *testing.T) {
	m := NewIDMap()
	first := m.GetOrAssign("same-uuid")
	second := m.GetOrAssign("same-uuid")
	if first != second {
		t.Fatalf("GetOrAssign returned %d then %d for same UUID", first, second)
	}
}

func TestIDMap_ToU32_Unknown(t *testing.T) {
	m := NewIDMap()
	m.GetOrAssign("known")
	_, ok := m.ToU32("unknown")
	if ok {
		t.Fatal("ToU32 returned true for unknown UUID")
	}
}

func TestIDMap_ToUUID_OutOfRange(t *testing.T) {
	m := NewIDMap()
	m.GetOrAssign("only-one")

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("ToUUID did not panic on out-of-range ID")
		}
	}()
	m.ToUUID(99)
}

func TestIDMap_Len(t *testing.T) {
	m := NewIDMap()
	if m.Len() != 0 {
		t.Fatalf("Len() = %d for empty map, want 0", m.Len())
	}
	for i := 0; i < 5; i++ {
		m.GetOrAssign(fmt.Sprintf("uuid-%d", i))
	}
	if m.Len() != 5 {
		t.Fatalf("Len() = %d, want 5", m.Len())
	}
}
