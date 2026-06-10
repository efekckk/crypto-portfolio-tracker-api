package api

import "time"

// createVirtualPortfolioRequest is the body of POST /v1/virtual/portfolios.
type createVirtualPortfolioRequest struct {
	Name            string  `json:"name"`
	StartingBalance float64 `json:"starting_balance"`
}

// virtualPortfolioCreateResponse confirms the new portfolio's metadata.
type virtualPortfolioCreateResponse struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	StartingBalance float64   `json:"starting_balance"`
	CreatedAt       time.Time `json:"created_at"`
}

// virtualPortfolioSummary is one row of the list endpoint payload — meta
// plus a small computed snapshot so the UI's portfolio cards don't have to
// round-trip a detail call per portfolio.
type virtualPortfolioSummary struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	StartingBalance float64   `json:"starting_balance"`
	CashBalance     float64   `json:"cash_balance"`
	TotalValue      float64   `json:"total_value"`
	TotalPnL        float64   `json:"total_pnl"`
	TotalPnLPercent float64   `json:"total_pnl_percent"`
	TradeCount      int       `json:"trade_count"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// virtualPortfoliosListResponse wraps the list endpoint payload.
type virtualPortfoliosListResponse struct {
	Portfolios []virtualPortfolioSummary `json:"portfolios"`
}

// virtualHoldingDTO is one coin position in the detail response. Per-coin
// current price metrics are pointers so the wire format carries `null`
// (not `0`) when the coin isn't in the markets snapshot.
type virtualHoldingDTO struct {
	CoinID               string   `json:"coin_id"`
	Amount               float64  `json:"amount"`
	AverageBuyPrice      float64  `json:"average_buy_price"`
	CurrentPrice         *float64 `json:"current_price"`
	CurrentValue         *float64 `json:"current_value"`
	UnrealizedPnL        *float64 `json:"unrealized_pnl"`
	UnrealizedPnLPercent *float64 `json:"unrealized_pnl_percent"`
}

// virtualPortfolioDetailResponse is the full detail payload for the
// portfolio's "show" endpoint.
type virtualPortfolioDetailResponse struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	StartingBalance float64             `json:"starting_balance"`
	CashBalance     float64             `json:"cash_balance"`
	TotalValue      float64             `json:"total_value"`
	RealizedPnL     float64             `json:"realized_pnl"`
	UnrealizedPnL   float64             `json:"unrealized_pnl"`
	TotalPnLPercent float64             `json:"total_pnl_percent"`
	Holdings        []virtualHoldingDTO `json:"holdings"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}
