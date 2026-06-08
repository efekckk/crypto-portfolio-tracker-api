package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
)

// alertsHandler wraps the CRUD endpoints for /v1/alerts. Every route is
// expected to live behind requireDeviceID so DeviceFromContext returns the
// caller's device.
type alertsHandler struct {
	alerts *storage.AlertRepo
}

func newAlertsHandler(alerts *storage.AlertRepo) *alertsHandler {
	return &alertsHandler{alerts: alerts}
}

// list handles GET /v1/alerts — returns every alert for the calling device.
func (h *alertsHandler) list(w http.ResponseWriter, r *http.Request) {
	dev := DeviceFromContext(r.Context())
	list, err := h.alerts.ListByDevice(r.Context(), dev.DeviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	dtos := make([]alertDTO, 0, len(list))
	for _, a := range list {
		dto, err := alertToDTO(a)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		dtos = append(dtos, dto)
	}
	writeJSON(w, http.StatusOK, alertsListResponse{Alerts: dtos})
}

// put handles PUT /v1/alerts/{id} — upsert. The body's id must match the URL.
func (h *alertsHandler) put(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	dev := DeviceFromContext(r.Context())

	urlID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			"url id is not a valid UUID")
		return
	}

	var dto alertDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}
	if dto.ID == "" {
		dto.ID = urlID.String()
	}
	bodyID, err := uuid.Parse(dto.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			"body id is not a valid UUID")
		return
	}
	if bodyID != urlID {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			"url id and body id do not match")
		return
	}

	alert, err := dto.toDomain()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}
	if err := h.alerts.Upsert(r.Context(), dev.DeviceID, alert); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	out, err := alertToDTO(alert)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// delete handles DELETE /v1/alerts/{id}. Idempotent — returns 204 whether or
// not the row existed beforehand.
func (h *alertsHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			"url id is not a valid UUID")
		return
	}
	if err := h.alerts.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
