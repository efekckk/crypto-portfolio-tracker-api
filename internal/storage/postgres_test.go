package storage_test

import (
	"context"
	"testing"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage/storagetest"
)

func TestPostgres_OpenAndPing(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()

	if err := h.Postgres.Pool.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestMigrate_CreatesAllExpectedTables(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()

	rows, err := h.Postgres.Pool.Query(context.Background(),
		`SELECT table_name FROM information_schema.tables
		 WHERE table_schema='public' AND table_name IN
		 ('devices','alerts','holdings','firings')
		 ORDER BY table_name`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, name)
	}
	want := []string{"alerts", "devices", "firings", "holdings"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i, name := range want {
		if got[i] != name {
			t.Fatalf("expected %s at index %d, got %s", name, i, got[i])
		}
	}
}

func TestFresh_TruncatesAllTables(t *testing.T) {
	h := storagetest.Start(t)
	defer h.Stop()

	ctx := context.Background()
	_, err := h.Postgres.Pool.Exec(ctx,
		`INSERT INTO devices (device_id, apns_token, apns_env, locale)
		 VALUES ('00000000-0000-0000-0000-000000000001', 'tok', 'development', 'en')`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	h.Fresh(t)

	var count int
	err = h.Postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices`).Scan(&count)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows after Fresh, got %d", count)
	}
}

func TestOpen_RejectsEmptyDSN(t *testing.T) {
	_, err := storage.Open(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty DSN, got nil")
	}
}
