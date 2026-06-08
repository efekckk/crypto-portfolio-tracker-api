package api

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
)

// coinIDPattern restricts coin ids to the same shape CoinGecko returns:
// lowercase letters, digits, dashes.
var coinIDPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// holdingsListResponse wraps the list endpoint payload.
type holdingsListResponse struct {
	Holdings []domain.Holding `json:"holdings"`
}

// holdingsHandler wraps the CRUD endpoints for /v1/holdings.
type holdingsHandler struct {
	holdings *storage.HoldingRepo
}

func newHoldingsHandler(holdings *storage.HoldingRepo) *holdingsHandler {
	return &holdingsHandler{holdings: holdings}
}

// list handles GET /v1/holdings — every holding for the calling device.
func (h *holdingsHandler) list(w http.ResponseWriter, r *http.Request) {
	dev := DeviceFromContext(r.Context())
	list, err := h.holdings.ListByDevice(r.Context(), dev.DeviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if list == nil {
		list = []domain.Holding{}
	}
	writeJSON(w, http.StatusOK, holdingsListResponse{Holdings: list})
}

// put handles PUT /v1/holdings/{coin_id} — upsert. The body's coin_id must
// match the URL.
func (h *holdingsHandler) put(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	dev := DeviceFromContext(r.Context())

	urlCoin := chi.URLParam(r, "coin_id")
	if !coinIDPattern.MatchString(urlCoin) {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			"url coin_id has an invalid shape")
		return
	}

	var body domain.Holding
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}
	if body.CoinID == "" {
		body.CoinID = urlCoin
	}
	if body.CoinID != urlCoin {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			"url coin_id and body coin_id do not match")
		return
	}
	if body.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			"amount must be positive")
		return
	}
	if body.AverageBuyPrice <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			"average_buy_price must be positive")
		return
	}

	if err := h.holdings.Upsert(r.Context(), dev.DeviceID, body); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	// Re-read so the response reflects the stored row (including default date).
	stored, err := h.holdings.Get(r.Context(), dev.DeviceID, body.CoinID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stored)
}

// delete handles DELETE /v1/holdings/{coin_id}. Idempotent.
func (h *holdingsHandler) delete(w http.ResponseWriter, r *http.Request) {
	dev := DeviceFromContext(r.Context())
	coin := chi.URLParam(r, "coin_id")
	if !coinIDPattern.MatchString(coin) {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			"url coin_id has an invalid shape")
		return
	}
	if err := h.holdings.Delete(r.Context(), dev.DeviceID, coin); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
