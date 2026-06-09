// Package virtual owns the paper-trading domain: portfolios, trades, and the
// pure-function trade math that derives current state from the trade log.
package virtual

import (
	"time"

	"github.com/google/uuid"
)

// Side is the direction of a single trade.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// Portfolio is the persistent metadata for a virtual portfolio. The runtime
// state (cash, holdings, P/L) is derived from trades, not stored here.
type Portfolio struct {
	ID              uuid.UUID
	DeviceID        uuid.UUID
	Name            string
	StartingBalance float64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Trade is one executed buy or sell. Amount is always positive; direction is
// encoded by Side. Price is the USD per unit at fill time, written by the
// server (clients can't supply it).
type Trade struct {
	ID          int64
	PortfolioID uuid.UUID
	Side        Side
	CoinID      string
	Amount      float64
	Price       float64
	ExecutedAt  time.Time
}

// HoldingPosition is one coin in a portfolio after folding all trades.
// CurrentPrice/CurrentValue/UnrealizedPnL are nil when the coin wasn't in
// the markets response (delisted, typo) — the API surfaces them as "—".
type HoldingPosition struct {
	CoinID               string
	Amount               float64
	AverageBuyPrice      float64
	CurrentPrice         *float64
	CurrentValue         *float64
	UnrealizedPnL        *float64
	UnrealizedPnLPercent *float64
}

// ComputedState is everything the read endpoints surface beyond Portfolio
// metadata: cash, holdings, realized + unrealized P/L, total value.
type ComputedState struct {
	CashBalance     float64
	Holdings        []HoldingPosition
	RealizedPnL     float64
	UnrealizedPnL   float64
	TotalValue      float64
	TotalPnLPercent float64
}

// Quote is the response from /quote — a snapshot of the current price for
// one coin plus the buy/sell amount limits the user can act on.
type Quote struct {
	CoinID        string
	CoinName      string
	Price         float64
	FetchedAt     time.Time
	MaxBuyAmount  float64
	MaxSellAmount float64
}
