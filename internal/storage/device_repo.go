package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Device is the persisted shape of a registered iOS device.
type Device struct {
	DeviceID  uuid.UUID
	APNsToken string
	APNsEnv   string // "development" | "production"
	Locale    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DeviceRepo persists and queries devices in Postgres.
type DeviceRepo struct {
	pool *pgxpool.Pool
}

// NewDeviceRepo wires a repo against an existing connection pool.
func NewDeviceRepo(pool *pgxpool.Pool) *DeviceRepo {
	return &DeviceRepo{pool: pool}
}

// ErrDeviceNotFound is returned by Get when no device matches the id.
var ErrDeviceNotFound = errors.New("storage: device not found")

// Upsert inserts a new device or updates the apns_token / apns_env / locale
// fields when one with the same device_id already exists. updated_at always
// follows the current transaction time on conflict.
func (r *DeviceRepo) Upsert(ctx context.Context, d Device) error {
	if d.DeviceID == uuid.Nil {
		return errors.New("storage: device_id is nil")
	}
	if d.APNsToken == "" {
		return errors.New("storage: apns_token is empty")
	}
	if d.APNsEnv != "development" && d.APNsEnv != "production" {
		return fmt.Errorf("storage: invalid apns_env %q", d.APNsEnv)
	}
	locale := d.Locale
	if locale == "" {
		locale = "en"
	}
	const q = `
		INSERT INTO devices (device_id, apns_token, apns_env, locale)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (device_id) DO UPDATE SET
		    apns_token = EXCLUDED.apns_token,
		    apns_env   = EXCLUDED.apns_env,
		    locale     = EXCLUDED.locale,
		    updated_at = NOW()
	`
	if _, err := r.pool.Exec(ctx, q, d.DeviceID, d.APNsToken, d.APNsEnv, locale); err != nil {
		return fmt.Errorf("storage: upsert device: %w", err)
	}
	return nil
}

// Get returns the device with the given id, or ErrDeviceNotFound.
func (r *DeviceRepo) Get(ctx context.Context, id uuid.UUID) (*Device, error) {
	const q = `
		SELECT device_id, apns_token, apns_env, locale, created_at, updated_at
		FROM devices WHERE device_id = $1
	`
	var d Device
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&d.DeviceID, &d.APNsToken, &d.APNsEnv, &d.Locale, &d.CreatedAt, &d.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDeviceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get device: %w", err)
	}
	return &d, nil
}

// Delete removes the device (cascading to its alerts, holdings, firings).
// Returns nil if the device didn't exist (idempotent).
func (r *DeviceRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM devices WHERE device_id = $1`, id); err != nil {
		return fmt.Errorf("storage: delete device: %w", err)
	}
	return nil
}
