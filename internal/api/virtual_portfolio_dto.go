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
