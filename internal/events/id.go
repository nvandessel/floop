package events

import (
	"fmt"
	"sync/atomic"
	"time"
)

// eventCounter and sessionCounter provide unique suffixes for generated IDs.
// Combined with nanosecond timestamps, they prevent collisions within a process.
var (
	eventCounter   atomic.Int64
	sessionCounter atomic.Int64
)

// generateEventID produces a unique event ID using timestamp and counter.
func generateEventID() string {
	n := eventCounter.Add(1)
	return fmt.Sprintf("evt-%d-%d", time.Now().UnixNano(), n)
}

// generateSessionID produces a unique session ID using timestamp and counter.
func generateSessionID() string {
	n := sessionCounter.Add(1)
	return fmt.Sprintf("session-%d-%d", time.Now().UnixNano(), n)
}
