package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
)

// AlertRepo persists and queries alerts. condition and recurrence are stored
// as JSONB blobs using the domain package's discriminated-union helpers, so
// the wire format on disk matches the wire format on the network.
type AlertRepo struct {
	pool *pgxpool.Pool
}

// NewAlertRepo wires a repo against an existing connection pool.
func NewAlertRepo(pool *pgxpool.Pool) *AlertRepo {
	return &AlertRepo{pool: pool}
}

// ErrAlertNotFound is returned by Get when no alert matches.
var ErrAlertNotFound = errors.New("storage: alert not found")

// Upsert inserts a new alert or replaces an existing one (matched by id).
// The caller is expected to supply a stable client-generated UUID for `alert.ID`.
func (r *AlertRepo) Upsert(ctx context.Context, deviceID uuid.UUID, alert domain.PriceAlert) error {
	if deviceID == uuid.Nil {
		return errors.New("storage: device_id is nil")
	}
	id, err := uuid.Parse(alert.ID)
	if err != nil {
		return fmt.Errorf("storage: invalid alert id %q: %w", alert.ID, err)
	}
	if alert.Condition == nil {
		return errors.New("storage: alert condition is nil")
	}
	if alert.Recurrence == nil {
		return errors.New("storage: alert recurrence is nil")
	}
	condJSON, err := domain.MarshalAlertCondition(alert.Condition)
	if err != nil {
		return fmt.Errorf("storage: marshal condition: %w", err)
	}
	recJSON, err := domain.MarshalRecurrence(alert.Recurrence)
	if err != nil {
		return fmt.Errorf("storage: marshal recurrence: %w", err)
	}

	const q = `
		INSERT INTO alerts (id, device_id, condition_json, recurrence_json,
		                    is_active, fired_at, last_condition_result)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
		    condition_json        = EXCLUDED.condition_json,
		    recurrence_json       = EXCLUDED.recurrence_json,
		    is_active             = EXCLUDED.is_active,
		    fired_at              = EXCLUDED.fired_at,
		    last_condition_result = EXCLUDED.last_condition_result,
		    updated_at            = NOW()
	`
	_, err = r.pool.Exec(ctx, q,
		id, deviceID, condJSON, recJSON, alert.IsActive, alert.FiredAt, alert.LastConditionResult,
	)
	if err != nil {
		return fmt.Errorf("storage: upsert alert: %w", err)
	}
	return nil
}

// Get loads a single alert by id, or ErrAlertNotFound.
func (r *AlertRepo) Get(ctx context.Context, id uuid.UUID) (*domain.PriceAlert, error) {
	const q = `
		SELECT id, condition_json, recurrence_json,
		       is_active, fired_at, last_condition_result
		FROM alerts WHERE id = $1
	`
	var (
		rawID     uuid.UUID
		condJSON  []byte
		recJSON   []byte
		isActive  bool
		firedAt   *time.Time
		lastCondR *bool
	)
	err := r.pool.QueryRow(ctx, q, id).Scan(&rawID, &condJSON, &recJSON, &isActive, &firedAt, &lastCondR)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAlertNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get alert: %w", err)
	}
	cond, err := domain.UnmarshalAlertCondition(condJSON)
	if err != nil {
		return nil, fmt.Errorf("storage: decode condition: %w", err)
	}
	rec, err := domain.UnmarshalRecurrence(recJSON)
	if err != nil {
		return nil, fmt.Errorf("storage: decode recurrence: %w", err)
	}
	return &domain.PriceAlert{
		ID:                  rawID.String(),
		Condition:           cond,
		Recurrence:          rec,
		IsActive:            isActive,
		FiredAt:             firedAt,
		LastConditionResult: lastCondR,
	}, nil
}

// ListByDevice returns every alert for a device, sorted by id for determinism.
func (r *AlertRepo) ListByDevice(ctx context.Context, deviceID uuid.UUID) ([]domain.PriceAlert, error) {
	const q = `
		SELECT id, condition_json, recurrence_json,
		       is_active, fired_at, last_condition_result
		FROM alerts
		WHERE device_id = $1
		ORDER BY id
	`
	return r.queryAlerts(ctx, q, deviceID)
}

