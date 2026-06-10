package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
)

// Server bundles the chi router with the repos every handler reaches into.
type Server struct {
	Router            chi.Router
	Devices           *storage.DeviceRepo
	Alerts            *storage.AlertRepo
	Holdings          *storage.HoldingRepo
	VirtualPortfolios *storage.VirtualPortfolioRepo
	VirtualTrades     *storage.VirtualTradeRepo
}

// NewServer builds the chi router with the standard middleware stack and
// mounts every public + authenticated route.
func NewServer(
	devices *storage.DeviceRepo,
	alerts *storage.AlertRepo,
	holdings *storage.HoldingRepo,
	virtualPortfolios *storage.VirtualPortfolioRepo,
	virtualTrades *storage.VirtualTradeRepo,
	virtualPricing virtualPortfolioPricing,
) *Server {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", healthHandler)

	dh := newDeviceHandler(devices)
	ah := newAlertsHandler(alerts)
	hh := newHoldingsHandler(holdings)
	vh := newVirtualPortfolioHandler(virtualPortfolios, virtualTrades, virtualPricing)

	r.Route("/v1", func(r chi.Router) {
		r.Post("/devices", dh.register)
		r.Group(func(r chi.Router) {
			r.Use(requireDeviceID(devices))
			r.Get("/alerts", ah.list)
			r.Put("/alerts/{id}", ah.put)
			r.Delete("/alerts/{id}", ah.delete)
			r.Get("/holdings", hh.list)
			r.Put("/holdings/{coin_id}", hh.put)
			r.Delete("/holdings/{coin_id}", hh.delete)
			r.Post("/virtual/portfolios", vh.register)
			r.Get("/virtual/portfolios", vh.list)
			r.Get("/virtual/portfolios/{id}", vh.getDetail)
			r.Delete("/virtual/portfolios/{id}", vh.delete)
			r.Get("/virtual/portfolios/{id}/quote", vh.quote)
		})
	})

	return &Server{
		Router: r, Devices: devices, Alerts: alerts, Holdings: holdings,
		VirtualPortfolios: virtualPortfolios, VirtualTrades: virtualTrades,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Router.ServeHTTP(w, r)
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
