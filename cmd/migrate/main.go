package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"smapass-back/db/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 || (os.Args[1] != "up" && os.Args[1] != "down-one" && os.Args[1] != "version") {
		return fmt.Errorf("usage: migrate up|down-one|version")
	}
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		return fmt.Errorf("DB_DSN is required")
	}
	source, err := iofs.New(migrations.Files, ".")
	if err != nil {
		return err
	}
	m, err := migrate.NewWithSourceInstance("iofs", source, dsn)
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("migration database initialization failed")
	}
	defer m.Close()
	m.LockTimeout = 30 * time.Second
	switch os.Args[1] {
	case "up":
		err = m.Up()
	case "down-one":
		err = m.Steps(-1)
	case "version":
		var version uint
		var dirty bool
		version, dirty, err = m.Version()
		if err == nil {
			fmt.Printf("version=%d dirty=%t\n", version, dirty)
		}
	}
	if errors.Is(err, migrate.ErrNoChange) {
		fmt.Println("no migration changes")
		return nil
	}
	if err != nil {
		return fmt.Errorf("migration failed; inspect schema version and SQL before retrying")
	}
	return nil
}
