package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
)

type ctxKey int

const deviceCtxKey ctxKey = iota

// requireDeviceID parses the X-Device-Id header, looks the device up via the
// repo, and stashes the resolved Device on the request context. Downstream
// handlers reach it via DeviceFromContext.
func requireDeviceID(devices *storage.DeviceRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get("X-Device-Id")
			if raw == "" {
				writeError(w, http.StatusUnauthorized, "device_unknown",
					"X-Device-Id header is required")
				return
			}
			id, err := uuid.Parse(raw)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "device_unknown",
					"X-Device-Id is not a valid UUID")
				return
			}
			d, err := devices.Get(r.Context(), id)
			if err != nil {
				if errors.Is(err, storage.ErrDeviceNotFound) {
					writeError(w, http.StatusUnauthorized, "device_unknown",
						"device is not registered; POST /v1/devices first")
					return
				}
				writeError(w, http.StatusInternalServerError, "internal", err.Error())
				return
			}
			ctx := context.WithValue(r.Context(), deviceCtxKey, d)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// DeviceFromContext returns the resolved Device that requireDeviceID stored
// on the request context. Panics if called without the middleware in front —
// this is a controller-side bug, not a runtime error.
func DeviceFromContext(ctx context.Context) *storage.Device {
	d, ok := ctx.Value(deviceCtxKey).(*storage.Device)
	if !ok {
		panic("api: requireDeviceID middleware not in chain")
	}
	return d
}