// ActiveAlert pairs a device id with an alert; the cron worker needs both to
// route push notifications.
type ActiveAlert struct {
	DeviceID uuid.UUID
	Alert    domain.PriceAlert
}

// ListActive returns every is_active=true alert in the system, paired with
// its device id. The cron tick reads through this list each pass.
func (r *AlertRepo) ListActive(ctx context.Context) ([]ActiveAlert, error) {
	const q = `
		SELECT id, device_id, condition_json, recurrence_json,
		       is_active, fired_at, last_condition_result
		FROM alerts
		WHERE is_active = true
		ORDER BY device_id, id
	`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("storage: query active alerts: %w", err)
	}
	defer rows.Close()

	var out []ActiveAlert
	for rows.Next() {
		var (
			id, devID uuid.UUID
			condJSON  []byte
			recJSON   []byte
			isActive  bool
			firedAt   *time.Time
			lastCondR *bool
		)
		if err := rows.Scan(&id, &devID, &condJSON, &recJSON, &isActive, &firedAt, &lastCondR); err != nil {
			return nil, fmt.Errorf("storage: scan active alert: %w", err)
		}
		cond, err := domain.UnmarshalAlertCondition(condJSON)
		if err != nil {
			return nil, fmt.Errorf("storage: decode condition: %w", err)
		}
		rec, err := domain.UnmarshalRecurrence(recJSON)
		if err != nil {
			return nil, fmt.Errorf("storage: decode recurrence: %w", err)
		}
		out = append(out, ActiveAlert{
			DeviceID: devID,
			Alert: domain.PriceAlert{
				ID:                  id.String(),
				Condition:           cond,
				Recurrence:          rec,
				IsActive:            isActive,
				FiredAt:             firedAt,
				LastConditionResult: lastCondR,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate active alerts: %w", err)
	}
	return out, nil
}

// UpdateState mutates an alert's runtime state (firedAt, isActive,
// lastConditionResult) without touching its condition/recurrence. The
// cron tick calls this after each firing decision.
func (r *AlertRepo) UpdateState(ctx context.Context, id uuid.UUID, isActive bool, firedAt *time.Time, lastCondResult *bool) error {
	const q = `
		UPDATE alerts SET
		    is_active             = $2,
		    fired_at              = $3,
		    last_condition_result = $4,
		    updated_at            = NOW()
		WHERE id = $1
	`
	tag, err := r.pool.Exec(ctx, q, id, isActive, firedAt, lastCondResult)
	if err != nil {
		return fmt.Errorf("storage: update alert state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlertNotFound
	}
	return nil
}

// Delete removes an alert by id. Idempotent (no error if missing).
func (r *AlertRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM alerts WHERE id = $1`, id); err != nil {
		return fmt.Errorf("storage: delete alert: %w", err)
	}
	return nil
}

// queryAlerts is a shared scan loop for queries that return a single device's
// alerts in the canonical column order.
func (r *AlertRepo) queryAlerts(ctx context.Context, q string, args ...any) ([]domain.PriceAlert, error) {
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: query alerts: %w", err)
	}
	defer rows.Close()

	var out []domain.PriceAlert
	for rows.Next() {
		var (
			id        uuid.UUID
			condJSON  []byte
			recJSON   []byte
			isActive  bool
			firedAt   *time.Time
			lastCondR *bool
		)
		if err := rows.Scan(&id, &condJSON, &recJSON, &isActive, &firedAt, &lastCondR); err != nil {
			return nil, fmt.Errorf("storage: scan alert: %w", err)
		}
		cond, err := domain.UnmarshalAlertCondition(condJSON)
		if err != nil {
			return nil, fmt.Errorf("storage: decode condition: %w", err)
		}
		rec, err := domain.UnmarshalRecurrence(recJSON)
		if err != nil {
			return nil, fmt.Errorf("storage: decode recurrence: %w", err)
		}
		out = append(out, domain.PriceAlert{
			ID:                  id.String(),
			Condition:           cond,
			Recurrence:          rec,
			IsActive:            isActive,
			FiredAt:             firedAt,
			LastConditionResult: lastCondR,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate alerts: %w", err)
	}
	return out, nil
}
