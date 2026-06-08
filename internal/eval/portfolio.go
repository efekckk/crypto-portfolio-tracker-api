package eval

import (
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
)

// portfolioSummary mirrors the iOS PortfolioSummary type: total value and
// percent P/L derived from holdings + current coin prices. Missing coins
// fall back to a current price of 0 — same trade-off the iOS evaluator
// already documents.
type portfolioSummary struct {
	totalValue float64
	totalCost  float64
	percentPnL float64
}

func buildSummary(holdings []domain.Holding, coinsByID map[string]domain.Coin) portfolioSummary {
	var totalValue, totalCost float64
	for _, h := range holdings {
		var price float64
		if coin, ok := coinsByID[h.CoinID]; ok {
			price = coin.CurrentPrice
		}
		totalValue += h.Amount * price
		totalCost += h.Amount * h.AverageBuyPrice
	}
	pnl := totalValue - totalCost
	var pct float64
	if totalCost > 0 {
		pct = (pnl / totalCost) * 100
	}
	return portfolioSummary{totalValue: totalValue, totalCost: totalCost, percentPnL: pct}
}
