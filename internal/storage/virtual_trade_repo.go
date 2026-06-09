package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/virtual"
)

// VirtualTradeRepo records executed paper trades. Rows are immutable — there
// is no UPDATE/DELETE on individual trades; deleting the parent portfolio
// cascades.
type VirtualTradeRepo struct {
	pool *pgxpool.Pool
}

func NewVirtualTradeRepo(pool *pgxpool.Pool) *VirtualTradeRepo {
	return &VirtualTradeRepo{pool: pool}
}

// Insert records a new trade and returns the server-assigned BIGSERIAL id.
// PortfolioID, Side, CoinID, Amount, Price, ExecutedAt must all be set.
func (r *VirtualTradeRepo) Insert(ctx context.Context, t virtual.Trade) (int64, error) {
	if t.PortfolioID == uuid.Nil {
		return 0, errors.New("storage: portfolio_id is nil")
	}
	if t.Side != virtual.SideBuy && t.Side != virtual.SideSell {
		return 0, fmt.Errorf("storage: invalid side %q", t.Side)
	}
	if t.CoinID == "" {
		return 0, errors.New("storage: coin_id is empty")
	}
	if t.Amount <= 0 {
		return 0, errors.New("storage: amount must be positive")
	}
	if t.Price <= 0 {
		return 0, errors.New("storage: price must be positive")
	}
	if t.ExecutedAt.IsZero() {
		return 0, errors.New("storage: executed_at is zero")
	}
	const q = `
		INSERT INTO virtual_trades (portfolio_id, side, coin_id, amount, price, executed_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`
	var id int64
	if err := r.pool.QueryRow(ctx, q,
		t.PortfolioID, string(t.Side), t.CoinID, t.Amount, t.Price, t.ExecutedAt,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("storage: insert virtual trade: %w", err)
	}
	return id, nil
}

// ListAllByPortfolio returns every trade for the portfolio in chronological
// order (executed_at ASC, id ASC). The Compute fold expects this order. Use
// this for state derivation, not for paginated UI history.
func (r *VirtualTradeRepo) ListAllByPortfolio(ctx context.Context, portfolioID uuid.UUID) ([]virtual.Trade, error) {
	const q = `
		SELECT id, portfolio_id, side, coin_id, amount, price, executed_at
		FROM virtual_trades
		WHERE portfolio_id = $1
		ORDER BY executed_at ASC, id ASC
	`
	rows, err := r.pool.Query(ctx, q, portfolioID)
	if err != nil {
		return nil, fmt.Errorf("storage: query virtual trades: %w", err)
	}
	defer rows.Close()

	var out []virtual.Trade
	for rows.Next() {
		var (
			t    virtual.Trade
			side string
		)
		if err := rows.Scan(&t.ID, &t.PortfolioID, &side, &t.CoinID, &t.Amount, &t.Price, &t.ExecutedAt); err != nil {
			return nil, fmt.Errorf("storage: scan virtual trade: %w", err)
		}
		t.Side = virtual.Side(side)
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate virtual trades: %w", err)
	}
	return out, nil
}

// ListPage returns up to `limit` trades for the portfolio, ordered newest
// first by id DESC. Since id is a monotonically increasing BIGSERIAL set
// at insert time, id-order matches executed_at-order in practice and lets
// the cursor predicate stay a simple `id < $N`. beforeID is exclusive:
// pass the last `id` from the previous page to fetch the next page; pass
// 0 for the first page. Returns the trades plus the next cursor (0 when
// no more rows).
func (r *VirtualTradeRepo) ListPage(ctx context.Context, portfolioID uuid.UUID, beforeID int64, limit int) ([]virtual.Trade, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var (
		rows interface {
			Next() bool
			Scan(dest ...any) error
			Close()
			Err() error
		}
		err error
	)
	if beforeID > 0 {
		const q = `
			SELECT id, portfolio_id, side, coin_id, amount, price, executed_at
			FROM virtual_trades
			WHERE portfolio_id = $1 AND id < $2
			ORDER BY id DESC
			LIMIT $3
		`
		rows, err = r.pool.Query(ctx, q, portfolioID, beforeID, limit)
	} else {
		const q = `
			SELECT id, portfolio_id, side, coin_id, amount, price, executed_at
			FROM virtual_trades
			WHERE portfolio_id = $1
			ORDER BY id DESC
			LIMIT $2
		`
		rows, err = r.pool.Query(ctx, q, portfolioID, limit)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("storage: query virtual trades page: %w", err)
	}
	defer rows.Close()

	var out []virtual.Trade
	for rows.Next() {
		var (
			t    virtual.Trade
			side string
		)
		if err := rows.Scan(&t.ID, &t.PortfolioID, &side, &t.CoinID, &t.Amount, &t.Price, &t.ExecutedAt); err != nil {
			return nil, 0, fmt.Errorf("storage: scan virtual trade page: %w", err)
		}
		t.Side = virtual.Side(side)
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("storage: iterate virtual trade page: %w", err)
	}

	var nextCursor int64
	if len(out) == limit {
		nextCursor = out[len(out)-1].ID
	}
	return out, nextCursor, nil
}

// CountByPortfolio returns the number of trades on the portfolio. Handy for
// the list endpoint's `trade_count` summary field.
func (r *VirtualTradeRepo) CountByPortfolio(ctx context.Context, portfolioID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM virtual_trades WHERE portfolio_id = $1`,
		portfolioID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("storage: count virtual trades: %w", err)
	}
	return n, nil
}
