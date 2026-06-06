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

func seedAlert(t *testing.T, h *storagetest.Harness, deviceID uuid.UUID) uuid.UUID {
	t.Helper()
	repo := storage.NewAlertRepo(h.Postgres.Pool)
	id := uuid.New()
	if err := repo.Upsert(context.Background(), deviceID, domain.PriceAlert{
		ID:         id.String(),
		Condition:  domain.PriceCrossing{CoinID: "btc", Direction: domain.Above, TargetPrice: 1},
		Recurrence: domain.OneShot{},
		IsActive:   true,
	}); err != nil {
		t.Fatalf("seed alert: %v", err)
	}
	return id
}

func TestFiringRepo_InsertAndList_RoundTrip(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000300")
	seedDevice(t, h, dev)
	alertID := seedAlert(t, h, dev)
	repo := storage.NewFiringRepo(h.Postgres.Pool)

	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	actual := 80123.45
	id, err := repo.Insert(context.Background(), storage.Firing{
		AlertID: alertID, DeviceID: dev, FiredAt: now, ActualValue: &actual,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}
	list, err := repo.ListByAlert(context.Background(), alertID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 firing, got %d", len(list))
	}
	got := list[0]
	if got.ID != id || got.AlertID != alertID || got.DeviceID != dev {
		t.Fatalf("identifier mismatch: %+v", got)
	}
	if got.ActualValue == nil || *got.ActualValue != 80123.45 {
		t.Fatalf("expected actual_value=80123.45, got %v", got.ActualValue)
	}
	if got.DeliveryStatus != storage.DeliveryPending {
		t.Fatalf("expected pending, got %s", got.DeliveryStatus)
	}
}

func TestFiringRepo_Insert_DefaultsStatusToPending(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000301")
	seedDevice(t, h, dev)
	alertID := seedAlert(t, h, dev)
	repo := storage.NewFiringRepo(h.Postgres.Pool)

	id, err := repo.Insert(context.Background(), storage.Firing{
		AlertID: alertID, DeviceID: dev, FiredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	list, _ := repo.ListByAlert(context.Background(), alertID)
	if len(list) != 1 || list[0].ID != id || list[0].DeliveryStatus != storage.DeliveryPending {
		t.Fatalf("expected pending firing, got %+v", list)
	}
}

func TestFiringRepo_Insert_RejectsInvalidStatus(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000302")
	seedDevice(t, h, dev)
	alertID := seedAlert(t, h, dev)
	repo := storage.NewFiringRepo(h.Postgres.Pool)

	_, err := repo.Insert(context.Background(), storage.Firing{
		AlertID: alertID, DeviceID: dev, FiredAt: time.Now().UTC(),
		DeliveryStatus: storage.DeliveryStatus("weird"),
	})
	if err == nil {
		t.Fatal("expected error for invalid delivery_status")
	}
}

func TestFiringRepo_UpdateStatus_RoundTrip(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000303")
	seedDevice(t, h, dev)
	alertID := seedAlert(t, h, dev)
	repo := storage.NewFiringRepo(h.Postgres.Pool)

	id, err := repo.Insert(context.Background(), storage.Firing{
		AlertID: alertID, DeviceID: dev, FiredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := repo.UpdateStatus(context.Background(), id, storage.DeliveryDelivered); err != nil {
		t.Fatalf("update: %v", err)
	}
	list, _ := repo.ListByAlert(context.Background(), alertID)
	if list[0].DeliveryStatus != storage.DeliveryDelivered {
		t.Fatalf("expected delivered, got %s", list[0].DeliveryStatus)
	}
}

func TestFiringRepo_UpdateStatus_MissingReturnsNotFound(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := storage.NewFiringRepo(h.Postgres.Pool)

	err := repo.UpdateStatus(context.Background(), 99999, storage.DeliveryDelivered)
	if !errors.Is(err, storage.ErrFiringNotFound) {
		t.Fatalf("expected ErrFiringNotFound, got %v", err)
	}
}

func TestFiringRepo_ListByAlert_NewestFirst(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000304")
	seedDevice(t, h, dev)
	alertID := seedAlert(t, h, dev)
	repo := storage.NewFiringRepo(h.Postgres.Pool)

	for _, ts := range []time.Time{
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
	} {
		if _, err := repo.Insert(context.Background(), storage.Firing{
			AlertID: alertID, DeviceID: dev, FiredAt: ts,
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	list, err := repo.ListByAlert(context.Background(), alertID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
	if !list[0].FiredAt.After(list[1].FiredAt) || !list[1].FiredAt.After(list[2].FiredAt) {
		t.Fatalf("expected descending fired_at order, got %v", []time.Time{
			list[0].FiredAt, list[1].FiredAt, list[2].FiredAt,
		})
	}
}

func TestFiringRepo_CascadeOnAlertDelete(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	dev := uuid.MustParse("00000000-0000-0000-0000-000000000305")
	seedDevice(t, h, dev)
	alertID := seedAlert(t, h, dev)
	firings := storage.NewFiringRepo(h.Postgres.Pool)
	alerts := storage.NewAlertRepo(h.Postgres.Pool)

	if _, err := firings.Insert(context.Background(), storage.Firing{
		AlertID: alertID, DeviceID: dev, FiredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := alerts.Delete(context.Background(), alertID); err != nil {
		t.Fatalf("delete alert: %v", err)
	}
	list, err := firings.ListByAlert(context.Background(), alertID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 firings after alert delete, got %d", len(list))
	}
}
