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

func tradeBody(side, coinID string, amount float64) map[string]any {
	return map[string]any{"side": side, "coin_id": coinID, "amount": amount}
}

func TestTrade_Buy_HappyPath_DeductsCashAndAddsHolding(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := seedDeviceForVirtual_api(t, h)
	srv := newServerWithPricing(t, h, fixedPricing{prices: map[string]virtual.CachedPrice{
		"bitcoin": {Coin: domainCoin("bitcoin", "Bitcoin", 80000), FetchedAt: time.Now().UTC()},
	}})
	pid := createPortfolioViaAPI(t, srv, dev, "Active", 10000)

	rr := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+pid.String()+"/trades",
		tradeBody("buy", "bitcoin", 0.05),
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&body)
	trade := body["trade"].(map[string]any)
	if trade["side"] != "buy" || trade["coin_id"] != "bitcoin" {
		t.Fatalf("trade: %v", trade)
	}
	if trade["amount"].(float64) != 0.05 {
		t.Fatalf("amount: %v", trade["amount"])
	}
	if trade["price"].(float64) != 80000 {
		t.Fatalf("price: %v", trade["price"])
	}
	port := body["portfolio"].(map[string]any)
	if port["cash_balance"].(float64) != 10000-0.05*80000 {
		t.Fatalf("cash: %v", port["cash_balance"])
	}
	holdings := port["holdings"].([]any)
	if len(holdings) != 1 {
		t.Fatalf("expected 1 holding, got %d", len(holdings))
	}
	h0 := holdings[0].(map[string]any)
	if h0["coin_id"] != "bitcoin" || h0["amount"].(float64) != 0.05 {
		t.Fatalf("holding: %v", h0)
	}
}

func TestTrade_Sell_HappyPath_AddsCashAndRealizesPnL(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := seedDeviceForVirtual_api(t, h)
	srv := newServerWithPricing(t, h, fixedPricing{prices: map[string]virtual.CachedPrice{
		"bitcoin": {Coin: domainCoin("bitcoin", "Bitcoin", 90000), FetchedAt: time.Now().UTC()},
	}})
	pid := createPortfolioViaAPI(t, srv, dev, "Active", 10000)
	// Buy first.
	buy := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+pid.String()+"/trades",
		tradeBody("buy", "bitcoin", 0.05),
		map[string]string{"X-Device-Id": dev.String()})
	if buy.Code != http.StatusCreated {
		t.Fatalf("buy: %d", buy.Code)
	}
	// Sell half.
	rr := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+pid.String()+"/trades",
		tradeBody("sell", "bitcoin", 0.025),
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&body)
	port := body["portfolio"].(map[string]any)
	// realized = 0.025 * (90000 - 90000) = 0 (bought at 90000, sold at 90000)
	if !approxEqual(port["realized_pnl"].(float64), 0) {
		t.Fatalf("realized: %v", port["realized_pnl"])
	}
	// holding amount = 0.05 - 0.025 = 0.025
	holdings := port["holdings"].([]any)
	if len(holdings) != 1 {
		t.Fatalf("expected 1 holding, got %d", len(holdings))
	}
	h0 := holdings[0].(map[string]any)
	if h0["amount"].(float64) != 0.025 {
		t.Fatalf("amount: %v", h0["amount"])
	}
}

func TestTrade_Buy_InsufficientCash_422(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := seedDeviceForVirtual_api(t, h)
	srv := newServerWithPricing(t, h, fixedPricing{prices: map[string]virtual.CachedPrice{
		"bitcoin": {Coin: domainCoin("bitcoin", "Bitcoin", 80000), FetchedAt: time.Now().UTC()},
	}})
	pid := createPortfolioViaAPI(t, srv, dev, "Active", 100) // tiny balance

	rr := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+pid.String()+"/trades",
		tradeBody("buy", "bitcoin", 1),
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rr.Code)
	}
	var er api.ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&er)
	if er.Detail != "insufficient_cash" {
		t.Fatalf("detail: %q", er.Detail)
	}
}

func TestTrade_Sell_InsufficientHoldings_422(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := seedDeviceForVirtual_api(t, h)
	srv := newServerWithPricing(t, h, fixedPricing{prices: map[string]virtual.CachedPrice{
		"bitcoin": {Coin: domainCoin("bitcoin", "Bitcoin", 80000), FetchedAt: time.Now().UTC()},
	}})
	pid := createPortfolioViaAPI(t, srv, dev, "Active", 10000)

	rr := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+pid.String()+"/trades",
		tradeBody("sell", "bitcoin", 0.5),
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rr.Code)
	}
	var er api.ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&er)
	if er.Detail != "insufficient_holdings" {
		t.Fatalf("detail: %q", er.Detail)
	}
}

