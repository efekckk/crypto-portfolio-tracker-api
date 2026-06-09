package virtual

import "sort"

// positionEpsilon is the largest residual amount we treat as "fully sold".
// Floating point math accumulates rounding error across many buys + sells;
// 1e-9 is well below any realistic crypto holding (smallest unit on most
// chains is 1e-8) yet large enough to swallow normal IEEE 754 drift.
const positionEpsilon = 1e-9

// posState is the internal running state for one coin during the fold.
type posState struct {
	amount    float64
	totalCost float64
}

// Compute folds trades (expected in executed_at ASC order) into a
// ComputedState. currentPrices maps coin_id → latest price; a missing key
// signals "no market data this pass" and surfaces as nil per-position
// metrics. Compute is pure — same inputs always yield the same output.
func Compute(startingBalance float64, trades []Trade, currentPrices map[string]float64) ComputedState {
	cash := startingBalance
	realized := 0.0
	positions := map[string]*posState{}

	for _, t := range trades {
		pos, ok := positions[t.CoinID]
		if !ok {
			pos = &posState{}
			positions[t.CoinID] = pos
		}
		switch t.Side {
		case SideBuy:
			cost := t.Amount * t.Price
			cash -= cost
			pos.amount += t.Amount
			pos.totalCost += cost
		case SideSell:
			revenue := t.Amount * t.Price
			avgCost := 0.0
			if pos.amount > 0 {
				avgCost = pos.totalCost / pos.amount
			}
			costOfSold := avgCost * t.Amount
			cash += revenue
			realized += revenue - costOfSold
			pos.amount -= t.Amount
			pos.totalCost -= costOfSold
		}
		if pos.amount <= positionEpsilon {
			delete(positions, t.CoinID)
		}
	}

	// Stable ordering by coin id so identical input ⇒ identical output bytes.
	coinIDs := make([]string, 0, len(positions))
	for id := range positions {
		coinIDs = append(coinIDs, id)
	}
	sort.Strings(coinIDs)

	holdings := make([]HoldingPosition, 0, len(coinIDs))
	unrealizedTotal := 0.0
	currentValueTotal := 0.0
	for _, id := range coinIDs {
		pos := positions[id]
		avgBuy := 0.0
		if pos.amount > 0 {
			avgBuy = pos.totalCost / pos.amount
		}
		h := HoldingPosition{
			CoinID:          id,
			Amount:          pos.amount,
			AverageBuyPrice: avgBuy,
		}
		if price, ok := currentPrices[id]; ok {
			value := pos.amount * price
			pnl := value - pos.totalCost
			var pct float64
			if pos.totalCost > 0 {
				pct = (pnl / pos.totalCost) * 100
			}
			h.CurrentPrice = &price
			h.CurrentValue = &value
			h.UnrealizedPnL = &pnl
			h.UnrealizedPnLPercent = &pct
			unrealizedTotal += pnl
			currentValueTotal += value
		}
		holdings = append(holdings, h)
	}

	totalValue := cash + currentValueTotal
	var totalPnLPercent float64
	if startingBalance > 0 {
		totalPnLPercent = ((totalValue - startingBalance) / startingBalance) * 100
	}

	return ComputedState{
		CashBalance:     cash,
		Holdings:        holdings,
		RealizedPnL:     realized,
		UnrealizedPnL:   unrealizedTotal,
		TotalValue:      totalValue,
		TotalPnLPercent: totalPnLPercent,
	}
}
