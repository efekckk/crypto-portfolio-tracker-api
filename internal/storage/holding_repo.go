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

// HoldingRepo persists and queries holdings in Postgres.
type HoldingRepo struct {
	pool *pgxpool.Pool
}

// NewHoldingRepo wires a repo against an existing connection pool.
func NewHoldingRepo(pool *pgxpool.Pool) *HoldingRepo {
	return &HoldingRepo{pool: pool}
}

// ErrHoldingNotFound is returned by Get when no holding matches.
var ErrHoldingNotFound = errors.New("storage: holding not found")

// Upsert inserts or updates a holding for a (device_id, coin_id) pair.
func (r *HoldingRepo) Upsert(ctx context.Context, deviceID uuid.UUID, h domain.Holding) error {
	if deviceID == uuid.Nil {
		return errors.New("storage: device_id is nil")
	}
	if h.CoinID == "" {
		return errors.New("storage: coin_id is empty")
	}
	dateAdded := h.DateAdded
	if dateAdded.IsZero() {
		dateAdded = time.Now().UTC()
	}
	const q = `
		INSERT INTO holdings (device_id, coin_id, amount, average_buy_price, date_added)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (device_id, coin_id) DO UPDATE SET
		    amount            = EXCLUDED.amount,
		    average_buy_price = EXCLUDED.average_buy_price,
		    date_added        = EXCLUDED.date_added,
		    updated_at        = NOW()
	`
	if _, err := r.pool.Exec(ctx, q, deviceID, h.CoinID, h.Amount, h.AverageBuyPrice, dateAdded); err != nil {
		return fmt.Errorf("storage: upsert holding: %w", err)
	}
	return nil
}

// Get returns the holding for a (device_id, coin_id), or ErrHoldingNotFound.
func (r *HoldingRepo) Get(ctx context.Context, deviceID uuid.UUID, coinID string) (*domain.Holding, error) {
	const q = `
		SELECT coin_id, amount, average_buy_price, date_added
		FROM holdings
		WHERE device_id = $1 AND coin_id = $2
	`
	var h domain.Holding
	err := r.pool.QueryRow(ctx, q, deviceID, coinID).Scan(
		&h.CoinID, &h.Amount, &h.AverageBuyPrice, &h.DateAdded,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrHoldingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get holding: %w", err)
	}
	return &h, nil
}

// ListByDevice returns all holdings for a device, ordered by coin_id.
func (r *HoldingRepo) ListByDevice(ctx context.Context, deviceID uuid.UUID) ([]domain.Holding, error) {
	const q = `
		SELECT coin_id, amount, average_buy_price, date_added
		FROM holdings
		WHERE device_id = $1
		ORDER BY coin_id
	`
	rows, err := r.pool.Query(ctx, q, deviceID)
	if err != nil {
		return nil, fmt.Errorf("storage: list holdings: %w", err)
	}
	defer rows.Close()

	var out []domain.Holding
	for rows.Next() {
		var h domain.Holding
		if err := rows.Scan(&h.CoinID, &h.Amount, &h.AverageBuyPrice, &h.DateAdded); err != nil {
			return nil, fmt.Errorf("storage: scan holding: %w", err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate holdings: %w", err)
	}
	return out, nil
}

// Delete removes the holding for the given (device_id, coin_id). Idempotent.
func (r *HoldingRepo) Delete(ctx context.Context, deviceID uuid.UUID, coinID string) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM holdings WHERE device_id = $1 AND coin_id = $2`, deviceID, coinID); err != nil {
		return fmt.Errorf("storage: delete holding: %w", err)
	}
	return nil
}
