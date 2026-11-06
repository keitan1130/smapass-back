package device

import (
	"context"
	"time"

	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/infrastructure/database/authorization"
)

func (r *Repository) SetVisibility(ctx context.Context, principal entity.Principal, deviceID, visibility string) (*entity.DeviceVisibility, error) {
	var result *entity.DeviceVisibility
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := lockDevice(ctx, tx, deviceID); err != nil {
			return err
		}
		if err := authorization.RequireDevice(ctx, tx, principal, deviceID); err != nil {
			return err
		}
		var registration struct {
			StartedAt         time.Time
			LastVerifiedLogAt *time.Time
		}
		if err := tx.NewRaw("SELECT started_at,last_verified_log_at FROM device_ownerships WHERE device_id=? AND ended_at IS NULL", deviceID).Scan(ctx, &registration); err != nil {
			return err
		}
		var now time.Time
		if err := tx.NewRaw("SELECT clock_timestamp()").Scan(ctx, &now); err != nil {
			return err
		}
		if visibility == "private" && !now.Before(entity.DeviceReclaimableAt(registration.StartedAt, registration.LastVerifiedLogAt)) {
			return entity.ErrVisibilityLocked
		}
		if _, err := tx.ExecContext(ctx, "UPDATE devices SET visibility=? WHERE id=?", visibility, deviceID); err != nil {
			return err
		}
		result = &entity.DeviceVisibility{DeviceID: deviceID, Visibility: visibility}
		return nil
	})
	return result, err
}