func TestTrade_InvalidUUID_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)

	rr := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/not-a-uuid/trades",
		tradeBody("buy", "bitcoin", 1),
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestTrade_BadSide_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, dev, "Active", 10000)

	rr := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+pid.String()+"/trades",
		tradeBody("hodl", "bitcoin", 1),
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestTrade_BadCoinIDShape_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, dev, "Active", 10000)

	rr := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+pid.String()+"/trades",
		tradeBody("buy", "Not_A_Coin!", 1),
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestTrade_AmountZeroOrNegative_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, dev, "Active", 10000)

	for _, amount := range []float64{0, -1} {
		rr := doJSON(t, srv, http.MethodPost,
			"/v1/virtual/portfolios/"+pid.String()+"/trades",
			tradeBody("buy", "bitcoin", amount),
			map[string]string{"X-Device-Id": dev.String()})
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("amount=%v: expected 400, got %d", amount, rr.Code)
		}
	}
}

func TestTrade_NotFound_404(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)

	rr := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+uuid.NewString()+"/trades",
		tradeBody("buy", "bitcoin", 1),
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestTrade_CrossDevice_403(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	d1 := seedDeviceForVirtual_api(t, h)
	d2 := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, d1, "Mine", 10000)

	rr := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+pid.String()+"/trades",
		tradeBody("buy", "bitcoin", 0.01),
		map[string]string{"X-Device-Id": d2.String()})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestTrade_UnknownCoin_422(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := seedDeviceForVirtual_api(t, h)
	srv := newServerWithPricing(t, h, fixedPricing{prices: map[string]virtual.CachedPrice{
		"bitcoin": {Coin: domainCoin("bitcoin", "Bitcoin", 80000), FetchedAt: time.Now().UTC()},
	}})
	pid := createPortfolioViaAPI(t, srv, dev, "Active", 10000)

	rr := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+pid.String()+"/trades",
		tradeBody("buy", "dogecoin", 1),
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rr.Code)
	}
}

func TestTrade_PricingFailure_502(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := seedDeviceForVirtual_api(t, h)
	srv := newServerWithPricing(t, h, failingPricing{})
	pid := createPortfolioViaAPI(t, srv, dev, "Active", 10000)

	rr := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+pid.String()+"/trades",
		tradeBody("buy", "bitcoin", 0.01),
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
}

func TestTrade_PersistsTradeRowToDB(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := seedDeviceForVirtual_api(t, h)
	srv := newServerWithPricing(t, h, fixedPricing{prices: map[string]virtual.CachedPrice{
		"bitcoin": {Coin: domainCoin("bitcoin", "Bitcoin", 80000), FetchedAt: time.Now().UTC()},
	}})
	pid := createPortfolioViaAPI(t, srv, dev, "Active", 10000)

	rr := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+pid.String()+"/trades",
		tradeBody("buy", "bitcoin", 0.05),
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusCreated {
		t.Fatalf("trade: %d", rr.Code)
	}
	// Verify the trade ended up in the DB.
	trades, err := storage.NewVirtualTradeRepo(h.Postgres.Pool).
		ListAllByPortfolio(context.Background(), pid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade in DB, got %d", len(trades))
	}
	if trades[0].Side != virtual.SideBuy || trades[0].CoinID != "bitcoin" || trades[0].Price != 80000 {
		t.Fatalf("trade row: %+v", trades[0])
	}
}

func TestTrade_UpdatesPortfolioUpdatedAt(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := seedDeviceForVirtual_api(t, h)
	srv := newServerWithPricing(t, h, fixedPricing{prices: map[string]virtual.CachedPrice{
		"bitcoin": {Coin: domainCoin("bitcoin", "Bitcoin", 80000), FetchedAt: time.Now().UTC()},
	}})
	pid := createPortfolioViaAPI(t, srv, dev, "Active", 10000)

	before, _ := storage.NewVirtualPortfolioRepo(h.Postgres.Pool).Get(context.Background(), pid)
	time.Sleep(5 * time.Millisecond)

	rr := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+pid.String()+"/trades",
		tradeBody("buy", "bitcoin", 0.01),
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusCreated {
		t.Fatalf("trade: %d", rr.Code)
	}
	after, _ := storage.NewVirtualPortfolioRepo(h.Postgres.Pool).Get(context.Background(), pid)
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("expected updated_at to advance: %v vs %v", before.UpdatedAt, after.UpdatedAt)
	}
}
