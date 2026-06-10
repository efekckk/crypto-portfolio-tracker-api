package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// tokenBucket is one device's rate-limit state.
type tokenBucket struct {
	tokens   float64
	lastFill time.Time
}

// rateLimiter tracks per-device-id token buckets in memory. Refills at
// `Rate` tokens per second up to `Burst`. Safe for concurrent use.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[uuid.UUID]*tokenBucket
	rate    float64 // tokens per second
	burst   float64 // bucket capacity
	now     func() time.Time
}

// newRateLimiter builds a limiter with the given rate (tokens/sec) and burst
// (max tokens). The clock is injectable so tests can advance time without
// sleeping.
func newRateLimiter(rate, burst float64) *rateLimiter {
	return &rateLimiter{
		buckets: map[uuid.UUID]*tokenBucket{},
		rate:    rate,
		burst:   burst,
		now:     time.Now,
	}
}

// SetClock overrides the time source. Test-only.
func (r *rateLimiter) SetClock(now func() time.Time) {
	r.now = now
}

// allow consumes one token for the device. Returns true if the request is
// allowed; false if the bucket is empty.
func (r *rateLimiter) allow(deviceID uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	b, ok := r.buckets[deviceID]
	if !ok {
		b = &tokenBucket{tokens: r.burst, lastFill: now}
		r.buckets[deviceID] = b
	}
	// Refill based on elapsed time.
	elapsed := now.Sub(b.lastFill).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * r.rate
		if b.tokens > r.burst {
			b.tokens = r.burst
		}
		b.lastFill = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// rateLimitTrades is the chi middleware factory that wraps the trade
// endpoint. The bucket key is the requesting device id (resolved by
// requireDeviceID upstream so DeviceFromContext is safe to call).
func rateLimitTrades(limiter *rateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			dev := DeviceFromContext(r.Context())
			if !limiter.allow(dev.DeviceID) {
				writeError(w, http.StatusTooManyRequests, "rate_limited",
					"too many trade requests; try again shortly")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
