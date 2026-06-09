// Package worker owns the cron loop that drives one alert evaluation pass
// per tick, pushes the resulting firings to APNs, and records delivery state.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/eval"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/push"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
)

// FiringRecorder is the subset of storage.FiringRepo Runner needs.
type FiringRecorder interface {
	Insert(ctx context.Context, f storage.Firing) (int64, error)
	UpdateStatus(ctx context.Context, id int64, status storage.DeliveryStatus) error
}

// DeviceLookup is the subset of storage.DeviceRepo Runner needs.
type DeviceLookup interface {
	Get(ctx context.Context, id uuid.UUID) (*storage.Device, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

// Evaluator is the subset of eval.Evaluator Runner needs.
type Evaluator interface {
	Tick(ctx context.Context, now time.Time) ([]eval.Firing, error)
}

// Runner is the cron-side orchestrator. One method, Tick, runs the full
// pipeline for a single pass.
type Runner struct {
	Evaluator Evaluator
	Firings   FiringRecorder
	Devices   DeviceLookup
	Pusher    push.Pusher
}

// Tick runs one evaluation + delivery pass. Returns the number of firings
// produced by the evaluator regardless of how many succeeded; errors from
// individual deliveries are logged and don't abort the pass.
func (r *Runner) Tick(ctx context.Context, now time.Time) (int, error) {
	if r == nil {
		return 0, errors.New("worker: nil Runner")
	}
	firings, err := r.Evaluator.Tick(ctx, now)
	if err != nil {
		return 0, fmt.Errorf("worker: evaluator tick: %w", err)
	}
	for _, f := range firings {
		r.deliverOne(ctx, f)
	}
	return len(firings), nil
}

// deliverOne records the firing audit row, looks up the device, pushes via
// APNs, and updates the firing status. A 410 from APNs revokes the device
// row so subsequent ticks stop trying.
func (r *Runner) deliverOne(ctx context.Context, f eval.Firing) {
	alertID, err := uuid.Parse(f.Alert.ID)
	if err != nil {
		log.Printf("worker: invalid alert id %q: %v", f.Alert.ID, err)
		return
	}
	firingID, err := r.Firings.Insert(ctx, storage.Firing{
		AlertID:        alertID,
		DeviceID:       f.DeviceID,
		FiredAt:        f.FiredAt,
		ActualValue:    f.ActualValue,
		DeliveryStatus: storage.DeliveryPending,
	})
	if err != nil {
		log.Printf("worker: insert firing for alert %s: %v", alertID, err)
		return
	}

	device, err := r.Devices.Get(ctx, f.DeviceID)
	if err != nil {
		log.Printf("worker: lookup device %s: %v", f.DeviceID, err)
		_ = r.Firings.UpdateStatus(ctx, firingID, storage.DeliveryFailed)
		return
	}

	status, pushErr := r.Pusher.Push(ctx, f, *device)
	if pushErr != nil {
		log.Printf("worker: push for alert %s: %v", alertID, pushErr)
	}

	mapped := mapStatus(status)
	if err := r.Firings.UpdateStatus(ctx, firingID, mapped); err != nil {
		log.Printf("worker: update firing %d status: %v", firingID, err)
	}

	if status == push.Dropped {
		if err := r.Devices.Delete(ctx, f.DeviceID); err != nil {
			log.Printf("worker: revoke device %s after 410: %v", f.DeviceID, err)
		}
	}
}

// mapStatus converts a push delivery outcome to its storage equivalent.
func mapStatus(s push.DeliveryStatus) storage.DeliveryStatus {
	switch s {
	case push.Delivered:
		return storage.DeliveryDelivered
	case push.Dropped:
		return storage.DeliveryDropped
	default:
		return storage.DeliveryFailed
	}
}
