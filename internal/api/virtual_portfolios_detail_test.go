package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/api"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage/storagetest"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/virtual"
)

// createPortfolioViaAPI POSTs a portfolio for the given device and returns its id.
func createPortfolioViaAPI(t *testing.T, srv *api.Server, dev uuid.UUID, name string, balance float64) uuid.UUID {
	t.Helper()
	rr := doJSON(t, srv, http.MethodPost, "/v1/virtual/portfolios",
		map[string]any{"name": name, "starting_balance": balance},
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusCreated {
		t.Fatalf("seed portfolio: %d %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&body)
	return uuid.MustParse(body["id"].(string))
}

func TestGetVirtualPortfolioDetail_EmptyTrades_HappyPath_200(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, dev, "Alpha", 10000)

	rr := doJSON(t, srv, http.MethodGet, "/v1/virtual/portfolios/"+pid.String(), nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if body["name"] != "Alpha" {
		t.Fatalf("name: %v", body["name"])
	}
	if body["cash_balance"].(float64) != 10000 {
		t.Fatalf("cash: %v", body["cash_balance"])
	}
	if body["total_value"].(float64) != 10000 {
		t.Fatalf("total: %v", body["total_value"])
	}
	if hh, ok := body["holdings"].([]any); !ok || len(hh) != 0 {
		t.Fatalf("expected empty holdings, got %v", body["holdings"])
	}
}

func TestGetVirtualPortfolioDetail_WithTradesAndPrices(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := seedDeviceForVirtual_api(t, h)
	pid := uuid.New()
	if err := storage.NewVirtualPortfolioRepo(h.Postgres.Pool).Create(context.Background(), virtual.Portfolio{
		ID: pid, DeviceID: dev, Name: "Active", StartingBalance: 10000,
	}); err != nil {
		t.Fatalf("seed portfolio: %v", err)
	}
	tradeRepo := storage.NewVirtualTradeRepo(h.Postgres.Pool)
	if _, err := tradeRepo.Insert(context.Background(), virtual.Trade{
		PortfolioID: pid, Side: virtual.SideBuy, CoinID: "bitcoin",
		Amount: 0.1, Price: 70000, ExecutedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("seed trade: %v", err)
	}

	srv := api.NewServer(
		storage.NewDeviceRepo(h.Postgres.Pool),
		storage.NewAlertRepo(h.Postgres.Pool),
		storage.NewHoldingRepo(h.Postgres.Pool),
		storage.NewVirtualPortfolioRepo(h.Postgres.Pool),
		storage.NewVirtualTradeRepo(h.Postgres.Pool),
		fixedPricing{prices: map[string]virtual.CachedPrice{
			"bitcoin": {Coin: domainCoin("bitcoin", "Bitcoin", 80000)},
		}},
	)

	rr := doJSON(t, srv, http.MethodGet, "/v1/virtual/portfolios/"+pid.String(), nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&body)
	// cash = 10000 - 0.1*70000 = 3000; btc value = 0.1*80000 = 8000;
	// total = 11000; pnl = 11000-10000 = 1000 (10%).
	cash := body["cash_balance"].(float64)
	if cash < 2999.9 || cash > 3000.1 {
		t.Fatalf("cash: %v", body["cash_balance"])
	}
	total := body["total_value"].(float64)
	if total < 10999.9 || total > 11000.1 {
		t.Fatalf("total_value: %v", body["total_value"])
	}
	pnl := body["total_pnl_percent"].(float64)
	if pnl < 9.9 || pnl > 10.1 {
		t.Fatalf("pnl pct: %v", body["total_pnl_percent"])
	}
	holdings := body["holdings"].([]any)
	if len(holdings) != 1 {
		t.Fatalf("expected 1 holding, got %d", len(holdings))
	}
	h0 := holdings[0].(map[string]any)
	if h0["coin_id"] != "bitcoin" {
		t.Fatalf("coin_id: %v", h0["coin_id"])
	}
	if h0["current_price"].(float64) != 80000 {
		t.Fatalf("current_price: %v", h0["current_price"])
	}
	unrealized := h0["unrealized_pnl"].(float64)
	if unrealized < 999.9 || unrealized > 1000.1 {
		t.Fatalf("unrealized: %v", h0["unrealized_pnl"])
	}
}

func TestGetVirtualPortfolioDetail_MarketsDown_502(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := seedDeviceForVirtual_api(t, h)
	pid := uuid.New()
	if err := storage.NewVirtualPortfolioRepo(h.Postgres.Pool).Create(context.Background(), virtual.Portfolio{
		ID: pid, DeviceID: dev, Name: "Active", StartingBalance: 10000,
	}); err != nil {
		t.Fatalf("seed portfolio: %v", err)
	}
	if _, err := storage.NewVirtualTradeRepo(h.Postgres.Pool).Insert(context.Background(), virtual.Trade{
		PortfolioID: pid, Side: virtual.SideBuy, CoinID: "bitcoin",
		Amount: 0.1, Price: 70000, ExecutedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("seed trade: %v", err)
	}
	srv := api.NewServer(
		storage.NewDeviceRepo(h.Postgres.Pool),
		storage.NewAlertRepo(h.Postgres.Pool),
		storage.NewHoldingRepo(h.Postgres.Pool),
		storage.NewVirtualPortfolioRepo(h.Postgres.Pool),
		storage.NewVirtualTradeRepo(h.Postgres.Pool),
		failingPricing{},
	)

	rr := doJSON(t, srv, http.MethodGet, "/v1/virtual/portfolios/"+pid.String(), nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
	var er api.ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&er)
	if er.Error != "upstream_error" {
		t.Fatalf("expected upstream_error, got %q", er.Error)
	}
}

func TestGetVirtualPortfolioDetail_NotFound_404(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)

	rr := doJSON(t, srv, http.MethodGet, "/v1/virtual/portfolios/"+uuid.NewString(), nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestGetVirtualPortfolioDetail_CrossDevice_403(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	d1 := seedDeviceForVirtual_api(t, h)
	d2 := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, d1, "Mine", 10000)

	rr := doJSON(t, srv, http.MethodGet, "/v1/virtual/portfolios/"+pid.String(), nil,
		map[string]string{"X-Device-Id": d2.String()})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestGetVirtualPortfolioDetail_InvalidUUID_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)

	rr := doJSON(t, srv, http.MethodGet, "/v1/virtual/portfolios/not-a-uuid", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDeleteVirtualPortfolio_RemovesRow_204(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, dev, "Goner", 10000)

	rr := doJSON(t, srv, http.MethodDelete, "/v1/virtual/portfolios/"+pid.String(), nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	// confirm gone
	rr2 := doJSON(t, srv, http.MethodGet, "/v1/virtual/portfolios/"+pid.String(), nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rr2.Code)
	}
}

func TestDeleteVirtualPortfolio_Idempotent_204(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)

	rr := doJSON(t, srv, http.MethodDelete, "/v1/virtual/portfolios/"+uuid.NewString(), nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
}

func TestDeleteVirtualPortfolio_CrossDevice_403(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	d1 := seedDeviceForVirtual_api(t, h)
	d2 := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, d1, "Mine", 10000)

	rr := doJSON(t, srv, http.MethodDelete, "/v1/virtual/portfolios/"+pid.String(), nil,
		map[string]string{"X-Device-Id": d2.String()})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}
