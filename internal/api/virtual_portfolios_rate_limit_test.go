package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/api"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage/storagetest"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/virtual"
)

func TestTrade_RateLimit_Returns429AfterBurst(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := seedDeviceForVirtual_api(t, h)
	srv := newServerWithPricing(t, h, fixedPricing{prices: map[string]virtual.CachedPrice{
		"bitcoin": {Coin: domainCoin("bitcoin", "Bitcoin", 80000), FetchedAt: time.Now().UTC()},
	}})
	// Freeze the limiter clock so refills don't happen during the test.
	fixed := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	srv.TradesLimiter.SetClock(func() time.Time { return fixed })

	pid := createPortfolioViaAPI(t, srv, dev, "Active", 1_000_000) // big enough for 20 buys

	body := map[string]any{"side": "buy", "coin_id": "bitcoin", "amount": 0.0001}
	// Burst is 20 — first 20 should succeed.
	for i := 0; i < 20; i++ {
		rr := doJSON(t, srv, http.MethodPost,
			"/v1/virtual/portfolios/"+pid.String()+"/trades",
			body, map[string]string{"X-Device-Id": dev.String()})
		if rr.Code != http.StatusCreated {
			t.Fatalf("call %d: expected 201, got %d", i+1, rr.Code)
		}
	}
	// 21st call should be rate-limited.
	rr := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+pid.String()+"/trades",
		body, map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rr.Code, rr.Body.String())
	}
	var er api.ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&er)
	if er.Error != "rate_limited" {
		t.Fatalf("expected rate_limited, got %q", er.Error)
	}
}
