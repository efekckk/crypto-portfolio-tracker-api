package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage/storagetest"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/virtual"
)

func seedDeviceForVirtual(t *testing.T, h *storagetest.Harness) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := storage.NewDeviceRepo(h.Postgres.Pool).Upsert(context.Background(),
		storage.Device{DeviceID: id, APNsToken: "tok", APNsEnv: "development", Locale: "en"}); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	return id
}

func newPortfolio(deviceID uuid.UUID, name string, balance float64) virtual.Portfolio {
	return virtual.Portfolio{
		ID:              uuid.New(),
		DeviceID:        deviceID,
		Name:            name,
		StartingBalance: balance,
	}
}

func TestVirtualPortfolioRepo_CreateAndGet_RoundTrip(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewVirtualPortfolioRepo(h.Postgres.Pool)
	dev := seedDeviceForVirtual(t, h)

	p := newPortfolio(dev, "Aggressive", 10000)
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != p.ID || got.DeviceID != dev || got.Name != "Aggressive" || got.StartingBalance != 10000 {
		t.Fatalf("metadata mismatch: %+v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatal("timestamps must be populated")
	}
}

func TestVirtualPortfolioRepo_Create_RejectsDuplicateName(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewVirtualPortfolioRepo(h.Postgres.Pool)
	dev := seedDeviceForVirtual(t, h)

	if err := repo.Create(context.Background(), newPortfolio(dev, "Same", 10000)); err != nil {
		t.Fatalf("first create: %v", err)
	}
	err := repo.Create(context.Background(), newPortfolio(dev, "Same", 10000))
	if !errors.Is(err, storage.ErrVirtualPortfolioNameTaken) {
		t.Fatalf("expected ErrVirtualPortfolioNameTaken, got %v", err)
	}
}

func TestVirtualPortfolioRepo_Create_SameNameAcrossDevicesIsOK(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewVirtualPortfolioRepo(h.Postgres.Pool)
	d1 := seedDeviceForVirtual(t, h)
	d2 := seedDeviceForVirtual(t, h)

	if err := repo.Create(context.Background(), newPortfolio(d1, "Aggressive", 10000)); err != nil {
		t.Fatalf("d1: %v", err)
	}
	if err := repo.Create(context.Background(), newPortfolio(d2, "Aggressive", 10000)); err != nil {
		t.Fatalf("d2 should be allowed: %v", err)
	}
}

func TestVirtualPortfolioRepo_Create_RejectsNonPositiveBalance(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewVirtualPortfolioRepo(h.Postgres.Pool)
	dev := seedDeviceForVirtual(t, h)

	if err := repo.Create(context.Background(), newPortfolio(dev, "Bad", 0)); err == nil {
		t.Fatal("expected error for zero balance")
	}
	if err := repo.Create(context.Background(), newPortfolio(dev, "Bad", -1)); err == nil {
		t.Fatal("expected error for negative balance")
	}
}

func TestVirtualPortfolioRepo_Get_ReturnsErrNotFound(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewVirtualPortfolioRepo(h.Postgres.Pool)

	_, err := repo.Get(context.Background(), uuid.New())
	if !errors.Is(err, storage.ErrVirtualPortfolioNotFound) {
		t.Fatalf("expected ErrVirtualPortfolioNotFound, got %v", err)
	}
}

func TestVirtualPortfolioRepo_ListByDevice_OrdersByCreatedAtASC(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewVirtualPortfolioRepo(h.Postgres.Pool)
	dev := seedDeviceForVirtual(t, h)

	first := newPortfolio(dev, "Alpha", 10000)
	if err := repo.Create(context.Background(), first); err != nil {
		t.Fatalf("alpha: %v", err)
	}
	time.Sleep(2 * time.Millisecond) // ensure created_at differs
	second := newPortfolio(dev, "Beta", 5000)
	if err := repo.Create(context.Background(), second); err != nil {
		t.Fatalf("beta: %v", err)
	}

	list, err := repo.ListByDevice(context.Background(), dev)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
	if list[0].Name != "Alpha" || list[1].Name != "Beta" {
		t.Fatalf("expected [Alpha, Beta], got [%s, %s]", list[0].Name, list[1].Name)
	}
}

func TestVirtualPortfolioRepo_ListByDevice_IsolatesPerDevice(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewVirtualPortfolioRepo(h.Postgres.Pool)
	d1 := seedDeviceForVirtual(t, h)
	d2 := seedDeviceForVirtual(t, h)

	_ = repo.Create(context.Background(), newPortfolio(d1, "A", 1000))
	_ = repo.Create(context.Background(), newPortfolio(d1, "B", 1000))
	_ = repo.Create(context.Background(), newPortfolio(d2, "X", 1000))

	listD1, _ := repo.ListByDevice(context.Background(), d1)
	if len(listD1) != 2 {
		t.Fatalf("d1 expected 2, got %d", len(listD1))
	}
	listD2, _ := repo.ListByDevice(context.Background(), d2)
	if len(listD2) != 1 || listD2[0].Name != "X" {
		t.Fatalf("d2 expected [X], got %+v", listD2)
	}
}

func TestVirtualPortfolioRepo_CountByDevice_ReportsExactCount(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewVirtualPortfolioRepo(h.Postgres.Pool)
	dev := seedDeviceForVirtual(t, h)

	if n, _ := repo.CountByDevice(context.Background(), dev); n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
	for i := 0; i < 3; i++ {
		_ = repo.Create(context.Background(), newPortfolio(dev, "P"+string(rune('A'+i)), 1000))
	}
	n, err := repo.CountByDevice(context.Background(), dev)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}
}

