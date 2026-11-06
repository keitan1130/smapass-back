package device

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
	"smapass-back/internal/infrastructure/database/dto"
)

func requireDeviceID(ctx context.Context, db bun.IDB, id string) (string, error) {
	var deviceID string
	err := db.NewRaw("SELECT id FROM devices WHERE id=?", id).Scan(ctx, &deviceID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", repository.ErrNotFound
	}
	return deviceID, err
}

// State remains readable without credentials, including during a sale when no
// registration is active. All observations are selected by the device UUID.
func (r *Repository) FindState(ctx context.Context, id string, limit, offset int) (*entity.DeviceState, error) {
	var state *entity.DeviceState
	err := r.db.RunInTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead}, func(ctx context.Context, tx bun.Tx) error {
		var err error
		state, err = readState(ctx, tx, id, limit, offset, true)
		return err
	})
	return state, err
}

func readState(ctx context.Context, tx bun.IDB, id string, limit, offset int, includeHistory bool) (*entity.DeviceState, error) {
	var state entity.DeviceState
	deviceID, err := requireDeviceID(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	state.ID = id
	if err := tx.NewRaw(`SELECT CASE WHEN EXISTS
			(SELECT 1 FROM device_ownerships WHERE device_id=? AND ended_at IS NULL)
			THEN 'active' ELSE 'released' END`, deviceID).Scan(ctx, &state.RegistrationStatus); err != nil {
		return nil, err
	}
	var recording struct {
		RecordCount      int
		FirstCollectedAt *time.Time
		LastCollectedAt  *time.Time
		IsDemo           bool
	}
	if err := tx.NewRaw(`SELECT count(*) AS record_count, min(collected_at) AS first_collected_at,
			max(collected_at) AS last_collected_at, COALESCE(bool_or(canonicalization = 'demo-json-sort-v1'), false) AS is_demo FROM device_logs WHERE device_id=?`, deviceID).Scan(ctx, &recording); err != nil {
		return nil, err
	}
	state.RecordCount = recording.RecordCount
	state.IsDemo = recording.IsDemo
	state.FirstCollectedAt = recording.FirstCollectedAt
	state.LastCollectedAt = recording.LastCollectedAt
	var spec entity.DeviceSpec
	err = tx.NewRaw(`SELECT s.manufacturer,s.model,s.device_name,s.soc,s.cpu_cores,s.ram_bytes,
			s.storage_bytes,s.screen_size_inch,s.screen_density_dpi,s.collected_at
			FROM device_specs s JOIN device_logs l ON l.id=s.log_id
			WHERE s.device_id=? ORDER BY l.collected_at DESC,l.seq DESC LIMIT 1`, deviceID).Scan(ctx, &spec)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		state.Spec = &spec
	}
	var status dto.StatusRecord
	err = statusQuery(tx, &status).Where("s.device_id=?", deviceID).OrderExpr("l.collected_at DESC,l.seq DESC").Limit(1).Scan(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		value := status.Entity()
		state.Status = &value
		if state.Spec != nil {
			state.Spec.OSVersion = &status.OSVersion
			state.Spec.SDKInt = &status.SDKInt
		}
	}
	if includeHistory {
		items, total, err := statusHistory(ctx, tx, deviceID, limit, offset)
		if err != nil {
			return nil, err
		}
		state.History = entity.DeviceHistory{Items: items, Total: total, Limit: limit, Offset: offset}
	}
	return &state, nil
}

func statusQuery(db bun.IDB, model any) *bun.SelectQuery {
	return db.NewSelect().Model(model).ModelTableExpr("device_status AS s").
		ColumnExpr(`l.seq AS sequence,s.collected_at,s.os_version,s.sdk_int,s.battery_temp_c,
			s.battery_cycle_count,s.battery_charge_mah,s.battery_full_charge_mah,
			s.boot_count,s.had_abnormal_exit,s.thermal_headroom`).
		Join("JOIN device_logs l ON l.id=s.log_id")
}

func statusHistory(ctx context.Context, db bun.IDB, deviceID string, limit, offset int) ([]entity.StatusRecord, int, error) {
	rows := make([]dto.StatusRecord, 0)
	var total int
	if err := db.NewRaw("SELECT count(*) FROM device_status WHERE device_id=?", deviceID).Scan(ctx, &total); err != nil {
		return nil, 0, err
	}
	if err := statusQuery(db, &rows).Where("s.device_id=?", deviceID).OrderExpr("l.seq ASC").Limit(int64(limit)).Offset(int64(offset)).Scan(ctx); err != nil {
		return nil, 0, err
	}
	items := make([]entity.StatusRecord, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.Entity())
	}
	return items, total, nil
}

// ListStates shares a database snapshot and omits history.
func (r *Repository) ListStates(ctx context.Context, ids []string) ([]entity.DeviceSummary, error) {
	items := make([]entity.DeviceSummary, 0)
	err := r.db.RunInTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead}, func(ctx context.Context, tx bun.Tx) error {
		var deviceIDs []string
		if err := tx.NewRaw("SELECT id FROM devices WHERE id IN (?) ORDER BY id", bun.In(ids)).Scan(ctx, &deviceIDs); err != nil {
			return err
		}
		for _, deviceID := range deviceIDs {
			state, err := readState(ctx, tx, deviceID, 0, 0, false)
			if err != nil {
				return err
			}
			items = append(items, entity.DeviceSummary{ID: state.ID, RegistrationStatus: state.RegistrationStatus, RecordCount: state.RecordCount, FirstCollectedAt: state.FirstCollectedAt, LastCollectedAt: state.LastCollectedAt, Spec: state.Spec, Status: state.Status, IsDemo: state.IsDemo})
		}
		return nil
	})
	return items, err
}

func (r *Repository) Search(ctx context.Context, imeis []string) ([]entity.DeviceReference, error) {
	items := make([]entity.DeviceReference, 0)
	err := r.db.NewRaw(`SELECT imei,id FROM devices WHERE imei IN (?) ORDER BY imei`, bun.In(imeis)).Scan(ctx, &items)
	return items, err
}
