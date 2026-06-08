package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/api"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage/storagetest"
)

// seedDeviceForHoldings inserts a device row and returns its id.
func seedDeviceForHoldings(t *testing.T, h *storagetest.Harness) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := storage.NewDeviceRepo(h.Postgres.Pool).Upsert(context.Background(),
		storage.Device{DeviceID: id, APNsToken: "tok", APNsEnv: "development", Locale: "en"}); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	return id
}

func putHoldingBody(coinID string) map[string]any {
	return map[string]any{
		"coin_id":           coinID,
		"amount":            1.5,
		"average_buy_price": 30000.0,
		"date_added":        "2026-01-01T00:00:00Z",
	}
}

func TestPutHolding_Upserts_200(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForHoldings(t, h)

	rr := doJSON(t, srv, http.MethodPut, "/v1/holdings/bitcoin",
		putHoldingBody("bitcoin"), map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	stored, err := storage.NewHoldingRepo(h.Postgres.Pool).Get(context.Background(), dev, "bitcoin")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.CoinID != "bitcoin" || stored.Amount != 1.5 || stored.AverageBuyPrice != 30000 {
		t.Fatalf("unexpected stored holding: %+v", stored)
	}
}

func TestPutHolding_DefaultsDateAddedWhenZero(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForHoldings(t, h)

	body := map[string]any{
		"coin_id":           "ethereum",
		"amount":            2.0,
		"average_buy_price": 2000.0,
		// no date_added
	}
	before := time.Now().UTC().Add(-1 * time.Second)
	rr := doJSON(t, srv, http.MethodPut, "/v1/holdings/ethereum",
		body, map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	stored, err := storage.NewHoldingRepo(h.Postgres.Pool).Get(context.Background(), dev, "ethereum")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.DateAdded.Before(before) {
		t.Fatalf("expected DateAdded to default to now (after %v), got %v", before, stored.DateAdded)
	}
}

func TestPutHolding_BodyAndURLMismatch_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForHoldings(t, h)

	rr := doJSON(t, srv, http.MethodPut, "/v1/holdings/bitcoin",
		putHoldingBody("ethereum"), map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	var er api.ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&er)
	if er.Error != "invalid_payload" {
		t.Fatalf("expected invalid_payload, got %q", er.Error)
	}
}

func TestPutHolding_RejectsBadCoinIDPattern_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForHoldings(t, h)

	rr := doJSON(t, srv, http.MethodPut, "/v1/holdings/Bad_Coin!",
		map[string]any{"coin_id": "Bad_Coin!", "amount": 1.0, "average_buy_price": 1.0},
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPutHolding_RejectsNonPositiveAmount_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForHoldings(t, h)

	body := putHoldingBody("bitcoin")
	body["amount"] = 0.0
	rr := doJSON(t, srv, http.MethodPut, "/v1/holdings/bitcoin",
		body, map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPutHolding_NoXDeviceID_401(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)

	rr := doJSON(t, srv, http.MethodPut, "/v1/holdings/bitcoin",
		putHoldingBody("bitcoin"), nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestGetHoldings_ReturnsThisDevicesHoldings(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev1 := seedDeviceForHoldings(t, h)
	dev2 := seedDeviceForHoldings(t, h)

	repo := storage.NewHoldingRepo(h.Postgres.Pool)
	for _, holding := range []domain.Holding{
		{CoinID: "bitcoin", Amount: 1, AverageBuyPrice: 30000, DateAdded: time.Now().UTC()},
		{CoinID: "ethereum", Amount: 2, AverageBuyPrice: 2000, DateAdded: time.Now().UTC()},
	} {
		if err := repo.Upsert(context.Background(), dev1, holding); err != nil {
			t.Fatalf("seed dev1: %v", err)
		}
	}
	if err := repo.Upsert(context.Background(), dev2, domain.Holding{
		CoinID: "solana", Amount: 5, AverageBuyPrice: 80, DateAdded: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed dev2: %v", err)
	}

	rr := doJSON(t, srv, http.MethodGet, "/v1/holdings", nil,
		map[string]string{"X-Device-Id": dev1.String()})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp struct {
		Holdings []domain.Holding `json:"holdings"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Holdings) != 2 {
		t.Fatalf("expected 2, got %d", len(resp.Holdings))
	}
}

func TestGetHoldings_EmptyDeviceReturnsEmptyArray(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForHoldings(t, h)

	rr := doJSON(t, srv, http.MethodGet, "/v1/holdings", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp struct {
		Holdings []domain.Holding `json:"holdings"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Holdings == nil {
		t.Fatal("holdings should be [] not null")
	}
	if len(resp.Holdings) != 0 {
		t.Fatalf("expected empty, got %d", len(resp.Holdings))
	}
}

func TestDeleteHolding_RemovesRow_204(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForHoldings(t, h)
	repo := storage.NewHoldingRepo(h.Postgres.Pool)

	if err := repo.Upsert(context.Background(), dev, domain.Holding{
		CoinID: "bitcoin", Amount: 1, AverageBuyPrice: 30000, DateAdded: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rr := doJSON(t, srv, http.MethodDelete, "/v1/holdings/bitcoin", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
}

func TestDeleteHolding_MissingIsStill204(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForHoldings(t, h)

	rr := doJSON(t, srv, http.MethodDelete, "/v1/holdings/doge", nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
}
