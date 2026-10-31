package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

func Open(ctx context.Context, dsn string) (*bun.DB, error) {
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	sqldb.SetMaxOpenConns(10)
	sqldb.SetMaxIdleConns(5)
	sqldb.SetConnMaxLifetime(30 * time.Minute)
	database := bun.NewDB(sqldb, pgdialect.New())
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		// Do not print DSN or driver errors which can contain credentials.
		return nil, fmt.Errorf("database connection failed")
	}
	return database, nil
}
