// Package storage owns the Postgres connection pool and migration runner.
package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres wraps a pgxpool.Pool with a thin lifecycle helper.
type Postgres struct {
	Pool *pgxpool.Pool
	dsn  string
}

// Open builds a connection pool against the given DSN. The DSN should be a
// libpq-compatible URL, e.g. "postgres://user:pass@host:5432/db?sslmode=disable".
func Open(ctx context.Context, dsn string) (*Postgres, error) {
	if dsn == "" {
		return nil, errors.New("storage: empty DSN")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: parse DSN: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("storage: build pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("storage: ping: %w", err)
	}
	return &Postgres{Pool: pool, dsn: dsn}, nil
}

// Close releases the pool. Safe to call multiple times.
func (p *Postgres) Close() {
	if p == nil || p.Pool == nil {
		return
	}
	p.Pool.Close()
}

// Migrate applies all pending migrations from the given file:// URL.
// `migrationsURL` example: "file:///abs/path/to/migrations".
func Migrate(dsn, migrationsURL string) error {
	if dsn == "" {
		return errors.New("storage: empty DSN")
	}
	if migrationsURL == "" {
		return errors.New("storage: empty migrations URL")
	}
	m, err := migrate.New(migrationsURL, dsn)
	if err != nil {
		return fmt.Errorf("storage: open migrate: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("storage: migrate up: %w", err)
	}
	return nil
}
