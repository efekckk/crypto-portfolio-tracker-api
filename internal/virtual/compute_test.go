package virtual

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

// helpers --------------------------------------------------------------

func ts(min int) time.Time {
	return time.Date(2026, 1, 1, 0, min, 0, 0, time.UTC)
}

func tBuy(coinID string, amount, price float64, when time.Time) Trade {
	return Trade{
		PortfolioID: uuid.New(), Side: SideBuy,
		CoinID: coinID, Amount: amount, Price: price, ExecutedAt: when,
	}
}

func tSell(coinID string, amount, price float64, when time.Time) Trade {
	return Trade{
		PortfolioID: uuid.New(), Side: SideSell,
		CoinID: coinID, Amount: amount, Price: price, ExecutedAt: when,
	}
}

func near(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}

// holdingByCoin returns the holding for the given coin, or nil.
func holdingByCoin(s ComputedState, coinID string) *HoldingPosition {
	for i := range s.Holdings {
		if s.Holdings[i].CoinID == coinID {
			return &s.Holdings[i]
		}
	}
	return nil
}

// tests ----------------------------------------------------------------

func TestCompute_EmptyTrades_AllCashNoHoldings(t *testing.T) {
	s := Compute(10000, nil, nil)
	if s.CashBalance != 10000 {
		t.Fatalf("cash: %v", s.CashBalance)
	}
	if len(s.Holdings) != 0 {
		t.Fatalf("holdings: %+v", s.Holdings)
	}
	if s.TotalValue != 10000 {
		t.Fatalf("total value: %v", s.TotalValue)
	}
	if s.TotalPnLPercent != 0 {
		t.Fatalf("pnl percent: %v", s.TotalPnLPercent)
	}
}

func TestCompute_SingleBuy_CashDownHoldingsUp(t *testing.T) {
	s := Compute(10000, []Trade{
		tBuy("btc", 0.1, 70000, ts(1)),
	}, map[string]float64{"btc": 80000})

	if !near(s.CashBalance, 3000) {
		t.Fatalf("cash: %v", s.CashBalance)
	}
	h := holdingByCoin(s, "btc")
	if h == nil || !near(h.Amount, 0.1) || !near(h.AverageBuyPrice, 70000) {
		t.Fatalf("holding: %+v", h)
	}
	if h.CurrentPrice == nil || *h.CurrentPrice != 80000 {
		t.Fatalf("current price: %v", h.CurrentPrice)
	}
	if h.CurrentValue == nil || !near(*h.CurrentValue, 8000) {
		t.Fatalf("current value: %v", h.CurrentValue)
	}
	if h.UnrealizedPnL == nil || !near(*h.UnrealizedPnL, 1000) {
		t.Fatalf("unrealized: %v", h.UnrealizedPnL)
	}
	if !near(s.TotalValue, 11000) {
		t.Fatalf("total: %v", s.TotalValue)
	}
}

func TestCompute_TwoBuys_WeightedAverageCost(t *testing.T) {
	s := Compute(10000, []Trade{
		tBuy("btc", 0.1, 70000, ts(1)),
		tBuy("btc", 0.05, 80000, ts(2)),
	}, nil)
	h := holdingByCoin(s, "btc")
	if h == nil {
		t.Fatal("missing btc holding")
	}
	if !near(h.Amount, 0.15) {
		t.Fatalf("amount: %v", h.Amount)
	}
	// (0.1*70000 + 0.05*80000) / 0.15 = 11000 / 0.15 = 73333.33...
	if !near(h.AverageBuyPrice, 73333.333333) {
		t.Fatalf("avg buy: %v", h.AverageBuyPrice)
	}
}

func TestCompute_BuyThenSell_RealizedPnL(t *testing.T) {
	s := Compute(10000, []Trade{
		tBuy("btc", 0.1, 70000, ts(1)),
		tSell("btc", 0.05, 90000, ts(2)),
	}, nil)
	// avg cost = 70000; cost of sold = 0.05*70000 = 3500; revenue = 4500;
	// realized = 1000.
	if !near(s.RealizedPnL, 1000) {
		t.Fatalf("realized: %v", s.RealizedPnL)
	}
	h := holdingByCoin(s, "btc")
	if h == nil || !near(h.Amount, 0.05) {
		t.Fatalf("amount after sell: %+v", h)
	}
}

func TestCompute_SellAvgCostInvariant(t *testing.T) {
	// Two buys at different prices, then a partial sell — avg cost on the
	// remaining position should equal the pre-sell weighted average.
	s := Compute(100000, []Trade{
		tBuy("btc", 1, 60000, ts(1)),
		tBuy("btc", 1, 80000, ts(2)), // avg now 70000
		tSell("btc", 0.5, 100000, ts(3)),
	}, nil)
	h := holdingByCoin(s, "btc")
	if h == nil {
		t.Fatal("missing")
	}
	if !near(h.AverageBuyPrice, 70000) {
		t.Fatalf("avg cost should stay 70000, got %v", h.AverageBuyPrice)
	}
}

