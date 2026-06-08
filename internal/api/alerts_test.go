package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/api"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage/storagetest"
)

// seedDeviceForAlerts inserts a device row and returns its id; alert handler
// tests use this so requireDeviceID resolves.
func seedDeviceForAlerts(t *testing.T, h *storagetest.Harness) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := storage.NewDeviceRepo(h.Postgres.Pool).Upsert(context.Background(),
		storage.Device{DeviceID: id, APNsToken: "tok", APNsEnv: "development", Locale: "en"}); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	return id
}

func putAlertBody(alertID uuid.UUID) map[string]any {
	return map[string]any{
		"id": alertID.String(),
		"condition": map[string]any{
			"type":         "priceCrossing",
			"coin_id":      "bitcoin",
			"direction":    "above",
			"target_price": 75000,
		},
		"recurrence": map[string]any{"type": "oneShot"},
		"is_active":  true,
	}
}

func TestPutAlert_Upserts_201Like200(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForAlerts(t, h)
	alertID := uuid.New()

	rr := doJSON(t, srv, http.MethodPut, "/v1/alerts/"+alertID.String(),
		putAlertBody(alertID), map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	stored, err := storage.NewAlertRepo(h.Postgres.Pool).Get(context.Background(), alertID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if pc, ok := stored.Condition.(domain.PriceCrossing); !ok || pc.CoinID != "bitcoin" {
		t.Fatalf("unexpected condition: %+v", stored.Condition)
	}
	if _, ok := stored.Recurrence.(domain.OneShot); !ok {
		t.Fatalf("expected OneShot, got %T", stored.Recurrence)
	}
}

func TestPutAlert_BodyAndURLMismatch_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForAlerts(t, h)
	urlID := uuid.New()
	bodyID := uuid.New() // intentionally different

	rr := doJSON(t, srv, http.MethodPut, "/v1/alerts/"+urlID.String(),
		putAlertBody(bodyID), map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	var er api.ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&er)
	if er.Error != "invalid_payload" {
		t.Fatalf("expected invalid_payload, got %q", er.Error)
	}
}

func TestPutAlert_RejectsUnknownConditionType_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForAlerts(t, h)
	alertID := uuid.New()

	body := map[string]any{
		"id":         alertID.String(),
		"condition":  map[string]any{"type": "madeUp"},
		"recurrence": map[string]any{"type": "oneShot"},
		"is_active":  true,
	}
	rr := doJSON(t, srv, http.MethodPut, "/v1/alerts/"+alertID.String(),
		body, map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPutAlert_NoXDeviceID_401(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	alertID := uuid.New()

	rr := doJSON(t, srv, http.MethodPut, "/v1/alerts/"+alertID.String(),
		putAlertBody(alertID), nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestGetAlerts_ReturnsThisDevicesAlerts(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev1 := seedDeviceForAlerts(t, h)
	dev2 := seedDeviceForAlerts(t, h)

	alerts := storage.NewAlertRepo(h.Postgres.Pool)
	for i := 0; i < 2; i++ {
		if err := alerts.Upsert(context.Background(), dev1, domain.PriceAlert{
			ID:         uuid.New().String(),
			Condition:  domain.PriceCrossing{CoinID: "btc", Direction: domain.Above, TargetPrice: float64(i + 1)},
			Recurrence: domain.OneShot{}, IsActive: true,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if err := alerts.Upsert(context.Background(), dev2, domain.PriceAlert{
		ID:         uuid.New().String(),
		Condition:  domain.PriceCrossing{CoinID: "eth", Direction: domain.Above, TargetPrice: 1},
		Recurrence: domain.OneShot{}, IsActive: true,
	}); err != nil {
		t.Fatalf("seed dev2: %v", err)
	}

	rr := doJSON(t, srv, http.MethodGet, "/v1/alerts", nil,
		map[string]string{"X-Device-Id": dev1.String()})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Alerts []json.RawMessage `json:"alerts"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Alerts) != 2 {
		t.Fatalf("expected 2 alerts, got %d", len(resp.Alerts))
	}
}

func TestDeleteAlert_RemovesRow_204(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForAlerts(t, h)
	alerts := storage.NewAlertRepo(h.Postgres.Pool)

	alertID := uuid.New()
	if err := alerts.Upsert(context.Background(), dev, domain.PriceAlert{
		ID:         alertID.String(),
		Condition:  domain.PriceCrossing{CoinID: "btc", Direction: domain.Above, TargetPrice: 1},
		Recurrence: domain.OneShot{}, IsActive: true,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rr := doJSON(t, srv, http.MethodDelete, "/v1/alerts/"+alertID.String(), nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if _, err := alerts.Get(context.Background(), alertID); err == nil {
		t.Fatal("expected alert to be gone")
	}
}

func TestDeleteAlert_MissingAlert_204(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)
	dev := seedDeviceForAlerts(t, h)

	rr := doJSON(t, srv, http.MethodDelete, "/v1/alerts/"+uuid.New().String(), nil,
		map[string]string{"X-Device-Id": dev.String()})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
}
