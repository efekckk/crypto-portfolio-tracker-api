package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage/storagetest"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/virtual"
)

// seedTradesDirect inserts N trades for the portfolio directly via the repo
// so the test doesn't go through the trade endpoint (which depends on price
// fetches). Returns the inserted ids in insert order.
func seedTradesDirect(t *testing.T, h *storagetest.Harness, pid uuid.UUID, n int) []int64 {
	t.Helper()
	repo := storage.NewVirtualTradeRepo(h.Postgres.Pool)
	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		id, err := repo.Insert(context.Background(), virtual.Trade{
			PortfolioID: pid, Side: virtual.SideBuy, CoinID: "bitcoin",
			Amount: 0.01, Price: 70000,
			ExecutedAt: time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("seed trade %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestTradeHistory_EmptyPortfolio_ReturnsEmptyArray(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, dev, "Fresh", 10000)

	rr := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+pid.String()+"/trades", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var body struct {
		Trades     []any  `json:"trades"`
		NextCursor *int64 `json:"next_cursor"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if body.Trades == nil {
		t.Fatal("expected [] not null")
	}
	if len(body.Trades) != 0 {
		t.Fatalf("expected 0 trades, got %d", len(body.Trades))
	}
	if body.NextCursor != nil {
		t.Fatalf("expected null cursor, got %v", body.NextCursor)
	}
}

func TestTradeHistory_NewestFirst_WithDefaultLimit(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, dev, "Active", 10000)
	ids := seedTradesDirect(t, h, pid, 5) // ids[0] oldest, ids[4] newest

	rr := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+pid.String()+"/trades", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var body struct {
		Trades []struct {
			ID int64 `json:"id"`
		} `json:"trades"`
		NextCursor *int64 `json:"next_cursor"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if len(body.Trades) != 5 {
		t.Fatalf("expected 5 trades, got %d", len(body.Trades))
	}
	// Newest first: body.Trades[0].ID == ids[4]
	for i := 0; i < 5; i++ {
		want := ids[len(ids)-1-i]
		if body.Trades[i].ID != want {
			t.Fatalf("index %d: expected id %d, got %d", i, want, body.Trades[i].ID)
		}
	}
	if body.NextCursor != nil {
		t.Fatalf("expected null cursor (page not full), got %v", body.NextCursor)
	}
}

func TestTradeHistory_Pagination_WalksAllPages(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, dev, "Active", 10000)
	ids := seedTradesDirect(t, h, pid, 7)

	// Page 1: limit=3 → ids[6], ids[5], ids[4]; next_cursor=ids[4]
	rr1 := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+pid.String()+"/trades?limit=3", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr1.Code != http.StatusOK {
		t.Fatalf("page1: %d", rr1.Code)
	}
	var page1 struct {
		Trades []struct {
			ID int64 `json:"id"`
		} `json:"trades"`
		NextCursor *int64 `json:"next_cursor"`
	}
	_ = json.NewDecoder(rr1.Body).Decode(&page1)
	if len(page1.Trades) != 3 || page1.NextCursor == nil || *page1.NextCursor != ids[4] {
		t.Fatalf("page1: %+v cursor=%v", page1.Trades, page1.NextCursor)
	}

	// Page 2: before_id=ids[4] limit=3 → ids[3], ids[2], ids[1]; next_cursor=ids[1]
	rr2 := doJSON(t, srv, http.MethodGet,
		fmt.Sprintf("/v1/virtual/portfolios/%s/trades?limit=3&before_id=%d", pid, *page1.NextCursor), nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr2.Code != http.StatusOK {
		t.Fatalf("page2: %d", rr2.Code)
	}
	var page2 struct {
		Trades []struct {
			ID int64 `json:"id"`
		} `json:"trades"`
		NextCursor *int64 `json:"next_cursor"`
	}
	_ = json.NewDecoder(rr2.Body).Decode(&page2)
	if len(page2.Trades) != 3 || page2.NextCursor == nil || *page2.NextCursor != ids[1] {
		t.Fatalf("page2: %+v cursor=%v", page2.Trades, page2.NextCursor)
	}

	// Page 3: before_id=ids[1] limit=3 → ids[0]; next_cursor=null (page not full)
	rr3 := doJSON(t, srv, http.MethodGet,
		fmt.Sprintf("/v1/virtual/portfolios/%s/trades?limit=3&before_id=%d", pid, *page2.NextCursor), nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr3.Code != http.StatusOK {
		t.Fatalf("page3: %d", rr3.Code)
	}
	var page3 struct {
		Trades []struct {
			ID int64 `json:"id"`
		} `json:"trades"`
		NextCursor *int64 `json:"next_cursor"`
	}
	_ = json.NewDecoder(rr3.Body).Decode(&page3)
	if len(page3.Trades) != 1 || page3.Trades[0].ID != ids[0] {
		t.Fatalf("page3 trades: %+v", page3.Trades)
	}
	if page3.NextCursor != nil {
		t.Fatalf("expected null cursor on last page, got %v", page3.NextCursor)
	}
}

func TestTradeHistory_BadLimit_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, dev, "Fresh", 10000)

	for _, raw := range []string{"abc", "0", "-1"} {
		rr := doJSON(t, srv, http.MethodGet,
			"/v1/virtual/portfolios/"+pid.String()+"/trades?limit="+raw, nil,
			map[string]string{"X-Device-Id": dev.String()})
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("limit=%q: expected 400, got %d", raw, rr.Code)
		}
	}
}

func TestTradeHistory_BadBeforeID_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, dev, "Fresh", 10000)

	for _, raw := range []string{"abc", "-5"} {
		rr := doJSON(t, srv, http.MethodGet,
			"/v1/virtual/portfolios/"+pid.String()+"/trades?before_id="+raw, nil,
			map[string]string{"X-Device-Id": dev.String()})
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("before_id=%q: expected 400, got %d", raw, rr.Code)
		}
	}
}

func TestTradeHistory_NotFound_404(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)

	rr := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+uuid.NewString()+"/trades", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestTradeHistory_CrossDevice_403(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	d1 := seedDeviceForVirtual_api(t, h)
	d2 := seedDeviceForVirtual_api(t, h)
	pid := createPortfolioViaAPI(t, srv, d1, "Mine", 10000)

	rr := doJSON(t, srv, http.MethodGet,
		"/v1/virtual/portfolios/"+pid.String()+"/trades", nil,
		map[string]string{"X-Device-Id": d2.String()})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}
