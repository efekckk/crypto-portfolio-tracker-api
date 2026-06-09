package virtual

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
)

// MarketsClient fetches market snapshots. This mirrors the interface in
// eval/coingecko.go but avoids an import cycle.
type MarketsClient interface {
	FetchMarkets(ctx context.Context, ids []string, vsCurrency string) ([]domain.Coin, error)
}

// PriceTTL is how long a fetched price is considered fresh enough to reuse
// without hitting CoinGecko again. Trade endpoints care about "current"
// price but consecutive trades within a couple of seconds shouldn't burn
// quota.
const PriceTTL = 2 * time.Second

// CachedPrice is what the cache returns. It includes the timestamp the
// price was fetched so callers can surface FetchedAt to the client.
type CachedPrice struct {
	Coin      domain.Coin
	FetchedAt time.Time
}

// PricingService wraps a MarketsClient with a per-coin TTL cache and a
// singleflight gate so concurrent fetches for the same coin collapse into
// one upstream call.
type PricingService struct {
	markets  MarketsClient
	currency string

	mu    sync.Mutex
	cache map[string]CachedPrice

	group singleflight.Group

	// clock returns the current time. Tests inject a fake to advance time
	// without sleeping.
	clock func() time.Time
}

// NewPricingService builds a service around the given markets client. The
// currency is forwarded on every upstream fetch (e.g. "usd").
func NewPricingService(markets MarketsClient, currency string) *PricingService {
	if currency == "" {
		currency = "usd"
	}
	return &PricingService{
		markets:  markets,
		currency: currency,
		cache:    map[string]CachedPrice{},
		clock:    time.Now,
	}
}

// SetClock overrides the time source. Test-only.
func (s *PricingService) SetClock(now func() time.Time) {
	s.clock = now
}

// FetchOne returns a CachedPrice for the given coin id. A cache hit
// (within PriceTTL) skips the upstream call entirely. A cache miss is
// coalesced via singleflight so N concurrent callers share one fetch.
func (s *PricingService) FetchOne(ctx context.Context, coinID string) (CachedPrice, error) {
	if coinID == "" {
		return CachedPrice{}, errors.New("virtual: empty coin_id")
	}
	now := s.clock()

	s.mu.Lock()
	if entry, ok := s.cache[coinID]; ok && now.Sub(entry.FetchedAt) < PriceTTL {
		s.mu.Unlock()
		return entry, nil
	}
	s.mu.Unlock()

	v, err, _ := s.group.Do(coinID, func() (any, error) {
		coins, err := s.markets.FetchMarkets(ctx, []string{coinID}, s.currency)
		if err != nil {
			return CachedPrice{}, fmt.Errorf("virtual: fetch markets: %w", err)
		}
		if len(coins) == 0 {
			return CachedPrice{}, fmt.Errorf("virtual: coin %q not in markets response", coinID)
		}
		entry := CachedPrice{Coin: coins[0], FetchedAt: s.clock()}
		s.mu.Lock()
		s.cache[coinID] = entry
		s.mu.Unlock()
		return entry, nil
	})
	if err != nil {
		return CachedPrice{}, err
	}
	return v.(CachedPrice), nil
}

// FetchMany returns prices for several coins at once. Cache hits short-
// circuit; misses are batched into a single upstream FetchMarkets call.
// The returned map keys the coin id; missing ids (not in the markets
// response) are absent from the map.
func (s *PricingService) FetchMany(ctx context.Context, coinIDs []string, vsCurrency string) (map[string]CachedPrice, error) {
	if len(coinIDs) == 0 {
		return map[string]CachedPrice{}, nil
	}
	if vsCurrency == "" {
		vsCurrency = s.currency
	}
	now := s.clock()
	out := make(map[string]CachedPrice, len(coinIDs))
	var misses []string

	s.mu.Lock()
	for _, id := range coinIDs {
		if entry, ok := s.cache[id]; ok && now.Sub(entry.FetchedAt) < PriceTTL {
			out[id] = entry
		} else {
			misses = append(misses, id)
		}
	}
	s.mu.Unlock()

	if len(misses) == 0 {
		return out, nil
	}

	coins, err := s.markets.FetchMarkets(ctx, misses, vsCurrency)
	if err != nil {
		return nil, fmt.Errorf("virtual: fetch markets: %w", err)
	}
	fetched := s.clock()
	s.mu.Lock()
	for _, c := range coins {
		entry := CachedPrice{Coin: c, FetchedAt: fetched}
		s.cache[c.ID] = entry
		out[c.ID] = entry
	}
	s.mu.Unlock()
	return out, nil
}
