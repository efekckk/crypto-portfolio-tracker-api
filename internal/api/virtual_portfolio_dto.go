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

// virtualQuoteResponse is the body of GET /v1/virtual/portfolios/{id}/quote.
type virtualQuoteResponse struct {
	CoinID        string    `json:"coin_id"`
	CoinName      string    `json:"coin_name"`
	Price         float64   `json:"price"`
	FetchedAt     time.Time `json:"fetched_at"`
	MaxBuyAmount  float64   `json:"max_buy_amount"`
	MaxSellAmount float64   `json:"max_sell_amount"`
}

// executeTradeRequest is the body of POST /v1/virtual/portfolios/{id}/trades.
type executeTradeRequest struct {
	Side   string  `json:"side"`
	CoinID string  `json:"coin_id"`
	Amount float64 `json:"amount"`
}

// virtualTradeDTO is the trade row returned in the trade response and in
// the trade history endpoint.
type virtualTradeDTO struct {
	ID         int64     `json:"id"`
	Side       string    `json:"side"`
	CoinID     string    `json:"coin_id"`
	Amount     float64   `json:"amount"`
	Price      float64   `json:"price"`
	ExecutedAt time.Time `json:"executed_at"`
}

// executeTradeResponse carries the newly inserted trade alongside the
// portfolio's post-trade snapshot so the client doesn't need a follow-up
// GET to refresh its view.
type executeTradeResponse struct {
	Trade     virtualTradeDTO                `json:"trade"`
	Portfolio virtualPortfolioDetailResponse `json:"portfolio"`
}

// virtualTradeHistoryResponse is the body of the trade history endpoint.
// NextCursor is a pointer so the wire format carries `null` when there are
// no further pages.
type virtualTradeHistoryResponse struct {
	Trades     []virtualTradeDTO `json:"trades"`
	NextCursor *int64            `json:"next_cursor"`
}
