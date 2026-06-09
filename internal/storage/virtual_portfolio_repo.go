package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/virtual"
)

// VirtualPortfolioRepo persists virtual (paper-trading) portfolios. Runtime
// state (cash, holdings, P/L) is derived from trades by the virtual package
// — this repo only holds the metadata.
type VirtualPortfolioRepo struct {
	pool *pgxpool.Pool
}

func NewVirtualPortfolioRepo(pool *pgxpool.Pool) *VirtualPortfolioRepo {
	return &VirtualPortfolioRepo{pool: pool}
}

// ErrVirtualPortfolioNotFound is returned by Get when the id doesn't match
// any row, or when a portfolio belongs to a different device than the caller
// is allowed to see (the handler layer maps this to 404 or 403 as needed).
var ErrVirtualPortfolioNotFound = errors.New("storage: virtual portfolio not found")

// ErrVirtualPortfolioNameTaken is returned by Create when a portfolio with
// the same (device_id, name) pair already exists.
var ErrVirtualPortfolioNameTaken = errors.New("storage: virtual portfolio name already in use for this device")

// Create inserts a new portfolio. The caller is expected to supply a stable
// client-generated UUID for `p.ID` and a non-zero device id. Validation of
// name length and starting balance happens in the HTTP layer; the DB CHECK
// constraint on starting_balance > 0 is the safety net.
func (r *VirtualPortfolioRepo) Create(ctx context.Context, p virtual.Portfolio) error {
	if p.ID == uuid.Nil {
		return errors.New("storage: portfolio id is nil")
	}
	if p.DeviceID == uuid.Nil {
		return errors.New("storage: device_id is nil")
	}
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("storage: portfolio name is empty")
	}
	if p.StartingBalance <= 0 {
		return errors.New("storage: starting_balance must be positive")
	}
	const q = `
		INSERT INTO virtual_portfolios (id, device_id, name, starting_balance)
		VALUES ($1, $2, $3, $4)
	`
	if _, err := r.pool.Exec(ctx, q, p.ID, p.DeviceID, p.Name, p.StartingBalance); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// 23505 = unique_violation; the (device_id, name) UNIQUE constraint.
			return ErrVirtualPortfolioNameTaken
		}
		return fmt.Errorf("storage: insert virtual portfolio: %w", err)
	}
	return nil
}

// Get returns the portfolio with the given id, or ErrVirtualPortfolioNotFound.
func (r *VirtualPortfolioRepo) Get(ctx context.Context, id uuid.UUID) (*virtual.Portfolio, error) {
	const q = `
		SELECT id, device_id, name, starting_balance, created_at, updated_at
		FROM virtual_portfolios WHERE id = $1
	`
	var p virtual.Portfolio
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&p.ID, &p.DeviceID, &p.Name, &p.StartingBalance, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrVirtualPortfolioNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get virtual portfolio: %w", err)
	}
	return &p, nil
}

// ListByDevice returns every portfolio belonging to the device, ordered by
// created_at ASC so the response mirrors the order the user created them.
func (r *VirtualPortfolioRepo) ListByDevice(ctx context.Context, deviceID uuid.UUID) ([]virtual.Portfolio, error) {
	const q = `
		SELECT id, device_id, name, starting_balance, created_at, updated_at
		FROM virtual_portfolios
		WHERE device_id = $1
		ORDER BY created_at ASC, id ASC
	`
	rows, err := r.pool.Query(ctx, q, deviceID)
	if err != nil {
		return nil, fmt.Errorf("storage: list virtual portfolios: %w", err)
	}
	defer rows.Close()

	var out []virtual.Portfolio
	for rows.Next() {
		var p virtual.Portfolio
		if err := rows.Scan(&p.ID, &p.DeviceID, &p.Name, &p.StartingBalance, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("storage: scan virtual portfolio: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate virtual portfolios: %w", err)
	}
	return out, nil
}

// Delete removes the portfolio (cascading to its trades). Idempotent.
func (r *VirtualPortfolioRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM virtual_portfolios WHERE id = $1`, id); err != nil {
		return fmt.Errorf("storage: delete virtual portfolio: %w", err)
	}
	return nil
}

// CountByDevice returns how many portfolios the device owns. The handler
// uses this to enforce the per-device cap before insert.
func (r *VirtualPortfolioRepo) CountByDevice(ctx context.Context, deviceID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM virtual_portfolios WHERE device_id = $1`,
		deviceID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("storage: count virtual portfolios: %w", err)
	}
	return n, nil
}

// Touch updates `updated_at` to NOW. Useful when the surrounding feature
// changes the portfolio's state without rewriting its metadata directly.
// Returns ErrVirtualPortfolioNotFound when the id doesn't match.
func (r *VirtualPortfolioRepo) Touch(ctx context.Context, id uuid.UUID, now time.Time) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE virtual_portfolios SET updated_at = $2 WHERE id = $1`, id, now)
	if err != nil {
		return fmt.Errorf("storage: touch virtual portfolio: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrVirtualPortfolioNotFound
	}
	return nil
}
