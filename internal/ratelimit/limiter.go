// Package ratelimit provides per-key token bucket rate limiting for MCP tools.
package ratelimit

import (
	"fmt"
	"sync"
	"time"
)

// Limiter implements a per-key token bucket rate limiter.
// Each key gets its own bucket with the configured rate and burst.
// It is safe for concurrent use.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64          // tokens per second
	burst   int              // max burst size (also initial token count)
	nowFunc func() time.Time // injectable clock for testing
}

type bucket struct {
	tokens    float64
	lastCheck time.Time
}

// NewLimiter creates a rate limiter with the given rate (tokens/sec) and burst size.
// The burst size also serves as the initial number of tokens available.
func NewLimiter(rate float64, burst int) *Limiter {
	return &Limiter{
		buckets: make(map[string]*bucket),
		rate:    rate,
		burst:   burst,
		nowFunc: time.Now,
	}
}

// Allow checks if a request for the given key should be allowed.
// Returns true if allowed, false if rate limited.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.nowFunc()

	b, ok := l.buckets[key]
	if !ok {
		// First request for this key: start with full burst
		b = &bucket{
			tokens:    float64(l.burst),
			lastCheck: now,
		}
		l.buckets[key] = b
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(b.lastCheck).Seconds()
	if elapsed > 0 {
		b.tokens += l.rate * elapsed
		if b.tokens > float64(l.burst) {
			b.tokens = float64(l.burst)
		}
		b.lastCheck = now
	}

	// Check if we have at least 1 token
	if b.tokens < 1.0 {
		return false
	}

	b.tokens--
	return true
}

// ToolLimiters maps tool names to their rate limiters.
type ToolLimiters map[string]*Limiter

// NewToolLimiters creates the default set of per-tool rate limiters.
// These limits are generous enough for normal usage but prevent abuse.
func NewToolLimiters() ToolLimiters {
	return ToolLimiters{
		"floop_learn":       NewLimiter(10.0/60.0, 3), // 10/minute, burst 3
		"floop_active":      NewLimiter(1.0, 10),      // 60/minute, burst 10
		"floop_backup":      NewLimiter(5.0/60.0, 2),  // 5/minute, burst 2
		"floop_restore":     NewLimiter(5.0/60.0, 2),  // 5/minute, burst 2
		"floop_connect":     NewLimiter(30.0/60.0, 5), // 30/minute, burst 5
		"floop_deduplicate": NewLimiter(5.0/60.0, 1),  // 5/minute, burst 1
		"floop_list":        NewLimiter(1.0, 10),      // 60/minute, burst 10
		"floop_validate":    NewLimiter(10.0/60.0, 5), // 10/minute, burst 5
		"floop_graph":       NewLimiter(30.0/60.0, 5), // 30/minute, burst 5
		"floop_feedback":    NewLimiter(30.0/60.0, 5), // 30/minute, burst 5
	}
}

// CheckLimit checks the rate limit for a given tool name.
// Returns nil if allowed, or an error if rate limited.
// Tools without a configured limiter are always allowed.
func CheckLimit(limiters ToolLimiters, toolName string) error {
	limiter, ok := limiters[toolName]
	if !ok {
		return nil // No limiter configured = no limit
	}

	if !limiter.Allow(toolName) {
		return fmt.Errorf("rate limit exceeded for %s, please try again shortly", toolName)
	}

	return nil
}