func TestCompute_FullSell_PositionDeleted(t *testing.T) {
	s := Compute(10000, []Trade{
		tBuy("btc", 0.1, 70000, ts(1)),
		tSell("btc", 0.1, 80000, ts(2)),
	}, nil)
	if h := holdingByCoin(s, "btc"); h != nil {
		t.Fatalf("expected no holding, got %+v", h)
	}
	// Realized = (0.1*80000) - (0.1*70000) = 1000
	if !near(s.RealizedPnL, 1000) {
		t.Fatalf("realized: %v", s.RealizedPnL)
	}
	// Cash = 10000 - 7000 + 8000 = 11000
	if !near(s.CashBalance, 11000) {
		t.Fatalf("cash: %v", s.CashBalance)
	}
}

func TestCompute_AmountEpsilon_TreatedAsFullySold(t *testing.T) {
	// Buy 0.1, sell 0.0999999998 — residual 2e-10 should round to fully sold.
	s := Compute(10000, []Trade{
		tBuy("btc", 0.1, 70000, ts(1)),
		tSell("btc", 0.0999999998, 80000, ts(2)),
	}, nil)
	if h := holdingByCoin(s, "btc"); h != nil {
		t.Fatalf("expected no holding (epsilon swallowed), got amount=%v", h.Amount)
	}
}

func TestCompute_MultiCoinInterleaved(t *testing.T) {
	s := Compute(20000, []Trade{
		tBuy("btc", 0.1, 60000, ts(1)),
		tBuy("eth", 2, 2000, ts(2)),
		tBuy("btc", 0.05, 80000, ts(3)),
		tSell("eth", 1, 2500, ts(4)),
	}, nil)
	btc := holdingByCoin(s, "btc")
	eth := holdingByCoin(s, "eth")
	if btc == nil || eth == nil {
		t.Fatalf("missing: btc=%v eth=%v", btc, eth)
	}
	if !near(btc.Amount, 0.15) {
		t.Fatalf("btc amount: %v", btc.Amount)
	}
	if !near(eth.Amount, 1) {
		t.Fatalf("eth amount: %v", eth.Amount)
	}
	// eth realized = 2500 - 2000 = 500
	if !near(s.RealizedPnL, 500) {
		t.Fatalf("realized: %v", s.RealizedPnL)
	}
}

func TestCompute_MissingCurrentPrice_NilHoldingMetrics(t *testing.T) {
	s := Compute(10000, []Trade{
		tBuy("btc", 0.1, 70000, ts(1)),
	}, nil) // no markets data
	h := holdingByCoin(s, "btc")
	if h == nil {
		t.Fatal("missing")
	}
	if h.CurrentPrice != nil || h.CurrentValue != nil || h.UnrealizedPnL != nil {
		t.Fatalf("expected nil current metrics, got %+v", h)
	}
	if s.UnrealizedPnL != 0 {
		t.Fatalf("expected zero unrealized total, got %v", s.UnrealizedPnL)
	}
	// Total value should still include cash (no current value contribution).
	if !near(s.TotalValue, 3000) {
		t.Fatalf("total value: %v", s.TotalValue)
	}
}

func TestCompute_HoldingsSortedByCoinID(t *testing.T) {
	s := Compute(10000, []Trade{
		tBuy("solana", 5, 80, ts(1)),
		tBuy("bitcoin", 0.1, 60000, ts(2)),
		tBuy("ethereum", 2, 2000, ts(3)),
	}, nil)
	want := []string{"bitcoin", "ethereum", "solana"}
	if len(s.Holdings) != len(want) {
		t.Fatalf("len: got %d want %d", len(s.Holdings), len(want))
	}
	for i, w := range want {
		if s.Holdings[i].CoinID != w {
			t.Fatalf("index %d: got %s want %s", i, s.Holdings[i].CoinID, w)
		}
	}
}

func TestCompute_UnrealizedPnLPercent_DividesByCost(t *testing.T) {
	s := Compute(10000, []Trade{
		tBuy("btc", 0.1, 70000, ts(1)),
	}, map[string]float64{"btc": 84000})
	h := holdingByCoin(s, "btc")
	// cost = 7000; value = 8400; pnl = 1400; pct = 20%.
	if h == nil || h.UnrealizedPnLPercent == nil || !near(*h.UnrealizedPnLPercent, 20) {
		t.Fatalf("pct: %+v", h)
	}
}

