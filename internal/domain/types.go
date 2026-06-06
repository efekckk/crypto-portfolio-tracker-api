package domain

import "time"

// Direction is the comparison direction for thresholded alerts.
type Direction string

const (
	Above Direction = "above"
	Below Direction = "below"
)

// PercentWindow is the time window for percent-change alerts.
type PercentWindow string

const (
	Window24h PercentWindow = "h24"
	Window7d  PercentWindow = "d7"
	Window30d PercentWindow = "d30"
)

// Coin is a market snapshot returned by the markets endpoint.
type Coin struct {
	ID                       string   `json:"id"`
	Symbol                   string   `json:"symbol"`
	Name                     string   `json:"name"`
	CurrentPrice             float64  `json:"current_price"`
	PriceChangePercentage24h *float64 `json:"price_change_percentage_24h,omitempty"`
	PriceChangePercentage7d  *float64 `json:"price_change_percentage_7d,omitempty"`
	PriceChangePercentage30d *float64 `json:"price_change_percentage_30d,omitempty"`
}

// Holding is a single position in the user's portfolio.
type Holding struct {
	CoinID          string    `json:"coin_id"`
	Amount          float64   `json:"amount"`
	AverageBuyPrice float64   `json:"average_buy_price"`
	DateAdded       time.Time `json:"date_added"`
}

// PriceAlert pairs a condition with a recurrence and the runtime state needed
// to enforce that recurrence across evaluation passes.
type PriceAlert struct {
	ID                  string         `json:"id"`
	Condition           AlertCondition `json:"condition"`
	Recurrence          Recurrence     `json:"recurrence"`
	IsActive            bool           `json:"is_active"`
	FiredAt             *time.Time     `json:"fired_at,omitempty"`
	LastConditionResult *bool          `json:"last_condition_result,omitempty"`
}
