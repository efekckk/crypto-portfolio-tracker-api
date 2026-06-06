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

func newAlertRepo(t *testing.T, h *storagetest.Harness) *storage.AlertRepo {
	t.Helper()
	return storage.NewAlertRepo(h.Postgres.Pool)
}

func TestAlertRepo_RoundTrip_PriceCrossingOneShot(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000200")
	seedDevice(t, h, dev)
	repo := newAlertRepo(t, h)

	id := uuid.New()
	original := domain.PriceAlert{
		ID:                  id.String(),
		Condition:           domain.PriceCrossing{CoinID: "bitcoin", Direction: domain.Above, TargetPrice: 75000},
		Recurrence:          domain.OneShot{},
		IsActive:            true,
		FiredAt:             nil,
		LastConditionResult: nil,
	}
	if err := repo.Upsert(context.Background(), dev, original); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := repo.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != original.ID || got.IsActive != original.IsActive {
		t.Fatalf("metadata mismatch: %+v", got)
	}
	pc, ok := got.Condition.(domain.PriceCrossing)
	if !ok {
		t.Fatalf("expected PriceCrossing, got %T", got.Condition)
	}
	if pc.CoinID != "bitcoin" || pc.Direction != domain.Above || pc.TargetPrice != 75000 {
		t.Fatalf("condition mismatch: %+v", pc)
	}
	if _, ok := got.Recurrence.(domain.OneShot); !ok {
		t.Fatalf("expected OneShot, got %T", got.Recurrence)
	}
}

