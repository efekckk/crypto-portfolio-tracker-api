package virtual

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
)

// stubMarkets is a minimal MarketsClient implementation for the cache
// tests. It records call counts and lets the test rig latency to exercise
// the singleflight path.
type stubMarkets struct {
	mu        sync.Mutex
	calls     int32
	result    []domain.Coin
	err       error
	delay     time.Duration
	lastIDs   []string
	currency  string
}

func (s *stubMarkets) FetchMarkets(ctx context.Context, ids []string, vsCurrency string) ([]domain.Coin, error) {
	atomic.AddInt32(&s.calls, 1)
	s.mu.Lock()
	s.lastIDs = append([]string(nil), ids...)
	s.currency = vsCurrency
	s.mu.Unlock()
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func newSvc(t *testing.T, m *stubMarkets) *PricingService {
	t.Helper()
	svc := NewPricingService(m, "usd")
	return svc
}

func TestPricingService_FetchOne_CacheHitSkipsUpstream(t *testing.T) {
	m := &stubMarkets{result: []domain.Coin{{ID: "bitcoin", Name: "Bitcoin", CurrentPrice: 75000}}}
	svc := newSvc(t, m)

	frozen := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return frozen })

	first, err := svc.FetchOne(context.Background(), "bitcoin")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first.Coin.ID != "bitcoin" || first.Coin.CurrentPrice != 75000 {
		t.Fatalf("first.Coin: %+v", first.Coin)
	}
	if m.calls != 1 {
		t.Fatalf("expected 1 upstream call, got %d", m.calls)
	}

	// Still within TTL → no upstream call.
	second, err := svc.FetchOne(context.Background(), "bitcoin")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if m.calls != 1 {
		t.Fatalf("expected 1 upstream call (cache hit), got %d", m.calls)
	}
	if second.FetchedAt != first.FetchedAt {
		t.Fatalf("expected same FetchedAt on cache hit, got %v vs %v", second.FetchedAt, first.FetchedAt)
	}
}

func TestPricingService_FetchOne_CacheMissAfterTTL(t *testing.T) {
	m := &stubMarkets{result: []domain.Coin{{ID: "bitcoin", CurrentPrice: 75000}}}
	svc := newSvc(t, m)

	tick := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return tick })

	if _, err := svc.FetchOne(context.Background(), "bitcoin"); err != nil {
		t.Fatalf("warm: %v", err)
	}
	// Advance past TTL.
	tick = tick.Add(PriceTTL + time.Millisecond)
	if _, err := svc.FetchOne(context.Background(), "bitcoin"); err != nil {
		t.Fatalf("post-ttl: %v", err)
	}
	if m.calls != 2 {
		t.Fatalf("expected 2 upstream calls, got %d", m.calls)
	}
}

func TestPricingService_FetchOne_UpstreamErrorPropagates(t *testing.T) {
	m := &stubMarkets{err: errors.New("upstream boom")}
	svc := newSvc(t, m)

	_, err := svc.FetchOne(context.Background(), "bitcoin")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPricingService_FetchOne_UnknownCoin_ReturnsError(t *testing.T) {
	m := &stubMarkets{result: []domain.Coin{}} // empty markets response
	svc := newSvc(t, m)

	_, err := svc.FetchOne(context.Background(), "made-up-coin")
	if err == nil {
		t.Fatal("expected error for missing coin")
	}
}

func TestPricingService_FetchOne_Concurrent_CollapsesToOneUpstream(t *testing.T) {
	m := &stubMarkets{
		result: []domain.Coin{{ID: "bitcoin", CurrentPrice: 75000}},
		delay:  20 * time.Millisecond, // ensures concurrency overlaps
	}
	svc := newSvc(t, m)

	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.FetchOne(context.Background(), "bitcoin")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent fetch: %v", err)
		}
	}
	if atomic.LoadInt32(&m.calls) != 1 {
		t.Fatalf("expected 1 upstream call (singleflight), got %d", m.calls)
	}
}

func TestPricingService_FetchOne_EmptyCoinID_Rejected(t *testing.T) {
	svc := newSvc(t, &stubMarkets{})
	if _, err := svc.FetchOne(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty coin_id")
	}
}

func TestPricingService_FetchMany_MixedHitMiss_OneUpstreamForMisses(t *testing.T) {
	m := &stubMarkets{result: []domain.Coin{
		{ID: "bitcoin", CurrentPrice: 75000},
		{ID: "ethereum", CurrentPrice: 4000},
	}}
	svc := newSvc(t, m)

	tick := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return tick })

	// Warm bitcoin only.
	if _, err := svc.FetchOne(context.Background(), "bitcoin"); err != nil {
		t.Fatalf("warm: %v", err)
	}
	if m.calls != 1 {
		t.Fatalf("expected 1 call after warm, got %d", m.calls)
	}

	// FetchMany([bitcoin, ethereum]) — bitcoin hit, ethereum miss → 1 more call.
	got, err := svc.FetchMany(context.Background(), []string{"bitcoin", "ethereum"}, "")
	if err != nil {
		t.Fatalf("many: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if m.calls != 2 {
		t.Fatalf("expected 2 total upstream calls, got %d", m.calls)
	}
	// Verify the second upstream call asked only for the miss.
	m.mu.Lock()
	lastIDs := m.lastIDs
	m.mu.Unlock()
	if len(lastIDs) != 1 || lastIDs[0] != "ethereum" {
		t.Fatalf("expected misses=[ethereum], got %v", lastIDs)
	}
}

func TestPricingService_FetchMany_EmptyInput_NoUpstream(t *testing.T) {
	m := &stubMarkets{}
	svc := newSvc(t, m)

	got, err := svc.FetchMany(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("many: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %d", len(got))
	}
	if m.calls != 0 {
		t.Fatalf("expected 0 upstream calls, got %d", m.calls)
	}
}

func TestPricingService_FetchMany_AllHits_NoUpstream(t *testing.T) {
	m := &stubMarkets{result: []domain.Coin{
		{ID: "bitcoin", CurrentPrice: 75000},
	}}
	svc := newSvc(t, m)

	if _, err := svc.FetchOne(context.Background(), "bitcoin"); err != nil {
		t.Fatalf("warm: %v", err)
	}
	got, err := svc.FetchMany(context.Background(), []string{"bitcoin"}, "")
	if err != nil {
		t.Fatalf("many: %v", err)
	}
	if got["bitcoin"].Coin.CurrentPrice != 75000 {
		t.Fatalf("expected cached price, got %+v", got["bitcoin"])
	}
	if m.calls != 1 {
		t.Fatalf("expected 1 call total (the warm), got %d", m.calls)
	}
}
