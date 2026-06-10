package api_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/api"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage/storagetest"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/virtual"
)

// approxEqual checks if two floats are approximately equal within a tiny epsilon.
func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// newServerWithPricing lets a test inject a specific pricing implementation.
func newServerWithPricing(t *testing.T, h *storagetest.Harness, p virtualPortfolioPricingForTest) *api.Server {
	t.Helper()
	return api.NewServer(
		storage.NewDeviceRepo(h.Postgres.Pool),
		storage.NewAlertRepo(h.Postgres.Pool),
		storage.NewHoldingRepo(h.Postgres.Pool),
		storage.NewVirtualPortfolioRepo(h.Postgres.Pool),
		storage.NewVirtualTradeRepo(h.Postgres.Pool),
		p,
	)
}

// virtualPortfolioPricingForTest mirrors the package-internal interface so
// test files in the api_test package can satisfy it without leaking the
// interface name.
type virtualPortfolioPricingForTest interface {
	FetchMany(ctx context.Context, coinIDs []string, vsCurrency string) (map[string]virtual.CachedPrice, error)
	FetchOne(ctx context.Context, coinID string) (virtual.CachedPrice, error)
}

func TestQuote_HappyPath_EmptyPortfolio(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := seedDeviceForVirtual_api(t, h)
	srv := newServerWithPricing(t, h, fixedPricing{prices: map[string]virtual.CachedPrice{
		"bitcoin": {Coin: domainCoin("bitcoin", "Bitcoin", 80000), FetchedAt: time.Now().UTC()},
	}})
	pid := createPortfolioViaAPI(t, srv, dev, "Fresh", 10000)

	rr := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+pid.String()+"/quote?coin_id=bitcoin", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if body["coin_id"] != "bitcoin" {
		t.Fatalf("coin_id: %v", body["coin_id"])
	}
	if body["coin_name"] != "Bitcoin" {
		t.Fatalf("coin_name: %v", body["coin_name"])
	}
	if body["price"].(float64) != 80000 {
		t.Fatalf("price: %v", body["price"])
	}
	expected := 10000.0 / 80000.0
	actual := body["max_buy_amount"].(float64)
	if !approxEqual(actual, expected) {
		t.Fatalf("max_buy: expected %v, got %v", expected, actual)
	}
	if body["max_sell_amount"].(float64) != 0 {
		t.Fatalf("max_sell: %v", body["max_sell_amount"])
	}
}

func TestQuote_WithExistingHoldings_MaxSellReflectsAmount(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := seedDeviceForVirtual_api(t, h)
	pid := uuid.New()
	if err := storage.NewVirtualPortfolioRepo(h.Postgres.Pool).Create(context.Background(), virtual.Portfolio{
		ID: pid, DeviceID: dev, Name: "Holding", StartingBalance: 10000,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := storage.NewVirtualTradeRepo(h.Postgres.Pool).Insert(context.Background(), virtual.Trade{
		PortfolioID: pid, Side: virtual.SideBuy, CoinID: "bitcoin",
		Amount: 0.1, Price: 70000, ExecutedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed trade: %v", err)
	}
	srv := newServerWithPricing(t, h, fixedPricing{prices: map[string]virtual.CachedPrice{
		"bitcoin": {Coin: domainCoin("bitcoin", "Bitcoin", 80000), FetchedAt: time.Now().UTC()},
	}})

	rr := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+pid.String()+"/quote?coin_id=bitcoin", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if body["max_sell_amount"].(float64) != 0.1 {
		t.Fatalf("expected max_sell=0.1, got %v", body["max_sell_amount"])
	}
	// Cash = 10000 - 0.1*70000 = 3000; max_buy = 3000/80000.
	expected := 3000.0 / 80000.0
	actual := body["max_buy_amount"].(float64)
	if !approxEqual(actual, expected) {
		t.Fatalf("max_buy: expected %v, got %v", expected, actual)
	}
}

func TestQuote_NotFound_404(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)

	rr := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+uuid.NewString()+"/quote?coin_id=bitcoin", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestQuote_CrossDevice_403(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	d1 := seedDeviceForVirtual_api(t, h)
	d2 := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, d1, "Mine", 10000)

	rr := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+pid.String()+"/quote?coin_id=bitcoin", nil,
		map[string]string{"X-Device-Id": d2.String()})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestQuote_InvalidUUID_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)

	rr := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/not-a-uuid/quote?coin_id=bitcoin", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestQuote_MissingCoinIDQuery_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, dev, "Fresh", 10000)

	rr := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+pid.String()+"/quote", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestQuote_BadCoinIDShape_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, dev, "Fresh", 10000)

	rr := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+pid.String()+"/quote?coin_id=Not_A_Coin!", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestQuote_UnknownCoin_422(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := seedDeviceForVirtual_api(t, h)
	srv := newServerWithPricing(t, h, fixedPricing{prices: map[string]virtual.CachedPrice{
		// only bitcoin known
		"bitcoin": {Coin: domainCoin("bitcoin", "Bitcoin", 80000), FetchedAt: time.Now().UTC()},
	}})
	pid := createPortfolioViaAPI(t, srv, dev, "Fresh", 10000)

	rr := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+pid.String()+"/quote?coin_id=dogecoin", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestQuote_PricingFailure_502(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := seedDeviceForVirtual_api(t, h)
	srv := newServerWithPricing(t, h, failingPricing{})
	pid := createPortfolioViaAPI(t, srv, dev, "Fresh", 10000)

	rr := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+pid.String()+"/quote?coin_id=bitcoin", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
}
