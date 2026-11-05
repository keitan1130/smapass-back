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

func publicPrimary(ctx context.Context, db bun.IDB, imei string) (string, error) {
	var primary string
	err := db.NewRaw("SELECT primary_imei FROM device_imei_aliases WHERE imei=?", imei).Scan(ctx, &primary)
	if errors.Is(err, sql.ErrNoRows) {
		return "", repository.ErrNotFound
	}
	return primary, err
}

// State remains readable without credentials, including during a sale when no
// registration is active. All observations are selected by the physical IMEI.
func (r *Repository) FindState(ctx context.Context, imei string) (*entity.DeviceState, error) {
	var state entity.DeviceState
	err := r.db.RunInTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead}, func(ctx context.Context, tx bun.Tx) error {
		primary, err := publicPrimary(ctx, tx, imei)
		if err != nil {
			return err
		}
		state.IMEI = primary
		state.IMEIs = []string{}
		if err := tx.NewRaw("SELECT imei FROM device_imei_aliases WHERE primary_imei=? ORDER BY imei", primary).Scan(ctx, &state.IMEIs); err != nil {
			return err
		}
		if err := tx.NewRaw(`SELECT CASE WHEN EXISTS
			(SELECT 1 FROM device_ownerships WHERE imei=? AND ended_at IS NULL)
			THEN 'active' ELSE 'released' END`, primary).Scan(ctx, &state.RegistrationStatus); err != nil {
			return err
		}
		var recording struct {
			RecordCount      int
			FirstCollectedAt *time.Time
			LastCollectedAt  *time.Time
		}
		if err := tx.NewRaw(`SELECT count(*) AS record_count, min(collected_at) AS first_collected_at,
			max(collected_at) AS last_collected_at FROM device_logs WHERE imei=?`, primary).Scan(ctx, &recording); err != nil {
			return err
		}
		state.RecordCount = recording.RecordCount
		state.FirstCollectedAt = recording.FirstCollectedAt
		state.LastCollectedAt = recording.LastCollectedAt
		var spec entity.DeviceSpec
		err = tx.NewRaw(`SELECT s.manufacturer,s.model,s.device_name,s.soc,s.cpu_cores,s.ram_bytes,
			s.storage_bytes,s.screen_size_inch,s.screen_density_dpi,s.collected_at
			FROM device_specs s JOIN device_logs l ON l.id=s.log_id
			WHERE s.imei=? ORDER BY l.seq DESC LIMIT 1`, primary).Scan(ctx, &spec)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			state.Spec = &spec
		}
		var status entity.StatusRecord
		err = statusQuery(tx, &status).Where("s.imei=?", primary).OrderExpr("l.seq DESC").Limit(1).Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			state.Status = &status
			if state.Spec != nil {
				state.Spec.OSVersion = &status.OSVersion
				state.Spec.SDKInt = &status.SDKInt
			}
		}
		var certificate dto.Certificate
		err = tx.NewSelect().Model(&certificate).
			Column("c.id", "c.imei", "c.status", "c.issued_at", "c.is_demo", "c.snapshot").
			// A listing is registrant-provided, so do not carry an old seller's listing forward.
			ColumnExpr(`CASE WHEN o.ended_at IS NULL AND c.status='valid' AND l.certificate_id IS NOT NULL
				THEN jsonb_build_object('marketplace',l.marketplace,'item_id',l.item_id,
				'imei_suffix',l.imei_suffix,'registered_at',l.registered_at) ELSE NULL END AS listing`).
			Join("JOIN device_ownerships o ON o.id=c.ownership_id").
			Join("LEFT JOIN certificate_listings l ON l.certificate_id=c.id").
			Where("c.imei=?", primary).OrderExpr("c.issued_at DESC,c.id DESC").Limit(1).Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			value := certificate.Entity()
			state.Certificate = &value
		}
		return nil
	})
	if err != nil {
		return nil, err
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

func (r *Repository) StatusHistory(ctx context.Context, imei string, limit, offset int) ([]entity.StatusRecord, int, error) {
	items := make([]entity.StatusRecord, 0)
	var total int
	err := r.db.RunInTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead}, func(ctx context.Context, tx bun.Tx) error {
		primary, err := publicPrimary(ctx, tx, imei)
		if err != nil {
			return err
		}
		if err := tx.NewRaw("SELECT count(*) FROM device_status WHERE imei=?", primary).Scan(ctx, &total); err != nil {
			return err
		}
		return statusQuery(tx, &items).Where("s.imei=?", primary).OrderExpr("l.seq ASC").Limit(int64(limit)).Offset(int64(offset)).Scan(ctx)
	})
	return items, total, err
}
