package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
)

// Server bundles the chi router with the repos every handler reaches into.
// cmd/api wires one of these at boot.
type Server struct {
	Router  chi.Router
	Devices *storage.DeviceRepo
}

// NewServer builds a Server with the standard middleware stack and routes
// the public endpoints. /health is mounted at the root; everything else
// lives under /v1.
func NewServer(devices *storage.DeviceRepo) *Server {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", healthHandler)

	dh := newDeviceHandler(devices)

	r.Route("/v1", func(r chi.Router) {
		r.Post("/devices", dh.register)
		// Authenticated routes are mounted here in later tasks:
		// r.Group(func(r chi.Router) {
		//     r.Use(requireDeviceID(devices))
		//     r.Get("/alerts", ...)
		//     ...
		// })
	})

	return &Server{Router: r, Devices: devices}
}

// ServeHTTP lets a Server satisfy http.Handler directly.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Router.ServeHTTP(w, r)
}

// healthHandler is a 200/OK liveness probe.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
