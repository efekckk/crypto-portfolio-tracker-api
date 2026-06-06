package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage/storagetest"
)

// seedDevice inserts a device so holdings can reference it via FK.
func seedDevice(t *testing.T, h *storagetest.Harness, id uuid.UUID) {
	t.Helper()
	repo := storage.NewDeviceRepo(h.Postgres.Pool)
	if err := repo.Upsert(context.Background(), storage.Device{
		DeviceID: id, APNsToken: "tok", APNsEnv: "development", Locale: "en",
	}); err != nil {
		t.Fatalf("seed device: %v", err)
	}
}

func newHoldingRepo(t *testing.T, h *storagetest.Harness) *storage.HoldingRepo {
	t.Helper()
	return storage.NewHoldingRepo(h.Postgres.Pool)
}

func TestHoldingRepo_UpsertAndGet_RoundTrip(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000100")
	seedDevice(t, h, dev)
	repo := newHoldingRepo(t, h)

	holding := domain.Holding{
		CoinID: "bitcoin", Amount: 1.5, AverageBuyPrice: 30000,
		DateAdded: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := repo.Upsert(context.Background(), dev, holding); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := repo.Get(context.Background(), dev, "bitcoin")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CoinID != "bitcoin" || got.Amount != 1.5 || got.AverageBuyPrice != 30000 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if !got.DateAdded.Equal(holding.DateAdded) {
		t.Fatalf("date mismatch: %v vs %v", got.DateAdded, holding.DateAdded)
	}
}

func TestHoldingRepo_Upsert_OverwritesAmountAndPrice(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000101")
	seedDevice(t, h, dev)
	repo := newHoldingRepo(t, h)

	first := domain.Holding{CoinID: "eth", Amount: 1, AverageBuyPrice: 2000,
		DateAdded: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	if err := repo.Upsert(context.Background(), dev, first); err != nil {
		t.Fatalf("first: %v", err)
	}
	second := domain.Holding{CoinID: "eth", Amount: 3, AverageBuyPrice: 2500,
		DateAdded: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}
	if err := repo.Upsert(context.Background(), dev, second); err != nil {
		t.Fatalf("second: %v", err)
	}
	got, err := repo.Get(context.Background(), dev, "eth")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Amount != 3 || got.AverageBuyPrice != 2500 {
		t.Fatalf("expected (3, 2500), got (%v, %v)", got.Amount, got.AverageBuyPrice)
	}
}

func TestHoldingRepo_ListByDevice_ReturnsAllOrdered(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000102")
	seedDevice(t, h, dev)
	repo := newHoldingRepo(t, h)

	for _, hh := range []domain.Holding{
		{CoinID: "solana", Amount: 5, AverageBuyPrice: 80, DateAdded: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{CoinID: "bitcoin", Amount: 1, AverageBuyPrice: 50000, DateAdded: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{CoinID: "ethereum", Amount: 2, AverageBuyPrice: 2000, DateAdded: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	} {
		if err := repo.Upsert(context.Background(), dev, hh); err != nil {
			t.Fatalf("upsert %s: %v", hh.CoinID, err)
		}
	}
	list, err := repo.ListByDevice(context.Background(), dev)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
	want := []string{"bitcoin", "ethereum", "solana"}
	for i, hh := range list {
		if hh.CoinID != want[i] {
			t.Fatalf("index %d: expected %s, got %s", i, want[i], hh.CoinID)
		}
	}
}

func TestHoldingRepo_Get_ReturnsErrNotFound(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000103")
	seedDevice(t, h, dev)
	repo := newHoldingRepo(t, h)

	_, err := repo.Get(context.Background(), dev, "doge")
	if !errors.Is(err, storage.ErrHoldingNotFound) {
		t.Fatalf("expected ErrHoldingNotFound, got %v", err)
	}
}

func TestHoldingRepo_Delete_IsIdempotent(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000104")
	seedDevice(t, h, dev)
	repo := newHoldingRepo(t, h)

	if err := repo.Delete(context.Background(), dev, "btc"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
	if err := repo.Upsert(context.Background(), dev, domain.Holding{
		CoinID: "btc", Amount: 1, AverageBuyPrice: 50000, DateAdded: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := repo.Delete(context.Background(), dev, "btc"); err != nil {
		t.Fatalf("delete existing: %v", err)
	}
	if err := repo.Delete(context.Background(), dev, "btc"); err != nil {
		t.Fatalf("delete again: %v", err)
	}
}

func TestHoldingRepo_Upsert_RejectsEmptyCoinID(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000105")
	seedDevice(t, h, dev)
	repo := newHoldingRepo(t, h)

	err := repo.Upsert(context.Background(), dev, domain.Holding{
		Amount: 1, AverageBuyPrice: 1, DateAdded: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected error for empty coin_id")
	}
}

func TestHoldingRepo_CascadeOnDeviceDelete(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000106")
	seedDevice(t, h, dev)
	holdings := newHoldingRepo(t, h)
	devices := storage.NewDeviceRepo(h.Postgres.Pool)

	if err := holdings.Upsert(context.Background(), dev, domain.Holding{
		CoinID: "btc", Amount: 1, AverageBuyPrice: 50000, DateAdded: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := devices.Delete(context.Background(), dev); err != nil {
		t.Fatalf("delete device: %v", err)
	}
	list, err := holdings.ListByDevice(context.Background(), dev)
	if err != nil {
		t.Fatalf("list after cascade: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 holdings after device delete (cascade), got %d", len(list))
	}
}
