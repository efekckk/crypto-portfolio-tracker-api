package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/virtual"
)

// maxVirtualPortfoliosPerDevice caps the per-device portfolio count.
const maxVirtualPortfoliosPerDevice = 5

// virtualPortfolioPricing is the subset of *virtual.PricingService that the
// list handler needs. Tests inject a fake so the list endpoint works without
// a real CoinGecko round-trip.
type virtualPortfolioPricing interface {
	FetchMany(ctx context.Context, coinIDs []string, vsCurrency string) (map[string]virtual.CachedPrice, error)
}

// virtualPortfolioHandler wraps the /v1/virtual/portfolios endpoints. All
// routes are expected to live behind requireDeviceID.
type virtualPortfolioHandler struct {
	portfolios *storage.VirtualPortfolioRepo
	trades     *storage.VirtualTradeRepo
	pricing    virtualPortfolioPricing
}

func newVirtualPortfolioHandler(
	portfolios *storage.VirtualPortfolioRepo,
	trades *storage.VirtualTradeRepo,
	pricing virtualPortfolioPricing,
) *virtualPortfolioHandler {
	return &virtualPortfolioHandler{portfolios: portfolios, trades: trades, pricing: pricing}
}

// register handles POST /v1/virtual/portfolios. Validates the body, checks
// the per-device cap, and inserts a fresh portfolio with a server-generated
// id.
func (h *virtualPortfolioHandler) register(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	dev := DeviceFromContext(r.Context())

	var body createVirtualPortfolioRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || len(name) > 50 {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			"name must be 1-50 characters")
		return
	}
	if body.StartingBalance <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			"starting_balance must be greater than zero")
		return
	}

	count, err := h.portfolios.CountByDevice(r.Context(), dev.DeviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if count >= maxVirtualPortfoliosPerDevice {
		writeError(w, http.StatusConflict, "conflict",
			"per-device portfolio limit reached")
		return
	}

	p := virtual.Portfolio{
		ID:              uuid.New(),
		DeviceID:        dev.DeviceID,
		Name:            name,
		StartingBalance: body.StartingBalance,
	}
	if err := h.portfolios.Create(r.Context(), p); err != nil {
		if errors.Is(err, storage.ErrVirtualPortfolioNameTaken) {
			writeError(w, http.StatusConflict, "conflict",
				"a portfolio with this name already exists for this device")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	// Re-read to capture the server-side timestamps.
	stored, err := h.portfolios.Get(r.Context(), p.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, virtualPortfolioCreateResponse{
		ID:              stored.ID.String(),
		Name:            stored.Name,
		StartingBalance: stored.StartingBalance,
		CreatedAt:       stored.CreatedAt,
	})
}

// list handles GET /v1/virtual/portfolios. Loads every portfolio for the
// device, then folds each one's trades to a summary. Coin prices are
// fetched in a single batched markets call so N portfolios cost 1 upstream
// fetch.
func (h *virtualPortfolioHandler) list(w http.ResponseWriter, r *http.Request) {
	dev := DeviceFromContext(r.Context())

	portfolios, err := h.portfolios.ListByDevice(r.Context(), dev.DeviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if len(portfolios) == 0 {
		writeJSON(w, http.StatusOK, virtualPortfoliosListResponse{Portfolios: []virtualPortfolioSummary{}})
		return
	}

	// Load every trade for every portfolio + collect required coin ids.
	tradesByPortfolio := make(map[uuid.UUID][]virtual.Trade, len(portfolios))
	coinIDSet := map[string]struct{}{}
	for _, p := range portfolios {
		trades, err := h.trades.ListAllByPortfolio(r.Context(), p.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		tradesByPortfolio[p.ID] = trades
		for _, t := range trades {
			coinIDSet[t.CoinID] = struct{}{}
		}
	}

	coinIDs := make([]string, 0, len(coinIDSet))
	for id := range coinIDSet {
		coinIDs = append(coinIDs, id)
	}
	priced, err := h.pricing.FetchMany(r.Context(), coinIDs, "usd")
	if err != nil {
		// Markets fetch failing is non-fatal for the list view: render
		// without current prices (cash + cost basis only). The detail
		// endpoint surfaces upstream errors more visibly.
		priced = map[string]virtual.CachedPrice{}
	}
	currentPrices := make(map[string]float64, len(priced))
	for id, cp := range priced {
		currentPrices[id] = cp.Coin.CurrentPrice
	}

	out := make([]virtualPortfolioSummary, 0, len(portfolios))
	for _, p := range portfolios {
		trades := tradesByPortfolio[p.ID]
		state := virtual.Compute(p.StartingBalance, trades, currentPrices)
		out = append(out, virtualPortfolioSummary{
			ID:              p.ID.String(),
			Name:            p.Name,
			StartingBalance: p.StartingBalance,
			CashBalance:     state.CashBalance,
			TotalValue:      state.TotalValue,
			TotalPnL:        state.TotalValue - p.StartingBalance,
			TotalPnLPercent: state.TotalPnLPercent,
			TradeCount:      len(trades),
			CreatedAt:       p.CreatedAt,
			UpdatedAt:       p.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, virtualPortfoliosListResponse{Portfolios: out})
}

// getDetail handles GET /v1/virtual/portfolios/{id}. Loads the portfolio,
// folds its trades, fetches current prices in a single markets call, and
// renders the full computed state.
func (h *virtualPortfolioHandler) getDetail(w http.ResponseWriter, r *http.Request) {
	dev := DeviceFromContext(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			"url id is not a valid UUID")
		return
	}

	p, err := h.portfolios.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, storage.ErrVirtualPortfolioNotFound) {
			writeError(w, http.StatusNotFound, "not_found",
				"portfolio not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if p.DeviceID != dev.DeviceID {
		writeError(w, http.StatusForbidden, "forbidden",
			"portfolio belongs to a different device")
		return
	}

	trades, err := h.trades.ListAllByPortfolio(r.Context(), p.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	coinIDSet := map[string]struct{}{}
	for _, t := range trades {
		coinIDSet[t.CoinID] = struct{}{}
	}
	coinIDs := make([]string, 0, len(coinIDSet))
	for cid := range coinIDSet {
		coinIDs = append(coinIDs, cid)
	}

	currentPrices := map[string]float64{}
	if len(coinIDs) > 0 {
		priced, err := h.pricing.FetchMany(r.Context(), coinIDs, "usd")
		if err != nil {
			writeError(w, http.StatusBadGateway, "upstream_error",
				"couldn't fetch current prices")
			return
		}
		for cid, cp := range priced {
			currentPrices[cid] = cp.Coin.CurrentPrice
		}
	}

	state := virtual.Compute(p.StartingBalance, trades, currentPrices)
	holdings := make([]virtualHoldingDTO, 0, len(state.Holdings))
	for _, h := range state.Holdings {
		holdings = append(holdings, virtualHoldingDTO{
			CoinID:               h.CoinID,
			Amount:               h.Amount,
			AverageBuyPrice:      h.AverageBuyPrice,
			CurrentPrice:         h.CurrentPrice,
			CurrentValue:         h.CurrentValue,
			UnrealizedPnL:        h.UnrealizedPnL,
			UnrealizedPnLPercent: h.UnrealizedPnLPercent,
		})
	}

	writeJSON(w, http.StatusOK, virtualPortfolioDetailResponse{
		ID:              p.ID.String(),
		Name:            p.Name,
		StartingBalance: p.StartingBalance,
		CashBalance:     state.CashBalance,
		TotalValue:      state.TotalValue,
		RealizedPnL:     state.RealizedPnL,
		UnrealizedPnL:   state.UnrealizedPnL,
		TotalPnLPercent: state.TotalPnLPercent,
		Holdings:        holdings,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	})
}

// delete handles DELETE /v1/virtual/portfolios/{id}. Idempotent: missing
// portfolio is still 204. Cross-device delete is rejected with 403 to keep
// behaviour consistent with getDetail.
func (h *virtualPortfolioHandler) delete(w http.ResponseWriter, r *http.Request) {
	dev := DeviceFromContext(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			"url id is not a valid UUID")
		return
	}

	// We look up first so we can reject cross-device attempts. A missing row
	// returns 204 silently (idempotent).
	p, err := h.portfolios.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, storage.ErrVirtualPortfolioNotFound) {
			writeJSON(w, http.StatusNoContent, nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if p.DeviceID != dev.DeviceID {
		writeError(w, http.StatusForbidden, "forbidden",
			"portfolio belongs to a different device")
		return
	}

	if err := h.portfolios.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
