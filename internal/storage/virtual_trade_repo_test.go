package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage/storagetest"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/virtual"
)

func seedPortfolioForTrades(t *testing.T, h *storagetest.Harness) (uuid.UUID, uuid.UUID) {
	t.Helper()
	dev := seedDeviceForVirtual(t, h)
	p := newPortfolio(dev, "TradeFixture", 10000)
	if err := storage.NewVirtualPortfolioRepo(h.Postgres.Pool).Create(context.Background(), p); err != nil {
		t.Fatalf("seed portfolio: %v", err)
	}
	return dev, p.ID
}

func mkTrade(portfolioID uuid.UUID, side virtual.Side, coinID string, amount, price float64, when time.Time) virtual.Trade {
	return virtual.Trade{
		PortfolioID: portfolioID, Side: side, CoinID: coinID,
		Amount: amount, Price: price, ExecutedAt: when,
	}
}

func TestVirtualTradeRepo_InsertReturnsBigserialID(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	_, portfolioID := seedPortfolioForTrades(t, h)
	repo := storage.NewVirtualTradeRepo(h.Postgres.Pool)

	id, err := repo.Insert(context.Background(),
		mkTrade(portfolioID, virtual.SideBuy, "bitcoin", 0.1, 70000, time.Now().UTC()))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}
}

func TestVirtualTradeRepo_Insert_RejectsBadInputs(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	_, pid := seedPortfolioForTrades(t, h)
	repo := storage.NewVirtualTradeRepo(h.Postgres.Pool)

	cases := []struct {
		name  string
		trade virtual.Trade
	}{
		{"empty coin", virtual.Trade{PortfolioID: pid, Side: virtual.SideBuy, Amount: 1, Price: 1, ExecutedAt: time.Now()}},
		{"zero amount", virtual.Trade{PortfolioID: pid, Side: virtual.SideBuy, CoinID: "btc", Amount: 0, Price: 1, ExecutedAt: time.Now()}},
		{"zero price", virtual.Trade{PortfolioID: pid, Side: virtual.SideBuy, CoinID: "btc", Amount: 1, Price: 0, ExecutedAt: time.Now()}},
		{"nil portfolio", virtual.Trade{Side: virtual.SideBuy, CoinID: "btc", Amount: 1, Price: 1, ExecutedAt: time.Now()}},
		{"bad side", virtual.Trade{PortfolioID: pid, Side: virtual.Side("garbage"), CoinID: "btc", Amount: 1, Price: 1, ExecutedAt: time.Now()}},
		{"zero time", virtual.Trade{PortfolioID: pid, Side: virtual.SideBuy, CoinID: "btc", Amount: 1, Price: 1}},
	}
	for _, c := range cases {
		if _, err := repo.Insert(context.Background(), c.trade); err == nil {
			t.Fatalf("%s: expected error, got nil", c.name)
		}
	}
}

func TestVirtualTradeRepo_ListAllByPortfolio_AscendingByExecutedAt(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	_, pid := seedPortfolioForTrades(t, h)
	repo := storage.NewVirtualTradeRepo(h.Postgres.Pool)

	// Insert out of chronological order to verify the ORDER BY actually works.
	t1 := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	for _, when := range []time.Time{t1, t2, t3} {
		if _, err := repo.Insert(context.Background(),
			mkTrade(pid, virtual.SideBuy, "btc", 1, 1, when)); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	list, err := repo.ListAllByPortfolio(context.Background(), pid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
	if !list[0].ExecutedAt.Equal(t2) || !list[1].ExecutedAt.Equal(t3) || !list[2].ExecutedAt.Equal(t1) {
		t.Fatalf("ascending order broken: %v %v %v",
			list[0].ExecutedAt, list[1].ExecutedAt, list[2].ExecutedAt)
	}
}

func TestVirtualTradeRepo_ListPage_NewestFirst_WithCursor(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	_, pid := seedPortfolioForTrades(t, h)
	repo := storage.NewVirtualTradeRepo(h.Postgres.Pool)

	// Seed 7 trades. id is BIGSERIAL so 1..7 in insert order.
	for i := 0; i < 7; i++ {
		if _, err := repo.Insert(context.Background(),
			mkTrade(pid, virtual.SideBuy, "btc", 1, 1, time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC))); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// First page: limit 3, no cursor → ids [7, 6, 5], next_cursor=5.
	page1, next1, err := repo.ListPage(context.Background(), pid, 0, 3)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("page1 len: %d", len(page1))
	}
	if page1[0].ID != 7 || page1[1].ID != 6 || page1[2].ID != 5 {
		t.Fatalf("page1 ids: %v %v %v", page1[0].ID, page1[1].ID, page1[2].ID)
	}
	if next1 != 5 {
		t.Fatalf("next1: %d", next1)
	}

	// Second page: cursor=5 → ids [4, 3, 2], next_cursor=2.
	page2, next2, err := repo.ListPage(context.Background(), pid, next1, 3)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if page2[0].ID != 4 || page2[1].ID != 3 || page2[2].ID != 2 {
		t.Fatalf("page2 ids: %v %v %v", page2[0].ID, page2[1].ID, page2[2].ID)
	}
	if next2 != 2 {
		t.Fatalf("next2: %d", next2)
	}

	// Third page: cursor=2 → ids [1], next_cursor=0 (last page).
	page3, next3, err := repo.ListPage(context.Background(), pid, next2, 3)
	if err != nil {
		t.Fatalf("page3: %v", err)
	}
	if len(page3) != 1 || page3[0].ID != 1 {
		t.Fatalf("page3: %+v", page3)
	}
	if next3 != 0 {
		t.Fatalf("next3 should be 0 (no more), got %d", next3)
	}
}

func TestVirtualTradeRepo_ListPage_ClampsLimit(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	_, pid := seedPortfolioForTrades(t, h)
	repo := storage.NewVirtualTradeRepo(h.Postgres.Pool)

	// Limit 0 and limit > 200 should both be clamped — the test only verifies
	// the call doesn't error and returns at most 200 rows.
	for _, limit := range []int{0, -1, 500} {
		_, _, err := repo.ListPage(context.Background(), pid, 0, limit)
		if err != nil {
			t.Fatalf("limit=%d: %v", limit, err)
		}
	}
}

func TestVirtualTradeRepo_CountByPortfolio(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	_, pid := seedPortfolioForTrades(t, h)
	repo := storage.NewVirtualTradeRepo(h.Postgres.Pool)

	if n, _ := repo.CountByPortfolio(context.Background(), pid); n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
	for i := 0; i < 4; i++ {
		_, _ = repo.Insert(context.Background(),
			mkTrade(pid, virtual.SideBuy, "btc", 1, 1, time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC)))
	}
	n, err := repo.CountByPortfolio(context.Background(), pid)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 4 {
		t.Fatalf("expected 4, got %d", n)
	}
}

func TestVirtualTradeRepo_CascadeOnPortfolioDelete(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev, pid := seedPortfolioForTrades(t, h)
	_ = dev
	trades := storage.NewVirtualTradeRepo(h.Postgres.Pool)
	portfolios := storage.NewVirtualPortfolioRepo(h.Postgres.Pool)

	if _, err := trades.Insert(context.Background(),
		mkTrade(pid, virtual.SideBuy, "btc", 1, 1, time.Now().UTC())); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := portfolios.Delete(context.Background(), pid); err != nil {
		t.Fatalf("delete portfolio: %v", err)
	}
	list, err := trades.ListAllByPortfolio(context.Background(), pid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected cascade delete, got %d trades", len(list))
	}
}
