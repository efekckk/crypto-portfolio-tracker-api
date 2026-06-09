// Package storagetest spins up an ephemeral Postgres container for tests.
package storagetest

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
)

// Harness owns a single Postgres container shared across tests in one package.
// Tests get a clean schema via `Fresh(t)` which truncates user tables.
type Harness struct {
	Postgres *storage.Postgres
	DSN      string
	pool     *dockertest.Pool
	resource *dockertest.Resource
}

// Start launches a Postgres container, applies migrations from the repo's
// `migrations/` directory, and returns a ready-to-use Harness. Caller MUST
// defer Stop(). Skips the test if Docker is not available.
func Start(t *testing.T) *Harness {
	t.Helper()

	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Skipf("docker not available: %v", err)
	}
	if err := pool.Client.Ping(); err != nil {
		t.Skipf("docker ping failed: %v", err)
	}

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "16-alpine",
		Env: []string{
			"POSTGRES_USER=test",
			"POSTGRES_PASSWORD=test",
			"POSTGRES_DB=test",
			"listen_addresses=*",
		},
	}, func(hostConfig *docker.HostConfig) {
		hostConfig.AutoRemove = true
		hostConfig.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}

	dsn := fmt.Sprintf("postgres://test:test@%s/test?sslmode=disable",
		resource.GetHostPort("5432/tcp"))

	pool.MaxWait = 60 * time.Second
	var pg *storage.Postgres
	if err := pool.Retry(func() error {
		var openErr error
		pg, openErr = storage.Open(context.Background(), dsn)
		return openErr
	}); err != nil {
		_ = pool.Purge(resource)
		t.Fatalf("postgres not ready: %v", err)
	}

	migrationsURL := "file://" + repoMigrationsAbsPath(t)
	if err := storage.Migrate(dsn, migrationsURL); err != nil {
		pg.Close()
		_ = pool.Purge(resource)
		t.Fatalf("migrate: %v", err)
	}

	return &Harness{Postgres: pg, DSN: dsn, pool: pool, resource: resource}
}

// Stop releases the container and pool.
func (h *Harness) Stop() {
	if h == nil {
		return
	}
	if h.Postgres != nil {
		h.Postgres.Close()
	}
	if h.pool != nil && h.resource != nil {
		_ = h.pool.Purge(h.resource)
	}
}

// Fresh truncates every user table so the next test sees an empty DB.
func (h *Harness) Fresh(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := h.Postgres.Pool.Exec(ctx,
		`TRUNCATE devices, alerts, holdings, firings, virtual_portfolios, virtual_trades RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// repoMigrationsAbsPath finds the repo's migrations/ directory using runtime
// caller info so tests can be run from any sub-package working directory.
func repoMigrationsAbsPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file = .../internal/storage/storagetest/harness.go
	// migrations live three dirs up + "/migrations".
	dir := filepath.Dir(file)
	for i := 0; i < 3; i++ {
		dir = filepath.Dir(dir)
	}
	abs, err := filepath.Abs(filepath.Join(dir, "migrations"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	return abs
}

// SanityCheckPostgres ensures the wrapper used by the harness is not nil.
// Lets callers of Start() use `require`-style flow without an extra import.
func (h *Harness) SanityCheckPostgres() error {
	if h == nil || h.Postgres == nil {
		return errors.New("harness: nil postgres")
	}
	return nil
}
