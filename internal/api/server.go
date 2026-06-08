package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
)

// Server bundles the chi router with the repos every handler reaches into.
type Server struct {
	Router  chi.Router
	Devices *storage.DeviceRepo
	Alerts  *storage.AlertRepo
}

// NewServer builds the chi router with the standard middleware stack and
// mounts both public and authenticated routes. /health and /v1/devices are
// public; everything else lives behind the requireDeviceID middleware so
// handlers can rely on DeviceFromContext.
func NewServer(devices *storage.DeviceRepo, alerts *storage.AlertRepo) *Server {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", healthHandler)

	dh := newDeviceHandler(devices)
	ah := newAlertsHandler(alerts)

	r.Route("/v1", func(r chi.Router) {
		r.Post("/devices", dh.register)
		r.Group(func(r chi.Router) {
			r.Use(requireDeviceID(devices))
			r.Get("/alerts", ah.list)
			r.Put("/alerts/{id}", ah.put)
			r.Delete("/alerts/{id}", ah.delete)
		})
	})

	return &Server{Router: r, Devices: devices, Alerts: alerts}
}

// ServeHTTP lets a Server satisfy http.Handler directly.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Router.ServeHTTP(w, r)
}

// healthHandler is a 200/OK liveness probe.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
