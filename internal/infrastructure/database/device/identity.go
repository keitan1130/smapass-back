package device

import (
	"context"
	"database/sql"
	"errors"

	"github.com/uptrace/bun"
	"smapass-back/internal/domain/repository"
)

// lockDevice serializes registration changes and log writes for one device UUID.
func lockDevice(ctx context.Context, db bun.IDB, deviceID string) error {
	var stored string
	err := db.NewRaw("SELECT id FROM devices WHERE id=? FOR UPDATE", deviceID).Scan(ctx, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ErrNotFound
	}
	return err
}
