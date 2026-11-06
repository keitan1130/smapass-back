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
	var legacy bool
	err = database.QueryRowContext(ctx, `SELECT to_regclass('public.certificates') IS NOT NULL
 OR EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='devices' AND column_name='id')
 OR EXISTS (SELECT 1 FROM schema_migrations WHERE version>1)`).Scan(&legacy)
	if err != nil {
		return fmt.Errorf("cannot inspect database schema")
	}
	if legacy {
		return fmt.Errorf("historical schema detected: the consolidated baseline is for new databases only; do not reset an existing database; see db/README.md")
	}
	return nil
}
