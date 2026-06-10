package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage/storagetest"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/virtual"
	"github.com/google/uuid"
)

// mutablePricing is a pricing stub that supports in-test price updates so
// the test can simulate market moves between trades.
type mutablePricing struct {
	prices map[string]virtual.CachedPrice
}

func (m *mutablePricing) set(coinID string, coin domain.Coin) {
	m.prices[coinID] = virtual.CachedPrice{Coin: coin, FetchedAt: time.Now().UTC()}
}

func (m *mutablePricing) FetchMany(_ context.Context, ids []string, _ string) (map[string]virtual.CachedPrice, error) {
	out := map[string]virtual.CachedPrice{}
	for _, id := range ids {
		if cp, ok := m.prices[id]; ok {
			out[id] = cp
		}
	}
	return out, nil
}

func (m *mutablePricing) FetchOne(_ context.Context, coinID string) (virtual.CachedPrice, error) {
	if cp, ok := m.prices[coinID]; ok {
		return cp, nil
	}
	return virtual.CachedPrice{}, fmt.Errorf("virtual: coin %q not in markets response", coinID)
}

// TestE2E_VirtualPortfolio_FullJourney walks the entire virtual portfolio flow
// the iOS client uses: register device, create two portfolios, quote a coin, buy,
// mark-to-market with a price move, buy another asset, sell at profit, paginate
// trade history, verify portfolio stats, hit the 5-portfolio cap, delete, and
// confirm 404 on deleted portfolio access.
func TestE2E_VirtualPortfolio_FullJourney(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)

	pricing := &mutablePricing{
		prices: map[string]virtual.CachedPrice{
			"bitcoin":  {Coin: domainCoin("bitcoin", "Bitcoin", 80000), FetchedAt: time.Now().UTC()},
			"ethereum": {Coin: domainCoin("ethereum", "Ethereum", 3000), FetchedAt: time.Now().UTC()},
		},
	}
	srv := newServerWithPricing(t, h, pricing)

	// --- 1. Register the device.
	dev := uuid.New()
	rrReg := doJSON(t, srv, http.MethodPost, "/v1/devices",
		map[string]any{
			"device_id":  dev.String(),
			"apns_token": "tok",
			"apns_env":   "development",
			"locale":     "en",
		}, nil)
	if rrReg.Code != http.StatusCreated {
		t.Fatalf("register device: %d", rrReg.Code)
	}
	hdr := map[string]string{"X-Device-Id": dev.String()}

	// --- 2. List virtual portfolios — empty.
	rrEmpty := doJSON(t, srv, http.MethodGet, "/v1/virtual/portfolios", nil, hdr)
	if rrEmpty.Code != http.StatusOK {
		t.Fatalf("list empty: %d", rrEmpty.Code)
	}
	var emptyList struct {
		Portfolios []any `json:"portfolios"`
	}
	_ = json.NewDecoder(rrEmpty.Body).Decode(&emptyList)
	if len(emptyList.Portfolios) != 0 {
		t.Fatalf("expected empty list, got %d", len(emptyList.Portfolios))
	}

	// --- 3. Create two portfolios.
	conservativeID := postCreatePortfolio(t, srv, hdr, "Conservative", 10000)
	aggressiveID := postCreatePortfolio(t, srv, hdr, "Aggressive", 100000)

	// --- 4. List portfolios — both visible, zero trades, cash == starting.
	rrList1 := doJSON(t, srv, http.MethodGet, "/v1/virtual/portfolios", nil, hdr)
	var list1 struct {
		Portfolios []struct {
			ID              string  `json:"id"`
			Name            string  `json:"name"`
			CashBalance     float64 `json:"cash_balance"`
			TotalValue      float64 `json:"total_value"`
			TotalPnL        float64 `json:"total_pnl"`
			TotalPnLPercent float64 `json:"total_pnl_percent"`
			TradeCount      int     `json:"trade_count"`
		} `json:"portfolios"`
	}
	_ = json.NewDecoder(rrList1.Body).Decode(&list1)
	if len(list1.Portfolios) != 2 {
		t.Fatalf("expected 2 portfolios, got %d", len(list1.Portfolios))
	}
	for _, p := range list1.Portfolios {
		if p.TradeCount != 0 || p.TotalPnL != 0 || p.CashBalance != p.TotalValue {
			t.Fatalf("fresh portfolio inconsistent: %+v", p)
		}
	}

	// --- 5. Quote bitcoin for Aggressive.
	rrQuote := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+aggressiveID.String()+"/quote?coin_id=bitcoin", nil, hdr)
	if rrQuote.Code != http.StatusOK {
		t.Fatalf("quote: %d", rrQuote.Code)
	}
	var quote struct {
		Price        float64 `json:"price"`
		MaxBuyAmount float64 `json:"max_buy_amount"`
	}
	_ = json.NewDecoder(rrQuote.Body).Decode(&quote)
	if quote.Price != 80000 {
		t.Fatalf("quote price: %v", quote.Price)
	}
	if quote.MaxBuyAmount != 100000.0/80000.0 {
		t.Fatalf("max_buy_amount: %v", quote.MaxBuyAmount)
	}

	// --- 6. Buy 0.5 BTC on Aggressive at 80k → cost 40k.
	rrBuy1 := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+aggressiveID.String()+"/trades",
		map[string]any{"side": "buy", "coin_id": "bitcoin", "amount": 0.5}, hdr)
	if rrBuy1.Code != http.StatusCreated {
		t.Fatalf("buy 1: %d %s", rrBuy1.Code, rrBuy1.Body.String())
	}

	// --- 7. Bitcoin pumps to 100k → check detail.
	pricing.set("bitcoin", domainCoin("bitcoin", "Bitcoin", 100000))

	rrDetail := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+aggressiveID.String(), nil, hdr)
	if rrDetail.Code != http.StatusOK {
		t.Fatalf("detail after pump: %d", rrDetail.Code)
	}
	var detail struct {
		CashBalance   float64 `json:"cash_balance"`
		TotalValue    float64 `json:"total_value"`
		UnrealizedPnL float64 `json:"unrealized_pnl"`
		Holdings      []struct {
			CoinID string  `json:"coin_id"`
			Amount float64 `json:"amount"`
		} `json:"holdings"`
	}
	_ = json.NewDecoder(rrDetail.Body).Decode(&detail)
	if detail.CashBalance != 60000 {
		t.Fatalf("cash after buy: %v", detail.CashBalance)
	}
	if detail.UnrealizedPnL != 0.5*(100000-80000) {
		t.Fatalf("unrealized: %v", detail.UnrealizedPnL)
	}
	if len(detail.Holdings) != 1 || detail.Holdings[0].CoinID != "bitcoin" || detail.Holdings[0].Amount != 0.5 {
		t.Fatalf("holdings: %+v", detail.Holdings)
	}

	// --- 8. Buy 2 ETH at 3k → cost 6k.
	rrBuy2 := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+aggressiveID.String()+"/trades",
		map[string]any{"side": "buy", "coin_id": "ethereum", "amount": 2.0}, hdr)
	if rrBuy2.Code != http.StatusCreated {
		t.Fatalf("buy 2: %d", rrBuy2.Code)
	}

	// --- 9. Sell 0.25 BTC at 100k → realized = 0.25*(100000-80000) = 5000.
	rrSell := doJSON(t, srv, http.MethodPost,
		"/v1/virtual/portfolios/"+aggressiveID.String()+"/trades",
		map[string]any{"side": "sell", "coin_id": "bitcoin", "amount": 0.25}, hdr)
	if rrSell.Code != http.StatusCreated {
		t.Fatalf("sell: %d", rrSell.Code)
	}
	var sellResp struct {
		Portfolio struct {
			RealizedPnL float64 `json:"realized_pnl"`
		} `json:"portfolio"`
	}
	_ = json.NewDecoder(rrSell.Body).Decode(&sellResp)
	if sellResp.Portfolio.RealizedPnL < 4999.99 || sellResp.Portfolio.RealizedPnL > 5000.01 {
		t.Fatalf("realized pnl: %v", sellResp.Portfolio.RealizedPnL)
	}

	// --- 10. Trade history page 1 (limit=2) → 2 newest trades + next cursor.
	rrHist1 := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+aggressiveID.String()+"/trades?limit=2", nil, hdr)
	if rrHist1.Code != http.StatusOK {
		t.Fatalf("hist page 1: %d", rrHist1.Code)
	}
	var hist1 struct {
		Trades []struct {
			ID     int64   `json:"id"`
			Side   string  `json:"side"`
			CoinID string  `json:"coin_id"`
			Amount float64 `json:"amount"`
		} `json:"trades"`
		NextCursor *int64 `json:"next_cursor"`
	}
	_ = json.NewDecoder(rrHist1.Body).Decode(&hist1)
	if len(hist1.Trades) != 2 {
		t.Fatalf("page 1 len: %d", len(hist1.Trades))
	}
	if hist1.NextCursor == nil {
		t.Fatal("page 1 should have a cursor")
	}
	// Newest first: the sell, then the ETH buy.
	if hist1.Trades[0].Side != "sell" || hist1.Trades[0].CoinID != "bitcoin" {
		t.Fatalf("page 1 [0]: %+v", hist1.Trades[0])
	}
	if hist1.Trades[1].Side != "buy" || hist1.Trades[1].CoinID != "ethereum" {
		t.Fatalf("page 1 [1]: %+v", hist1.Trades[1])
	}

	// --- 11. Trade history page 2 (using cursor) → BTC buy + nil cursor.
	rrHist2 := doJSON(t, srv, http.MethodGet,
		fmt.Sprintf("/v1/virtual/portfolios/%s/trades?limit=2&before_id=%d",
			aggressiveID, *hist1.NextCursor), nil, hdr)
	if rrHist2.Code != http.StatusOK {
		t.Fatalf("hist page 2: %d", rrHist2.Code)
	}
	var hist2 struct {
		Trades []struct {
			Side   string `json:"side"`
			CoinID string `json:"coin_id"`
		} `json:"trades"`
		NextCursor *int64 `json:"next_cursor"`
	}
	_ = json.NewDecoder(rrHist2.Body).Decode(&hist2)
	if len(hist2.Trades) != 1 || hist2.Trades[0].Side != "buy" || hist2.Trades[0].CoinID != "bitcoin" {
		t.Fatalf("page 2: %+v", hist2.Trades)
	}
	if hist2.NextCursor != nil {
		t.Fatalf("expected nil cursor on last page, got %v", hist2.NextCursor)
	}

	// --- 12. List portfolios — Aggressive now has 3 trades + positive total_pnl.
	rrList2 := doJSON(t, srv, http.MethodGet, "/v1/virtual/portfolios", nil, hdr)
	var list2 struct {
		Portfolios []struct {
			Name       string  `json:"name"`
			TradeCount int     `json:"trade_count"`
			TotalPnL   float64 `json:"total_pnl"`
		} `json:"portfolios"`
	}
	_ = json.NewDecoder(rrList2.Body).Decode(&list2)
	var agg *struct {
		Name       string  `json:"name"`
		TradeCount int     `json:"trade_count"`
		TotalPnL   float64 `json:"total_pnl"`
	}
	for i := range list2.Portfolios {
		if list2.Portfolios[i].Name == "Aggressive" {
			agg = &list2.Portfolios[i]
		}
	}
	if agg == nil {
		t.Fatal("aggressive portfolio missing from list")
	}
	if agg.TradeCount != 3 {
		t.Fatalf("expected 3 trades, got %d", agg.TradeCount)
	}
	if agg.TotalPnL <= 0 {
		t.Fatalf("expected positive total_pnl, got %v", agg.TotalPnL)
	}

	// --- 13. Create 3 more portfolios to reach the 5 cap, then attempt a 6th.
	postCreatePortfolio(t, srv, hdr, "Three", 1000)
	postCreatePortfolio(t, srv, hdr, "Four", 1000)
	postCreatePortfolio(t, srv, hdr, "Five", 1000)
	rrOver := doJSON(t, srv, http.MethodPost, "/v1/virtual/portfolios",
		map[string]any{"name": "Six", "starting_balance": 1000}, hdr)
	if rrOver.Code != http.StatusConflict {
		t.Fatalf("expected 409 over limit, got %d", rrOver.Code)
	}

	// --- 14. Delete Conservative — list shows 4 portfolios.
	rrDel := doJSON(t, srv, http.MethodDelete,
		"/v1/virtual/portfolios/"+conservativeID.String(), nil, hdr)
	if rrDel.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rrDel.Code)
	}
	rrList3 := doJSON(t, srv, http.MethodGet, "/v1/virtual/portfolios", nil, hdr)
	var list3 struct {
		Portfolios []any `json:"portfolios"`
	}
	_ = json.NewDecoder(rrList3.Body).Decode(&list3)
	if len(list3.Portfolios) != 4 {
		t.Fatalf("expected 4 after delete, got %d", len(list3.Portfolios))
	}

	// --- 15. Access deleted portfolio → 404.
	rrGone := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+conservativeID.String(), nil, hdr)
	if rrGone.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rrGone.Code)
	}
}

// postCreatePortfolio creates a portfolio via the HTTP API and returns its id.
func postCreatePortfolio(t *testing.T, srv http.Handler, hdr map[string]string, name string, balance float64) uuid.UUID {
	t.Helper()
	rr := doJSON(t, srv, http.MethodPost, "/v1/virtual/portfolios",
		map[string]any{"name": name, "starting_balance": balance}, hdr)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create %s: %d", name, rr.Code)
	}
	var body map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&body)
	return uuid.MustParse(body["id"].(string))
}
