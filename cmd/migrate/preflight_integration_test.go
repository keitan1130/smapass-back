//go:build integration

package main

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"smapass-back/db/migrations"
	"smapass-back/internal/infrastructure/database/testutil"
)

func TestPreflightAllowsLaterMigrationsAndRejectsIncompatibleSchema(t *testing.T) {
	db := testutil.NewDatabaseAtVersion(t, 1)
	ctx := context.Background()
	if err := preflightBaselineDB(ctx, db.DB); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := db.NewRaw("INSERT INTO devices (imei) VALUES ('490154203237518') RETURNING public_id").Scan(ctx, &id); err != nil {
		t.Fatal(err)
	}
	// Version 2 exists only in this test; production migrations are unchanged.
	up, err := migrations.Files.ReadFile("000001_initial_schema.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := migrations.Files.ReadFile("000001_initial_schema.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	source, err := iofs.New(fstest.MapFS{
		"000001_initial_schema.up.sql":   &fstest.MapFile{Data: up},
		"000001_initial_schema.down.sql": &fstest.MapFile{Data: down},
		"000002_fixture.up.sql":          &fstest.MapFile{Data: []byte("CREATE TABLE migration_fixture (id integer PRIMARY KEY);")},
		"000002_fixture.down.sql":        &fstest.MapFile{Data: []byte("DROP TABLE migration_fixture;")},
	}, ".")
	if err != nil {
		t.Fatal(err)
	}
	driver, err := postgres.WithInstance(db.DB, &postgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	if version, dirty, err := m.Version(); err != nil || version != 2 || dirty {
		t.Fatalf("version=%d dirty=%t err=%v", version, dirty, err)
	}
	if err = preflightBaselineDB(ctx, db.DB); err != nil {
		t.Fatalf("valid version 2 rejected: %v", err)
	}
	if err = m.Up(); !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("restart failed: %v", err)
	}
	if err = m.Steps(-1); err != nil {
		t.Fatal(err)
	}
	if err = preflightBaselineDB(ctx, db.DB); err != nil {
		t.Fatalf("rollback to version 1 rejected: %v", err)
	}
	var retained string
	if err = db.NewRaw("SELECT public_id FROM devices WHERE imei='490154203237518'").Scan(ctx, &retained); err != nil || retained != id {
		t.Fatalf("migration changed device: %v", err)
	}
	if _, err = db.ExecContext(ctx, "ALTER TABLE devices DROP COLUMN public_id"); err != nil {
		t.Fatal(err)
	}
	if err = preflightBaselineDB(ctx, db.DB); err == nil {
		t.Fatal("incompatible schema accepted")
	}
}
