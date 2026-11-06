package device

import (
	"context"
	"database/sql"
	"errors"

	"github.com/uptrace/bun"
	"smapass-back/internal/domain/repository"
)

// Writes resolve primary and secondary IMEIs; public reads use the device UUID.
func primaryIMEIByAlias(ctx context.Context, db bun.IDB, imei string) (string, error) {
	var primary string
	err := db.NewRaw("SELECT primary_imei FROM device_imei_aliases WHERE imei=?", imei).Scan(ctx, &primary)
	if errors.Is(err, sql.ErrNoRows) {
		return "", repository.ErrNotFound
	}
	return primary, err
}
