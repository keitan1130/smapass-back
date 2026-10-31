//go:build integration

package testutil

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/uptrace/bun"
	"smapass-back/db/migrations"
	"smapass-back/db/seeds"
	"smapass-back/pkg/db"
)

// NewDatabase creates an isolated PostgreSQL per test. No external DSN is used.
func NewDatabase(t testing.TB) *bun.DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18.6-alpine3.24",
		postgres.WithDatabase("smapass_test"), postgres.WithUsername("smapass_test"),
		postgres.WithPassword("smapass_test"), postgres.BasicWaitStrategies())
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start test database: %v", err)
	}
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("test database address: %v", err)
	}
	source, err := iofs.New(migrations.Files, ".")
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", source, dsn)
	if err != nil {
		_ = source.Close()
		t.Fatal("initialize test migrations")
	}
	if err = m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		_, _ = m.Close()
		t.Fatalf("migrate test database: %v", err)
	}
	if sourceErr, databaseErr := m.Close(); sourceErr != nil || databaseErr != nil {
		t.Fatalf("close migrations: %v / %v", sourceErr, databaseErr)
	}
	database, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err = database.ExecContext(ctx, seeds.Development); err != nil {
		t.Fatalf("seed test database: %v", err)
	}
	return database
}
