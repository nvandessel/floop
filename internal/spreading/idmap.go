package spreading

import "fmt"

// IDMap provides a bidirectional mapping between UUID strings and contiguous
// uint32 IDs used by the sproink FFI layer. It is not safe for concurrent use;
// all writes must complete before any concurrent reads.
type IDMap struct {
	toU32  map[string]uint32
	toUUID []string
}

// NewIDMap returns an empty IDMap ready for use.
func NewIDMap() *IDMap {
	return &IDMap{
		toU32: make(map[string]uint32),
	}
}

// GetOrAssign returns the uint32 ID for uuid, assigning the next sequential ID
// if the uuid has not been seen before.
func (m *IDMap) GetOrAssign(uuid string) uint32 {
	if id, ok := m.toU32[uuid]; ok {
		return id
	}
	id := uint32(len(m.toUUID))
	m.toU32[uuid] = id
	m.toUUID = append(m.toUUID, uuid)
	return id
}

// ToU32 returns the uint32 ID for uuid and true, or 0 and false if the uuid is
// unknown.
func (m *IDMap) ToU32(uuid string) (uint32, bool) {
	id, ok := m.toU32[uuid]
	return id, ok
}

// ToUUID returns the UUID string for the given uint32 ID. It panics if id is
// out of range.
func (m *IDMap) ToUUID(id uint32) string {
	if int(id) >= len(m.toUUID) {
		panic(fmt.Sprintf("IDMap: id %d out of range [0, %d)", id, len(m.toUUID)))
	}
	return m.toUUID[id]
}

// Len returns the number of mapped IDs.
func (m *IDMap) Len() int {
	return len(m.toUUID)
}
