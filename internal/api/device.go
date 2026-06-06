package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
)

// registerDeviceRequest is the body shape of POST /v1/devices.
type registerDeviceRequest struct {
	DeviceID  string `json:"device_id"`
	APNsToken string `json:"apns_token"`
	APNsEnv   string `json:"apns_env"`
	Locale    string `json:"locale"`
}

// registerDeviceResponse confirms the registration timestamp.
type registerDeviceResponse struct {
	RegisteredAt time.Time `json:"registered_at"`
}

// deviceHandler wraps the registration handler and any device-scoped routes
// added later.
type deviceHandler struct {
	devices *storage.DeviceRepo
}

func newDeviceHandler(devices *storage.DeviceRepo) *deviceHandler {
	return &deviceHandler{devices: devices}
}

// register handles POST /v1/devices. The first call inserts a new row; the
// second (same device_id) refreshes the APNs token / env / locale.
func (h *deviceHandler) register(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var body registerDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}

	id, err := uuid.Parse(body.DeviceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			"device_id must be a valid UUID")
		return
	}
	if body.APNsToken == "" {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			"apns_token is required")
		return
	}
	if body.APNsEnv != "development" && body.APNsEnv != "production" {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			"apns_env must be \"development\" or \"production\"")
		return
	}

	existing, err := h.devices.Get(r.Context(), id)
	isRefresh := err == nil && existing != nil
	// (errors other than ErrDeviceNotFound are tolerated here — Upsert will
	// surface them again in a moment.)

	if err := h.devices.Upsert(r.Context(), storage.Device{
		DeviceID:  id,
		APNsToken: body.APNsToken,
		APNsEnv:   body.APNsEnv,
		Locale:    body.Locale,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	status := http.StatusCreated
	if isRefresh {
		status = http.StatusOK
	}
	writeJSON(w, status, registerDeviceResponse{RegisteredAt: time.Now().UTC()})
}