func TestAlertRepo_RoundTrip_PercentChangeCooldown(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000201")
	seedDevice(t, h, dev)
	repo := newAlertRepo(t, h)

	id := uuid.New()
	cond := domain.PercentChange{CoinID: "ethereum", Direction: domain.Below, Window: domain.Window7d, Threshold: -5}
	rec := domain.Cooldown{Seconds: 3600}
	if err := repo.Upsert(context.Background(), dev, domain.PriceAlert{
		ID: id.String(), Condition: cond, Recurrence: rec, IsActive: true,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := repo.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if pc, ok := got.Condition.(domain.PercentChange); !ok || pc != cond {
		t.Fatalf("condition mismatch: %+v ok=%v", got.Condition, ok)
	}
	if cd, ok := got.Recurrence.(domain.Cooldown); !ok || cd != rec {
		t.Fatalf("recurrence mismatch: %+v ok=%v", got.Recurrence, ok)
	}
}

func TestAlertRepo_RoundTrip_PortfolioValueOnCrossing(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000202")
	seedDevice(t, h, dev)
	repo := newAlertRepo(t, h)

	id := uuid.New()
	lcr := true
	if err := repo.Upsert(context.Background(), dev, domain.PriceAlert{
		ID:                  id.String(),
		Condition:           domain.PortfolioValue{Direction: domain.Above, Threshold: 100000},
		Recurrence:          domain.OnCrossing{},
		IsActive:            true,
		LastConditionResult: &lcr,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := repo.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if pv, ok := got.Condition.(domain.PortfolioValue); !ok || pv.Threshold != 100000 || pv.Direction != domain.Above {
		t.Fatalf("condition mismatch: %+v", got.Condition)
	}
	if got.LastConditionResult == nil || !*got.LastConditionResult {
		t.Fatalf("expected lastConditionResult=true, got %v", got.LastConditionResult)
	}
}

func TestAlertRepo_RoundTrip_PortfolioPnLPercentOneShot(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000203")
	seedDevice(t, h, dev)
	repo := newAlertRepo(t, h)

	id := uuid.New()
	if err := repo.Upsert(context.Background(), dev, domain.PriceAlert{
		ID:         id.String(),
		Condition:  domain.PortfolioPnLPercent{Direction: domain.Below, Threshold: -10},
		Recurrence: domain.OneShot{},
		IsActive:   true,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := repo.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if pn, ok := got.Condition.(domain.PortfolioPnLPercent); !ok || pn.Threshold != -10 {
		t.Fatalf("condition mismatch: %+v", got.Condition)
	}
}

func TestAlertRepo_UpdateState_FiredAndDeactivates(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000204")
	seedDevice(t, h, dev)
	repo := newAlertRepo(t, h)

	id := uuid.New()
	if err := repo.Upsert(context.Background(), dev, domain.PriceAlert{
		ID: id.String(), Condition: domain.PriceCrossing{CoinID: "btc", Direction: domain.Above, TargetPrice: 1},
		Recurrence: domain.OneShot{}, IsActive: true,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	fired := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	if err := repo.UpdateState(context.Background(), id, false, &fired, nil); err != nil {
		t.Fatalf("update state: %v", err)
	}
	got, err := repo.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.IsActive {
		t.Fatal("expected isActive=false after update")
	}
	if got.FiredAt == nil || !got.FiredAt.Equal(fired) {
		t.Fatalf("expected firedAt=%v, got %v", fired, got.FiredAt)
	}
}

func TestAlertRepo_UpdateState_MissingReturnsNotFound(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := newAlertRepo(t, h)

	err := repo.UpdateState(context.Background(), uuid.New(), false, nil, nil)
	if !errors.Is(err, storage.ErrAlertNotFound) {
		t.Fatalf("expected ErrAlertNotFound, got %v", err)
	}
}

func TestAlertRepo_ListActive_FiltersInactive(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000205")
	seedDevice(t, h, dev)
	repo := newAlertRepo(t, h)

	activeID := uuid.New()
	inactiveID := uuid.New()
	for _, a := range []domain.PriceAlert{
		{ID: activeID.String(), Condition: domain.PriceCrossing{CoinID: "btc", Direction: domain.Above, TargetPrice: 1}, Recurrence: domain.OneShot{}, IsActive: true},
		{ID: inactiveID.String(), Condition: domain.PriceCrossing{CoinID: "eth", Direction: domain.Above, TargetPrice: 1}, Recurrence: domain.OneShot{}, IsActive: false},
	} {
		if err := repo.Upsert(context.Background(), dev, a); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	list, err := repo.ListActive(context.Background())
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 active, got %d", len(list))
	}
	if list[0].Alert.ID != activeID.String() {
		t.Fatalf("expected %s, got %s", activeID, list[0].Alert.ID)
	}
	if list[0].DeviceID != dev {
		t.Fatalf("expected device %s, got %s", dev, list[0].DeviceID)
	}
}

func TestAlertRepo_ListByDevice_ReturnsAllForDevice(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	d1 := uuid.MustParse("00000000-0000-0000-0000-000000000206")
	d2 := uuid.MustParse("00000000-0000-0000-0000-000000000207")
	seedDevice(t, h, d1)
	seedDevice(t, h, d2)
	repo := newAlertRepo(t, h)

	for i := 0; i < 3; i++ {
		if err := repo.Upsert(context.Background(), d1, domain.PriceAlert{
			ID:         uuid.New().String(),
			Condition:  domain.PriceCrossing{CoinID: "btc", Direction: domain.Above, TargetPrice: float64(i + 1)},
			Recurrence: domain.OneShot{}, IsActive: true,
		}); err != nil {
			t.Fatalf("upsert d1: %v", err)
		}
	}
	if err := repo.Upsert(context.Background(), d2, domain.PriceAlert{
		ID:         uuid.New().String(),
		Condition:  domain.PriceCrossing{CoinID: "eth", Direction: domain.Above, TargetPrice: 1},
		Recurrence: domain.OneShot{}, IsActive: true,
	}); err != nil {
		t.Fatalf("upsert d2: %v", err)
	}
	list, err := repo.ListByDevice(context.Background(), d1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 for d1, got %d", len(list))
	}
}

func TestAlertRepo_Get_ReturnsErrNotFound(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := newAlertRepo(t, h)

	_, err := repo.Get(context.Background(), uuid.New())
	if !errors.Is(err, storage.ErrAlertNotFound) {
		t.Fatalf("expected ErrAlertNotFound, got %v", err)
	}
}

func TestAlertRepo_Upsert_RejectsInvalidUUID(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000208")
	seedDevice(t, h, dev)
	repo := newAlertRepo(t, h)

	err := repo.Upsert(context.Background(), dev, domain.PriceAlert{
		ID:         "not-a-uuid",
		Condition:  domain.PriceCrossing{CoinID: "btc", Direction: domain.Above, TargetPrice: 1},
		Recurrence: domain.OneShot{}, IsActive: true,
	})
	if err == nil {
		t.Fatal("expected error for invalid alert id")
	}
}

func TestAlertRepo_CascadeOnDeviceDelete(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000209")
	seedDevice(t, h, dev)
	alerts := newAlertRepo(t, h)
	devices := storage.NewDeviceRepo(h.Postgres.Pool)

	if err := alerts.Upsert(context.Background(), dev, domain.PriceAlert{
		ID:         uuid.New().String(),
		Condition:  domain.PriceCrossing{CoinID: "btc", Direction: domain.Above, TargetPrice: 1},
		Recurrence: domain.OneShot{}, IsActive: true,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := devices.Delete(context.Background(), dev); err != nil {
		t.Fatalf("delete device: %v", err)
	}
	list, err := alerts.ListByDevice(context.Background(), dev)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 alerts after device delete, got %d", len(list))
	}
}
