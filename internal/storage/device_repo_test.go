package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage/storagetest"
)

func newDeviceRepo(t *testing.T, h *storagetest.Harness) *storage.DeviceRepo {
	t.Helper()
	return storage.NewDeviceRepo(h.Postgres.Pool)
}

func TestDeviceRepo_UpsertAndGet_RoundTrip(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := newDeviceRepo(t, h)

	d := storage.Device{
		DeviceID:  uuid.MustParse("00000000-0000-0000-0000-000000000010"),
		APNsToken: "abc123",
		APNsEnv:   "development",
		Locale:    "tr",
	}
	if err := repo.Upsert(context.Background(), d); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := repo.Get(context.Background(), d.DeviceID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DeviceID != d.DeviceID || got.APNsToken != d.APNsToken ||
		got.APNsEnv != d.APNsEnv || got.Locale != d.Locale {
		t.Fatalf("round trip mismatch: %+v vs %+v", got, d)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatal("timestamps must be populated")
	}
}

func TestDeviceRepo_Upsert_OverwritesTokenAndEnv(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := newDeviceRepo(t, h)

	id := uuid.MustParse("00000000-0000-0000-0000-000000000011")
	first := storage.Device{DeviceID: id, APNsToken: "old", APNsEnv: "development", Locale: "en"}
	if err := repo.Upsert(context.Background(), first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	second := storage.Device{DeviceID: id, APNsToken: "new", APNsEnv: "production", Locale: "tr"}
	if err := repo.Upsert(context.Background(), second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	got, err := repo.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.APNsToken != "new" || got.APNsEnv != "production" || got.Locale != "tr" {
		t.Fatalf("expected new state, got %+v", got)
	}
}

func TestDeviceRepo_Upsert_DefaultsLocaleToEn(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := newDeviceRepo(t, h)

	d := storage.Device{
		DeviceID:  uuid.MustParse("00000000-0000-0000-0000-000000000012"),
		APNsToken: "tok",
		APNsEnv:   "production",
		Locale:    "",
	}
	if err := repo.Upsert(context.Background(), d); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := repo.Get(context.Background(), d.DeviceID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Locale != "en" {
		t.Fatalf("expected locale=en, got %q", got.Locale)
	}
}

func TestDeviceRepo_Get_ReturnsErrNotFound(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := newDeviceRepo(t, h)

	_, err := repo.Get(context.Background(),
		uuid.MustParse("00000000-0000-0000-0000-000000000099"))
	if !errors.Is(err, storage.ErrDeviceNotFound) {
		t.Fatalf("expected ErrDeviceNotFound, got %v", err)
	}
}

func TestDeviceRepo_Delete_IsIdempotent(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := newDeviceRepo(t, h)

	id := uuid.MustParse("00000000-0000-0000-0000-000000000020")
	if err := repo.Delete(context.Background(), id); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
	if err := repo.Upsert(context.Background(),
		storage.Device{DeviceID: id, APNsToken: "tok", APNsEnv: "development", Locale: "en"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := repo.Delete(context.Background(), id); err != nil {
		t.Fatalf("delete existing: %v", err)
	}
	if err := repo.Delete(context.Background(), id); err != nil {
		t.Fatalf("delete again: %v", err)
	}
}

func TestDeviceRepo_Upsert_RejectsInvalidEnv(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := newDeviceRepo(t, h)

	err := repo.Upsert(context.Background(), storage.Device{
		DeviceID:  uuid.MustParse("00000000-0000-0000-0000-000000000030"),
		APNsToken: "tok", APNsEnv: "staging", Locale: "en",
	})
	if err == nil {
		t.Fatal("expected error for invalid apns_env")
	}
}

func TestDeviceRepo_Upsert_RejectsNilID(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()
	h.Fresh(t)
	repo := newDeviceRepo(t, h)

	err := repo.Upsert(context.Background(), storage.Device{
		APNsToken: "tok", APNsEnv: "development", Locale: "en",
	})
	if err == nil {
		t.Fatal("expected error for nil device id")
	}
}
