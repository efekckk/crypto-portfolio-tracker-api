package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/api"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage/storagetest"
)

// seedDeviceForVirtual_api seeds one device row and returns its id; mirrors the
// pattern used in alerts/holdings tests but kept local for clarity.
func seedDeviceForVirtual_api(t *testing.T, h *storagetest.Harness) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := storage.NewDeviceRepo(h.Postgres.Pool).Upsert(context.Background(),
		storage.Device{DeviceID: id, APNsToken: "tok", APNsEnv: "development", Locale: "en"}); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	return id
}

func createPortfolioBody(name string, balance float64) map[string]any {
	return map[string]any{"name": name, "starting_balance": balance}
}

func TestPostVirtualPortfolio_HappyPath_201(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)

	rr := doJSON(t, srv, http.MethodPost, "/v1/virtual/portfolios",
		createPortfolioBody("Aggressive", 10000),
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if body["name"] != "Aggressive" {
		t.Fatalf("name: %v", body["name"])
	}
	if body["starting_balance"].(float64) != 10000 {
		t.Fatalf("balance: %v", body["starting_balance"])
	}
	if _, ok := body["id"].(string); !ok {
		t.Fatalf("id missing: %v", body)
	}
}

func TestPostVirtualPortfolio_RejectsEmptyName_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)

	rr := doJSON(t, srv, http.MethodPost, "/v1/virtual/portfolios",
		createPortfolioBody("  ", 10000),
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPostVirtualPortfolio_RejectsLongName_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)

	rr := doJSON(t, srv, http.MethodPost, "/v1/virtual/portfolios",
		createPortfolioBody(strings.Repeat("x", 51), 10000),
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPostVirtualPortfolio_RejectsNonPositiveBalance_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)

	for _, balance := range []float64{0, -100} {
		rr := doJSON(t, srv, http.MethodPost, "/v1/virtual/portfolios",
			createPortfolioBody("Bad", balance),
			map[string]string{"X-Device-Id": dev.String()})
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("balance=%v: expected 400, got %d", balance, rr.Code)
		}
	}
}

func TestPostVirtualPortfolio_DuplicateName_409(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)

	rr1 := doJSON(t, srv, http.MethodPost, "/v1/virtual/portfolios",
		createPortfolioBody("Same", 10000),
		map[string]string{"X-Device-Id": dev.String()})
	if rr1.Code != http.StatusCreated {
		t.Fatalf("first create: %d", rr1.Code)
	}
	rr2 := doJSON(t, srv, http.MethodPost, "/v1/virtual/portfolios",
		createPortfolioBody("Same", 10000),
		map[string]string{"X-Device-Id": dev.String()})
	if rr2.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rr2.Code)
	}
	var er api.ErrorResponse
	_ = json.NewDecoder(rr2.Body).Decode(&er)
	if er.Error != "conflict" {
		t.Fatalf("expected conflict, got %q", er.Error)
	}
}

func TestPostVirtualPortfolio_PerDeviceLimit_409(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)

	for i := 0; i < 5; i++ {
		rr := doJSON(t, srv, http.MethodPost, "/v1/virtual/portfolios",
			createPortfolioBody("P"+string(rune('A'+i)), 10000),
			map[string]string{"X-Device-Id": dev.String()})
		if rr.Code != http.StatusCreated {
			t.Fatalf("seed %d: %d", i, rr.Code)
		}
	}
	rr := doJSON(t, srv, http.MethodPost, "/v1/virtual/portfolios",
		createPortfolioBody("Sixth", 10000),
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 over limit, got %d", rr.Code)
	}
}

func TestPostVirtualPortfolio_NoXDeviceID_401(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)

	rr := doJSON(t, srv, http.MethodPost, "/v1/virtual/portfolios",
		createPortfolioBody("X", 10000), nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestPostVirtualPortfolio_InvalidJSON_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)

	req := httptest.NewRequest(http.MethodPost, "/v1/virtual/portfolios",
		bytes.NewBufferString("{ this is not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Id", dev.String())
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestGetVirtualPortfolios_Empty_ReturnsEmptyArray(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForVirtual_api(t, h)

	rr := doJSON(t, srv, http.MethodGet, "/v1/virtual/portfolios", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var body struct {
		Portfolios []any `json:"portfolios"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if body.Portfolios == nil {
		t.Fatal("expected [] not null")
	}
	if len(body.Portfolios) != 0 {
		t.Fatalf("expected empty, got %d", len(body.Portfolios))
	}
}

func TestGetVirtualPortfolios_ReturnsThisDevicesPortfolios(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	d1 := seedDeviceForVirtual_api(t, h)
	d2 := seedDeviceForVirtual_api(t, h)

	for _, name := range []string{"Alpha", "Beta"} {
		rr := doJSON(t, srv, http.MethodPost, "/v1/virtual/portfolios",
			createPortfolioBody(name, 10000),
			map[string]string{"X-Device-Id": d1.String()})
		if rr.Code != http.StatusCreated {
			t.Fatalf("seed %s: %d", name, rr.Code)
		}
	}
	rr := doJSON(t, srv, http.MethodPost, "/v1/virtual/portfolios",
		createPortfolioBody("Other", 5000),
		map[string]string{"X-Device-Id": d2.String()})
	if rr.Code != http.StatusCreated {
		t.Fatalf("d2 seed: %d", rr.Code)
	}

	rr2 := doJSON(t, srv, http.MethodGet, "/v1/virtual/portfolios", nil,
		map[string]string{"X-Device-Id": d1.String()})
	if rr2.Code != http.StatusOK {
		t.Fatalf("list: %d", rr2.Code)
	}
	var body struct {
		Portfolios []map[string]any `json:"portfolios"`
	}
	_ = json.NewDecoder(rr2.Body).Decode(&body)
	if len(body.Portfolios) != 2 {
		t.Fatalf("expected 2 for d1, got %d", len(body.Portfolios))
	}
	// No trades yet → cash_balance == starting_balance == total_value;
	// total_pnl == 0; trade_count == 0.
	for _, p := range body.Portfolios {
		if p["cash_balance"].(float64) != p["starting_balance"].(float64) {
			t.Fatalf("expected cash == starting for empty trades, got %+v", p)
		}
		if p["total_pnl"].(float64) != 0 {
			t.Fatalf("expected pnl 0, got %v", p["total_pnl"])
		}
		if int(p["trade_count"].(float64)) != 0 {
			t.Fatalf("expected 0 trades, got %v", p["trade_count"])
		}
	}
}
