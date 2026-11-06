package device

import (
	"context"
	"database/sql"
	"errors"

	"github.com/uptrace/bun"
	"smapass-back/internal/domain/repository"
)

// lockDevice serializes registration changes and log writes for one IMEI.
func lockDevice(ctx context.Context, db bun.IDB, imei string) error {
	var stored string
	err := db.NewRaw("SELECT imei FROM devices WHERE imei=? FOR UPDATE", imei).Scan(ctx, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ErrNotFound
	}
	return err
}