func TestCompute_TotalPnLPercent_AgainstStartingBalance(t *testing.T) {
	s := Compute(10000, []Trade{
		tBuy("btc", 0.1, 70000, ts(1)),
	}, map[string]float64{"btc": 100000})
	// cash 3000 + btc value 10000 = 13000 -> pnl 3000 -> 30% on 10000 start.
	if !near(s.TotalPnLPercent, 30) {
		t.Fatalf("pct: %v", s.TotalPnLPercent)
	}
}

func TestCompute_NegativeTotalPnL_ReportedAsNegativePercent(t *testing.T) {
	s := Compute(10000, []Trade{
		tBuy("btc", 0.1, 70000, ts(1)),
	}, map[string]float64{"btc": 50000})
	// cash 3000 + value 5000 = 8000 -> pnl -2000 -> -20%.
	if !near(s.TotalPnLPercent, -20) {
		t.Fatalf("pct: %v", s.TotalPnLPercent)
	}
}

func TestCompute_OnlyCashNoCoin_TotalEqualsCash(t *testing.T) {
	s := Compute(10000, []Trade{
		tBuy("btc", 0.1, 70000, ts(1)),
		tSell("btc", 0.1, 70000, ts(2)),
	}, map[string]float64{"btc": 999999}) // current price irrelevant — nothing held
	if !near(s.CashBalance, 10000) {
		t.Fatalf("cash: %v", s.CashBalance)
	}
	if !near(s.TotalValue, 10000) {
		t.Fatalf("total: %v", s.TotalValue)
	}
	if !near(s.RealizedPnL, 0) {
		t.Fatalf("realized: %v", s.RealizedPnL)
	}
}

func TestCompute_SellEntireAtLoss_NegativeRealized(t *testing.T) {
	s := Compute(10000, []Trade{
		tBuy("btc", 0.1, 70000, ts(1)),
		tSell("btc", 0.1, 50000, ts(2)),
	}, nil)
	if !near(s.RealizedPnL, -2000) {
		t.Fatalf("realized: %v", s.RealizedPnL)
	}
}

func TestCompute_PartialSell_HalfRealizedHalfRemaining(t *testing.T) {
	s := Compute(10000, []Trade{
		tBuy("btc", 0.1, 70000, ts(1)),
		tSell("btc", 0.05, 90000, ts(2)),
	}, map[string]float64{"btc": 100000})
	// realized = 0.05 * (90000 - 70000) = 1000
	if !near(s.RealizedPnL, 1000) {
		t.Fatalf("realized: %v", s.RealizedPnL)
	}
	h := holdingByCoin(s, "btc")
	if h == nil || !near(h.Amount, 0.05) {
		t.Fatalf("amount: %+v", h)
	}
	// unrealized = 0.05 * (100000 - 70000) = 1500
	if !near(s.UnrealizedPnL, 1500) {
		t.Fatalf("unrealized: %v", s.UnrealizedPnL)
	}
}

func TestCompute_BuyRebuildsAfterFullSell(t *testing.T) {
	// Sell everything, then buy again — avg cost should reflect ONLY the new
	// buys, not be polluted by the deleted prior state.
	s := Compute(10000, []Trade{
		tBuy("btc", 0.1, 70000, ts(1)),
		tSell("btc", 0.1, 80000, ts(2)),
		tBuy("btc", 0.05, 60000, ts(3)),
	}, nil)
	h := holdingByCoin(s, "btc")
	if h == nil {
		t.Fatal("missing")
	}
	if !near(h.AverageBuyPrice, 60000) {
		t.Fatalf("avg should reset to 60000, got %v", h.AverageBuyPrice)
	}
}

func TestCompute_DeterministicOutput_SameInputSameOutput(t *testing.T) {
	trades := []Trade{
		tBuy("btc", 0.1, 70000, ts(1)),
		tBuy("eth", 2, 2000, ts(2)),
		tSell("btc", 0.05, 80000, ts(3)),
	}
	prices := map[string]float64{"btc": 90000, "eth": 2500}
	a := Compute(10000, trades, prices)
	b := Compute(10000, trades, prices)
	if a.CashBalance != b.CashBalance || a.RealizedPnL != b.RealizedPnL ||
		a.UnrealizedPnL != b.UnrealizedPnL || a.TotalValue != b.TotalValue {
		t.Fatalf("non-deterministic: %+v vs %+v", a, b)
	}
	if len(a.Holdings) != len(b.Holdings) {
		t.Fatalf("holdings len: %d vs %d", len(a.Holdings), len(b.Holdings))
	}
	for i := range a.Holdings {
		if a.Holdings[i].CoinID != b.Holdings[i].CoinID {
			t.Fatalf("order differs at %d", i)
		}
	}
}
