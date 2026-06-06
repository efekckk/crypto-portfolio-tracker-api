package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/api"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage/storagetest"
)

func newServer(t *testing.T, h *storagetest.Harness) *api.Server {
	t.Helper()
	return api.NewServer(storage.NewDeviceRepo(h.Postgres.Pool))
}

func doJSON(t *testing.T, srv http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return rr
}

func TestPostDevices_RegistersFreshDevice_201(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)

	id := uuid.NewString()
	rr := doJSON(t, srv, http.MethodPost, "/v1/devices", map[string]any{
		"device_id": id, "apns_token": "tok", "apns_env": "development", "locale": "tr",
	}, nil)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]string
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp["registered_at"] == "" {
		t.Fatalf("expected registered_at, got %v", resp)
	}
}

func TestPostDevices_RefreshExistingDevice_200(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)

	id := uuid.NewString()
	// First register
	rr1 := doJSON(t, srv, http.MethodPost, "/v1/devices", map[string]any{
		"device_id": id, "apns_token": "first", "apns_env": "development",
	}, nil)
	if rr1.Code != http.StatusCreated {
		t.Fatalf("first call: expected 201, got %d", rr1.Code)
	}
	// Second call with the same id — refresh
	rr2 := doJSON(t, srv, http.MethodPost, "/v1/devices", map[string]any{
		"device_id": id, "apns_token": "second", "apns_env": "production", "locale": "en",
	}, nil)
	if rr2.Code != http.StatusOK {
		t.Fatalf("refresh call: expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}

	// Verify the new fields landed.
	dev, err := storage.NewDeviceRepo(h.Postgres.Pool).Get(context.Background(), uuid.MustParse(id))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if dev.APNsToken != "second" || dev.APNsEnv != "production" || dev.Locale != "en" {
		t.Fatalf("expected refreshed state, got %+v", dev)
	}
}

func TestPostDevices_InvalidJSON_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)

	req := httptest.NewRequest(http.MethodPost, "/v1/devices",
		bytes.NewBufferString("{ not really json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	var er api.ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&er)
	if er.Error != "invalid_payload" {
		t.Fatalf("expected invalid_payload, got %q", er.Error)
	}
}

func TestPostDevices_InvalidUUID_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)

	rr := doJSON(t, srv, http.MethodPost, "/v1/devices", map[string]any{
		"device_id": "not-a-uuid", "apns_token": "tok", "apns_env": "development",
	}, nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPostDevices_InvalidEnv_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)

	rr := doJSON(t, srv, http.MethodPost, "/v1/devices", map[string]any{
		"device_id": uuid.NewString(), "apns_token": "tok", "apns_env": "staging",
	}, nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	var er api.ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&er)
	if er.Error != "invalid_payload" {
		t.Fatalf("expected invalid_payload, got %q", er.Error)
	}
}

func TestPostDevices_MissingToken_400(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	srv := newServer(t, h)

	rr := doJSON(t, srv, http.MethodPost, "/v1/devices", map[string]any{
		"device_id": uuid.NewString(), "apns_env": "development",
	}, nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHealth_returns_200_ok(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	srv := newServer(t, h)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}
