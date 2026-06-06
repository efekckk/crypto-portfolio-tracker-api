package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandler_returnsOKStatus(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	healthHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestHealthHandler_returnsJSONStatusOk(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	healthHandler(rr, req)

	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected JSON content-type, got %q", got)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf(`expected status=ok, got %q`, body["status"])
	}
}

func TestEnvOr_returnsFallbackWhenUnset(t *testing.T) {
	t.Setenv("FOO_TEST_UNSET", "")
	if got := envOr("FOO_TEST_UNSET", "default"); got != "default" {
		t.Fatalf("expected default, got %q", got)
	}
}

func TestEnvOr_returnsValueWhenSet(t *testing.T) {
	t.Setenv("FOO_TEST_SET", "from-env")
	if got := envOr("FOO_TEST_SET", "default"); got != "from-env" {
		t.Fatalf("expected from-env, got %q", got)
	}
}
