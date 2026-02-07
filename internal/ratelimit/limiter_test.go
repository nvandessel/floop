package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestNewLimiter(t *testing.T) {
	l := NewLimiter(10.0, 5)
	if l == nil {
		t.Fatal("NewLimiter returned nil")
	}
	if l.rate != 10.0 {
		t.Errorf("rate = %f, want 10.0", l.rate)
	}
	if l.burst != 5 {
		t.Errorf("burst = %d, want 5", l.burst)
	}
}

func TestAllow_WithinBurst(t *testing.T) {
	l := NewLimiter(1.0, 3)

	// First 3 requests should all be allowed (burst)
	for i := 0; i < 3; i++ {
		if !l.Allow("key1") {
			t.Errorf("request %d should be allowed (within burst)", i+1)
		}
	}
}

func TestAllow_ExceedsBurst(t *testing.T) {
	l := NewLimiter(1.0, 2)

	// Consume entire burst
	l.Allow("key1")
	l.Allow("key1")

	// Next request should be rejected
	if l.Allow("key1") {
		t.Error("request after burst exhaustion should be rejected")
	}
}

func TestAllow_RefillAfterWait(t *testing.T) {
	now := time.Now()
	l := NewLimiter(10.0, 2) // 10 tokens/sec
	l.nowFunc = func() time.Time { return now }

	// Consume burst
	l.Allow("key1")
	l.Allow("key1")

	// Should be rejected
	if l.Allow("key1") {
		t.Error("expected rejection after burst")
	}

	// Advance time by 200ms => 10 * 0.2 = 2 tokens refilled
	now = now.Add(200 * time.Millisecond)

	// Should be allowed now
	if !l.Allow("key1") {
		t.Error("expected allow after token refill")
	}
}

func TestAllow_IndependentKeys(t *testing.T) {
	l := NewLimiter(1.0, 1)

	// Exhaust key1's burst
	l.Allow("key1")
	if l.Allow("key1") {
		t.Error("key1 should be exhausted")
	}

	// key2 should still work independently
	if !l.Allow("key2") {
		t.Error("key2 should be allowed (independent bucket)")
	}
}

func TestAllow_BurstDoesNotExceedMax(t *testing.T) {
	now := time.Now()
	l := NewLimiter(100.0, 3) // High rate, but burst capped at 3
	l.nowFunc = func() time.Time { return now }

	// Exhaust burst
	l.Allow("key1")
	l.Allow("key1")
	l.Allow("key1")

	// Even after waiting a long time, tokens should cap at burst
	now = now.Add(10 * time.Second) // Would refill 1000 tokens uncapped

	// Should only get burst=3 tokens back
	for i := 0; i < 3; i++ {
		if !l.Allow("key1") {
			t.Errorf("request %d should be allowed after refill capped at burst", i+1)
		}
	}
	if l.Allow("key1") {
		t.Error("4th request should be rejected (burst cap)")
	}
}

func TestAllow_PartialTokenRefill(t *testing.T) {
	now := time.Now()
	l := NewLimiter(2.0, 5) // 2 tokens/sec
	l.nowFunc = func() time.Time { return now }

	// Use 3 tokens
	l.Allow("key1")
	l.Allow("key1")
	l.Allow("key1")

	// Advance 250ms => 2*0.25 = 0.5 tokens refilled, total ~2.5
	// (started with 5, used 3 => 2.0 remaining; +0.5 = 2.5)
	now = now.Add(250 * time.Millisecond)

	// Should allow (2.5 tokens available, need 1)
	if !l.Allow("key1") {
		t.Error("expected allow with partial refill")
	}
}

func TestAllow_ZeroRate(t *testing.T) {
	l := NewLimiter(0.0, 2)

	// Initial burst should still work
	if !l.Allow("key1") {
		t.Error("first request should use initial burst")
	}
	if !l.Allow("key1") {
		t.Error("second request should use initial burst")
	}

	// No refill ever (rate=0)
	if l.Allow("key1") {
		t.Error("should be rejected with zero rate")
	}
}

func TestAllow_ConcurrentAccess(t *testing.T) {
	l := NewLimiter(1000.0, 100)

	var wg sync.WaitGroup
	allowed := make(chan bool, 200)

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed <- l.Allow("concurrent-key")
		}()
	}

	wg.Wait()
	close(allowed)

	allowedCount := 0
	for a := range allowed {
		if a {
			allowedCount++
		}
	}

	// With burst=100 and 200 requests, should allow roughly 100
	// Allow some slack for timing
	if allowedCount < 90 || allowedCount > 110 {
		t.Errorf("allowed %d requests, expected ~100 (burst limit)", allowedCount)
	}
}

func TestNewToolLimiters(t *testing.T) {
	limiters := NewToolLimiters()

	expectedTools := []string{
		"floop_learn",
		"floop_active",
		"floop_backup",
		"floop_restore",
		"floop_connect",
		"floop_deduplicate",
		"floop_list",
		"floop_validate",
	}

	for _, tool := range expectedTools {
		if _, ok := limiters[tool]; !ok {
			t.Errorf("missing rate limiter for tool: %s", tool)
		}
	}
}

func TestToolRateLimits(t *testing.T) {
	limiters := NewToolLimiters()

	tests := []struct {
		name  string
		tool  string
		burst int
	}{
		{"learn burst", "floop_learn", 3},
		{"active burst", "floop_active", 10},
		{"backup burst", "floop_backup", 2},
		{"restore burst", "floop_restore", 2},
		{"connect burst", "floop_connect", 5},
		{"deduplicate burst", "floop_deduplicate", 1},
		{"list burst", "floop_list", 10},
		{"validate burst", "floop_validate", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := limiters[tt.tool]
			if limiter.burst != tt.burst {
				t.Errorf("burst = %d, want %d", limiter.burst, tt.burst)
			}
		})
	}
}

func TestCheckLimit(t *testing.T) {
	limiters := NewToolLimiters()

	// Should pass for a known tool
	err := CheckLimit(limiters, "floop_learn")
	if err != nil {
		t.Errorf("unexpected error for floop_learn: %v", err)
	}

	// Unknown tool should pass (no limiter = no limit)
	err = CheckLimit(limiters, "unknown_tool")
	if err != nil {
		t.Errorf("unexpected error for unknown tool: %v", err)
	}

	// Exhaust floop_deduplicate (burst=1)
	CheckLimit(limiters, "floop_deduplicate")
	err = CheckLimit(limiters, "floop_deduplicate")
	if err == nil {
		t.Error("expected rate limit error after burst exhaustion")
	}
}
