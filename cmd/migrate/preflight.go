package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Reject a historical schema before applying or rolling back the consolidated source.
func preflightBaseline(dsn string) error {
	database, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("cannot inspect database schema")
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return preflightBaselineDB(ctx, database)
}

func preflightBaselineDB(ctx context.Context, database *sql.DB) error {
	var legacy bool
	err := database.QueryRowContext(ctx, `SELECT (to_regclass('public.devices') IS NOT NULL AND NOT EXISTS
	 (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='devices' AND column_name='public_id'))
	 OR EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public'
	 AND table_name='key_registration_challenges' AND column_name='imeis')`).Scan(&legacy)
	if err != nil {
		return fmt.Errorf("cannot inspect database schema")
	}
	if legacy {
		return fmt.Errorf("historical schema detected: the consolidated baseline is for new databases only; do not reset an existing database; see db/README.md")
	}
	return nil
}
