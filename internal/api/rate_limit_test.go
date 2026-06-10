package api

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRateLimiter_AllowsBurstThenBlocks(t *testing.T) {
	fixed := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	l := newRateLimiter(1.0, 3.0) // 1/sec, burst 3
	l.SetClock(func() time.Time { return fixed })
	dev := uuid.New()

	for i := 0; i < 3; i++ {
		if !l.allow(dev) {
			t.Fatalf("call %d should be allowed", i+1)
		}
	}
	if l.allow(dev) {
		t.Fatal("4th call should be denied (bucket empty)")
	}
}

func TestRateLimiter_RefillsOverTime(t *testing.T) {
	tick := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	l := newRateLimiter(1.0, 1.0) // 1/sec, burst 1
	l.SetClock(func() time.Time { return tick })
	dev := uuid.New()

	if !l.allow(dev) {
		t.Fatal("first call should be allowed")
	}
	if l.allow(dev) {
		t.Fatal("second call should be denied (empty)")
	}
	// Advance by 1 second → bucket refills 1 token.
	tick = tick.Add(1 * time.Second)
	l.SetClock(func() time.Time { return tick })
	if !l.allow(dev) {
		t.Fatal("after 1s refill should be allowed")
	}
}

func TestRateLimiter_PerDeviceBuckets_Isolated(t *testing.T) {
	fixed := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	l := newRateLimiter(1.0, 1.0)
	l.SetClock(func() time.Time { return fixed })
	d1 := uuid.New()
	d2 := uuid.New()

	if !l.allow(d1) {
		t.Fatal("d1 first should be allowed")
	}
	if l.allow(d1) {
		t.Fatal("d1 second should be denied")
	}
	// d2 should still have its own full bucket.
	if !l.allow(d2) {
		t.Fatal("d2 should be allowed independently")
	}
}

func TestRateLimiter_CapsAtBurst_NoOverfill(t *testing.T) {
	tick := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	l := newRateLimiter(1.0, 3.0)
	l.SetClock(func() time.Time { return tick })
	dev := uuid.New()

	// Drain the bucket.
	for i := 0; i < 3; i++ {
		_ = l.allow(dev)
	}
	// Wait 100 seconds — refill should cap at burst (3), not 100.
	tick = tick.Add(100 * time.Second)
	l.SetClock(func() time.Time { return tick })
	for i := 0; i < 3; i++ {
		if !l.allow(dev) {
			t.Fatalf("call %d after long refill should be allowed", i+1)
		}
	}
	if l.allow(dev) {
		t.Fatal("4th call after refill should be denied (cap=3)")
	}
}

func TestRateLimiter_ConcurrentCallers_NoRace(t *testing.T) {
	fixed := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	l := newRateLimiter(0.0, 5.0) // no refill; burst only
	l.SetClock(func() time.Time { return fixed })
	dev := uuid.New()

	// 20 goroutines, only 5 should succeed.
	const goroutines = 20
	results := make(chan bool, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() { results <- l.allow(dev) }()
	}
	allowed := 0
	for i := 0; i < goroutines; i++ {
		if <-results {
			allowed++
		}
	}
	if allowed != 5 {
		t.Fatalf("expected exactly 5 to be allowed, got %d", allowed)
	}
}
