package eval

import (
	"time"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
)

// evalResult is the output of evaluating a condition against the market and
// portfolio data: whether the condition is true, and the measured value the
// notification body will reference.
type evalResult struct {
	conditionTrue bool
	actualValue   *float64
}

// evaluate returns the result and `ok=true`, or `ok=false` when data needed
// to evaluate the condition is missing (coin not in markets response, percent
// window field is nil, no portfolio summary). The caller treats nil as "no
// state change this pass".
func evaluate(c domain.AlertCondition, coinsByID map[string]domain.Coin, summary *portfolioSummary) (evalResult, bool) {
	switch v := c.(type) {
	case domain.PriceCrossing:
		coin, ok := coinsByID[v.CoinID]
		if !ok {
			return evalResult{}, false
		}
		measured := coin.CurrentPrice
		return evalResult{conditionTrue: crossed(measured, v.TargetPrice, v.Direction), actualValue: &measured}, true

	case domain.PercentChange:
		coin, ok := coinsByID[v.CoinID]
		if !ok {
			return evalResult{}, false
		}
		var ptr *float64
		switch v.Window {
		case domain.Window24h:
			ptr = coin.PriceChangePercentage24h
		case domain.Window7d:
			ptr = coin.PriceChangePercentage7d
		case domain.Window30d:
			ptr = coin.PriceChangePercentage30d
		}
		if ptr == nil {
			return evalResult{}, false
		}
		measured := *ptr
		return evalResult{conditionTrue: crossed(measured, v.Threshold, v.Direction), actualValue: &measured}, true

	case domain.PortfolioValue:
		if summary == nil {
			return evalResult{}, false
		}
		measured := summary.totalValue
		return evalResult{conditionTrue: crossed(measured, v.Threshold, v.Direction), actualValue: &measured}, true

	case domain.PortfolioPnLPercent:
		if summary == nil {
			return evalResult{}, false
		}
		measured := summary.percentPnL
		return evalResult{conditionTrue: crossed(measured, v.Threshold, v.Direction), actualValue: &measured}, true
	}
	return evalResult{}, false
}

// crossed reports whether `value` meets the threshold in the given direction.
// `above` is inclusive at the boundary (>= target); `below` is inclusive at
// the boundary (<= target). This matches the iOS v1.1 behaviour.
func crossed(value, target float64, dir domain.Direction) bool {
	switch dir {
	case domain.Above:
		return value >= target
	case domain.Below:
		return value <= target
	}
	return false
}

// shouldFire is the recurrence state machine. Assumes `conditionTrue` is the
// freshly evaluated truth bit; returns whether the alert should fire NOW.
func shouldFire(alert domain.PriceAlert, conditionTrue bool, now time.Time) bool {
	if !conditionTrue {
		return false
	}
	switch r := alert.Recurrence.(type) {
	case domain.OneShot:
		return alert.FiredAt == nil
	case domain.Cooldown:
		if alert.FiredAt == nil {
			return true
		}
		return now.Sub(*alert.FiredAt) >= time.Duration(r.Seconds)*time.Second
	case domain.OnCrossing:
		// Fires on false→true transition; nil treated as "previously false".
		return alert.LastConditionResult == nil || !*alert.LastConditionResult
	}
	return false
}

// coinName returns a presentable coin name for coin-bound condition variants;
// nil for portfolio variants or when the coin isn't in the markets response.
func coinName(c domain.AlertCondition, coinsByID map[string]domain.Coin) *string {
	var id string
	switch v := c.(type) {
	case domain.PriceCrossing:
		id = v.CoinID
	case domain.PercentChange:
		id = v.CoinID
	default:
		return nil
	}
	coin, ok := coinsByID[id]
	if !ok {
		return nil
	}
	name := coin.Name
	return &name
}
