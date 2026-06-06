package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DeliveryStatus is the lifecycle state of a push delivery attempt.
type DeliveryStatus string

const (
	DeliveryPending   DeliveryStatus = "pending"
	DeliveryDelivered DeliveryStatus = "delivered"
	DeliveryFailed    DeliveryStatus = "failed"
	DeliveryDropped   DeliveryStatus = "dropped"
)

// Firing is the audit record of a single alert firing.
type Firing struct {
	ID             int64
	AlertID        uuid.UUID
	DeviceID       uuid.UUID
	FiredAt        time.Time
	ActualValue    *float64
	DeliveryStatus DeliveryStatus
}

// FiringRepo persists firings and updates delivery status as the push pipeline
// moves through pending → delivered/failed/dropped.
type FiringRepo struct {
	pool *pgxpool.Pool
}

// NewFiringRepo wires a repo against an existing connection pool.
func NewFiringRepo(pool *pgxpool.Pool) *FiringRepo {
	return &FiringRepo{pool: pool}
}

// ErrFiringNotFound is returned by UpdateStatus when no firing matches the id.
var ErrFiringNotFound = errors.New("storage: firing not found")

// Insert records a new firing with the given alert/device/time and returns
// the server-assigned id. delivery_status starts as "pending".
func (r *FiringRepo) Insert(ctx context.Context, f Firing) (int64, error) {
	if f.AlertID == uuid.Nil {
		return 0, errors.New("storage: alert_id is nil")
	}
	if f.DeviceID == uuid.Nil {
		return 0, errors.New("storage: device_id is nil")
	}
	if f.FiredAt.IsZero() {
		return 0, errors.New("storage: fired_at is zero")
	}
	status := f.DeliveryStatus
	if status == "" {
		status = DeliveryPending
	}
	if !validDeliveryStatus(status) {
		return 0, fmt.Errorf("storage: invalid delivery_status %q", status)
	}
	const q = `
		INSERT INTO firings (alert_id, device_id, fired_at, actual_value, delivery_status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`
	var id int64
	if err := r.pool.QueryRow(ctx, q, f.AlertID, f.DeviceID, f.FiredAt, f.ActualValue, status).Scan(&id); err != nil {
		return 0, fmt.Errorf("storage: insert firing: %w", err)
	}
	return id, nil
}

// UpdateStatus moves an existing firing to a new delivery state.
func (r *FiringRepo) UpdateStatus(ctx context.Context, id int64, status DeliveryStatus) error {
	if !validDeliveryStatus(status) {
		return fmt.Errorf("storage: invalid delivery_status %q", status)
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE firings SET delivery_status = $2 WHERE id = $1`, id, status)
	if err != nil {
		return fmt.Errorf("storage: update firing status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrFiringNotFound
	}
	return nil
}

// ListByAlert returns firings for a given alert id, newest first.
func (r *FiringRepo) ListByAlert(ctx context.Context, alertID uuid.UUID) ([]Firing, error) {
	const q = `
		SELECT id, alert_id, device_id, fired_at, actual_value, delivery_status
		FROM firings
		WHERE alert_id = $1
		ORDER BY fired_at DESC, id DESC
	`
	rows, err := r.pool.Query(ctx, q, alertID)
	if err != nil {
		return nil, fmt.Errorf("storage: query firings: %w", err)
	}
	defer rows.Close()

	var out []Firing
	for rows.Next() {
		var (
			f      Firing
			status string
		)
		if err := rows.Scan(&f.ID, &f.AlertID, &f.DeviceID, &f.FiredAt, &f.ActualValue, &status); err != nil {
			return nil, fmt.Errorf("storage: scan firing: %w", err)
		}
		f.DeliveryStatus = DeliveryStatus(status)
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate firings: %w", err)
	}
	return out, nil
}

func validDeliveryStatus(s DeliveryStatus) bool {
	switch s {
	case DeliveryPending, DeliveryDelivered, DeliveryFailed, DeliveryDropped:
		return true
	}
	return false
}
