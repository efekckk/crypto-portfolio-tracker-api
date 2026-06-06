package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage/storagetest"
)

// dummyHandler echoes the resolved device id back to the caller.
func dummyHandler(w http.ResponseWriter, r *http.Request) {
	d := DeviceFromContext(r.Context())
	_ = json.NewEncoder(w).Encode(map[string]string{"device_id": d.DeviceID.String()})
}

func TestRequireDeviceID_MissingHeader_401(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewDeviceRepo(h.Postgres.Pool)

	mw := requireDeviceID(repo)
	srv := mw(http.HandlerFunc(dummyHandler))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	var er ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&er)
	if er.Error != "device_unknown" {
		t.Fatalf("expected device_unknown, got %q", er.Error)
	}
}

func TestRequireDeviceID_InvalidUUID_401(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewDeviceRepo(h.Postgres.Pool)

	srv := requireDeviceID(repo)(http.HandlerFunc(dummyHandler))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Device-Id", "garbage")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestRequireDeviceID_UnregisteredDevice_401(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewDeviceRepo(h.Postgres.Pool)

	srv := requireDeviceID(repo)(http.HandlerFunc(dummyHandler))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Device-Id", uuid.NewString())
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestRequireDeviceID_PopulatesContext_200(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewDeviceRepo(h.Postgres.Pool)

	id := uuid.New()
	if err := repo.Upsert(context.Background(), storage.Device{
		DeviceID: id, APNsToken: "tok", APNsEnv: "development", Locale: "en",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv := requireDeviceID(repo)(http.HandlerFunc(dummyHandler))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Device-Id", id.String())
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]string
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if body["device_id"] != id.String() {
		t.Fatalf("expected echo %s, got %v", id, body)
	}
}
