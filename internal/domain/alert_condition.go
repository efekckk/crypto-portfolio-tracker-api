package domain

import (
	"encoding/json"
	"fmt"
)

// AlertCondition is the closed set of conditions an alert can fire on.
// Use `MarshalAlertCondition` / `UnmarshalAlertCondition` for JSON; do NOT
// json.Marshal the interface directly (you'll get an empty object).
type AlertCondition interface {
	isAlertCondition()
	// RequiredCoinIDs returns the coin ids needed to evaluate this condition.
	// Returns nil for portfolio-level variants (they need the holdings set).
	RequiredCoinIDs() []string
}

// PriceCrossing fires when a coin's current price crosses a target.
type PriceCrossing struct {
	CoinID      string    `json:"coin_id"`
	Direction   Direction `json:"direction"`
	TargetPrice float64   `json:"target_price"`
}

func (PriceCrossing) isAlertCondition()          {}
func (c PriceCrossing) RequiredCoinIDs() []string { return []string{c.CoinID} }

// PercentChange fires when a coin's percent move in a window crosses a threshold.
type PercentChange struct {
	CoinID    string        `json:"coin_id"`
	Direction Direction     `json:"direction"`
	Window    PercentWindow `json:"window"`
	Threshold float64       `json:"threshold"`
}

func (PercentChange) isAlertCondition()          {}
func (c PercentChange) RequiredCoinIDs() []string { return []string{c.CoinID} }

// PortfolioValue fires when the user's total portfolio value crosses a threshold.
type PortfolioValue struct {
	Direction Direction `json:"direction"`
	Threshold float64   `json:"threshold"`
}

func (PortfolioValue) isAlertCondition()          {}
func (PortfolioValue) RequiredCoinIDs() []string  { return nil }

// PortfolioPnLPercent fires when the user's overall P/L percent crosses a threshold.
type PortfolioPnLPercent struct {
	Direction Direction `json:"direction"`
	Threshold float64   `json:"threshold"`
}

func (PortfolioPnLPercent) isAlertCondition()          {}
func (PortfolioPnLPercent) RequiredCoinIDs() []string  { return nil }

// MarshalAlertCondition serialises a condition with a `"type"` discriminator
// merged into the variant's own fields.
func MarshalAlertCondition(c AlertCondition) ([]byte, error) {
	var (
		typ   string
		inner any
	)
	switch v := c.(type) {
	case PriceCrossing:
		typ, inner = "priceCrossing", v
	case PercentChange:
		typ, inner = "percentChange", v
	case PortfolioValue:
		typ, inner = "portfolioValue", v
	case PortfolioPnLPercent:
		typ, inner = "portfolioPnLPercent", v
	default:
		return nil, fmt.Errorf("domain: cannot marshal condition of type %T", c)
	}

	raw, err := json.Marshal(inner)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	m["type"] = typ
	return json.Marshal(m)
}

// UnmarshalAlertCondition parses a condition by reading the `"type"` field
// and decoding into the matching concrete struct.
func UnmarshalAlertCondition(data []byte) (AlertCondition, error) {
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("domain: decode condition envelope: %w", err)
	}
	switch env.Type {
	case "priceCrossing":
		var v PriceCrossing
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return v, nil
	case "percentChange":
		var v PercentChange
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return v, nil
	case "portfolioValue":
		var v PortfolioValue
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return v, nil
	case "portfolioPnLPercent":
		var v PortfolioPnLPercent
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return v, nil
	case "":
		return nil, fmt.Errorf("domain: condition missing required %q field", "type")
	default:
		return nil, fmt.Errorf("domain: unknown condition type %q", env.Type)
	}
}