func TestVirtualPortfolioRepo_Delete_IsIdempotent(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewVirtualPortfolioRepo(h.Postgres.Pool)
	dev := seedDeviceForVirtual(t, h)

	p := newPortfolio(dev, "Goner", 10000)
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Delete(context.Background(), p.ID); err != nil {
		t.Fatalf("first delete: %v", err)
	}
	if err := repo.Delete(context.Background(), p.ID); err != nil {
		t.Fatalf("second delete should be idempotent: %v", err)
	}
	if _, err := repo.Get(context.Background(), p.ID); !errors.Is(err, storage.ErrVirtualPortfolioNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestVirtualPortfolioRepo_CascadeOnDeviceDelete(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewVirtualPortfolioRepo(h.Postgres.Pool)
	devices := storage.NewDeviceRepo(h.Postgres.Pool)
	dev := seedDeviceForVirtual(t, h)

	if err := repo.Create(context.Background(), newPortfolio(dev, "Doomed", 10000)); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := devices.Delete(context.Background(), dev); err != nil {
		t.Fatalf("delete device: %v", err)
	}
	list, err := repo.ListByDevice(context.Background(), dev)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected cascade delete, got %d", len(list))
	}
}

func TestVirtualPortfolioRepo_Touch_UpdatesUpdatedAt(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewVirtualPortfolioRepo(h.Postgres.Pool)
	dev := seedDeviceForVirtual(t, h)

	p := newPortfolio(dev, "Touchy", 10000)
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("create: %v", err)
	}
	before, _ := repo.Get(context.Background(), p.ID)

	time.Sleep(5 * time.Millisecond)
	later := time.Now().UTC()
	if err := repo.Touch(context.Background(), p.ID, later); err != nil {
		t.Fatalf("touch: %v", err)
	}
	after, _ := repo.Get(context.Background(), p.ID)
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("updated_at should advance: %v vs %v", before.UpdatedAt, after.UpdatedAt)
	}
}

func TestVirtualPortfolioRepo_Touch_MissingReturnsNotFound(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewVirtualPortfolioRepo(h.Postgres.Pool)

	err := repo.Touch(context.Background(), uuid.New(), time.Now())
	if !errors.Is(err, storage.ErrVirtualPortfolioNotFound) {
		t.Fatalf("expected ErrVirtualPortfolioNotFound, got %v", err)
	}
}
