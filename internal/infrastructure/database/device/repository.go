package device

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
	"smapass-back/internal/infrastructure/database/authorization"
)

type Repository struct{ db *bun.DB }

func NewRepository(db *bun.DB) *Repository { return &Repository{db: db} }

var _ repository.DeviceRepository = (*Repository)(nil)

func (r *Repository) Register(ctx context.Context, principal entity.Principal, input entity.RegisterDevice) (*entity.Device, error) {
	var result *entity.Device
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Overlapping IMEI sets acquire locks in the same order, including first registration.
		locks := slices.Clone(input.IMEIs)
		slices.Sort(locks)
		for _, imei := range locks {
			if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", imei); err != nil {
				return err
			}
		}
		var matches []string
		if err := tx.NewRaw("SELECT DISTINCT primary_imei FROM device_imei_aliases WHERE imei IN (?)", bun.In(input.IMEIs)).Scan(ctx, &matches); err != nil {
			return err
		}
		if len(matches) > 1 {
			return entity.ErrDeviceConflict
		}
		if len(matches) == 0 {
			return repository.ErrNotFound
		}
		primary := matches[0]
		if _, err := tx.ExecContext(ctx, "SELECT imei FROM devices WHERE imei = ? FOR UPDATE", primary); err != nil {
			return err
		}
		if err := authorization.RequireDevice(ctx, tx, principal, primary); err != nil {
			return err
		}
		var now time.Time
		if err := tx.NewRaw("SELECT clock_timestamp()").Scan(ctx, &now); err != nil {
			return err
		}
		var ownershipID int64
		if err := tx.NewRaw("SELECT id FROM device_ownerships WHERE imei = ? AND ended_at IS NULL", primary).Scan(ctx, &ownershipID); err != nil {
			return err
		}
		var activeKey struct{ ID, PublicKey string }
		err := tx.NewRaw("SELECT id, public_key FROM device_keys WHERE imei = ? AND revoked_at IS NULL", primary).Scan(ctx, &activeKey)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if activeKey.PublicKey != input.PublicKey {
			var exists bool
			if err := tx.NewRaw("SELECT EXISTS (SELECT 1 FROM device_keys WHERE public_key = ?)", input.PublicKey).Scan(ctx, &exists); err != nil {
				return err
			}
			if exists {
				return entity.ErrDeviceConflict
			}
			if _, err := tx.ExecContext(ctx, "UPDATE device_keys SET revoked_at = ? WHERE imei = ? AND revoked_at IS NULL", now, primary); err != nil {
				return err
			}
			if err := tx.NewRaw("INSERT INTO device_keys (imei, ownership_id, public_key, signature_algorithm) VALUES (?, ?, ?, 'ES256') RETURNING id", primary, ownershipID, input.PublicKey).Scan(ctx, &activeKey.ID); err != nil {
				return err
			}
		}
		for _, imei := range input.IMEIs {
			if _, err := tx.ExecContext(ctx, "INSERT INTO device_imei_aliases (imei, primary_imei) VALUES (?, ?) ON CONFLICT (imei) DO NOTHING", imei, primary); err != nil {
				return err
			}
		}
		var errResult error
		result, errResult = findOwned(ctx, tx, principal, primary)
		return errResult
	})
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) && pgErr.Field('C') == "23505" {
		return nil, entity.ErrDeviceConflict
	}
	return result, err
}

func (r *Repository) FindOwned(ctx context.Context, principal entity.Principal, imei string) (*entity.Device, error) {
	return findOwned(ctx, r.db, principal, imei)
}

// Revoke releases the current registration immediately, preserving historical evidence.
func (r *Repository) Revoke(ctx context.Context, principal entity.Principal, imei string) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var primary string
		err := tx.NewRaw("SELECT primary_imei FROM device_imei_aliases WHERE imei = ?", imei).Scan(ctx, &primary)
		if errors.Is(err, sql.ErrNoRows) {
			return repository.ErrNotFound
		}
		if err != nil {
			return err
		}
		// Log ingestion and registration take the same lock before checking ownership.
		if _, err := tx.ExecContext(ctx, "SELECT imei FROM devices WHERE imei = ? FOR UPDATE", primary); err != nil {
			return err
		}
		if err := authorization.RequireDevice(ctx, tx, principal, primary); err != nil {
			return err
		}
		var id int64
		err = tx.NewRaw(`SELECT id FROM device_ownerships WHERE imei = ? AND ended_at IS NULL
			AND owner_issuer = ? AND owner_subject = ?`, primary, principal.Issuer, principal.Subject).Scan(ctx, &id)
		if errors.Is(err, sql.ErrNoRows) {
			return repository.ErrNotFound
		}
		if err != nil {
			return err
		}
		var now time.Time
		if err := tx.NewRaw("SELECT clock_timestamp()").Scan(ctx, &now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE device_ownerships SET ended_at = ? WHERE id = ?", now, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE app_installations SET revoked_at=? WHERE ownership_id=? AND revoked_at IS NULL", now, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE device_keys SET revoked_at = ? WHERE ownership_id = ? AND revoked_at IS NULL", now, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE certificates SET status = 'revoked' WHERE ownership_id = ? AND status = 'valid'", id)
		return err
	})
}

func findOwned(ctx context.Context, db bun.IDB, principal entity.Principal, imei string) (*entity.Device, error) {
	var result entity.Device
	err := db.NewRaw(`SELECT d.imei, o.id AS ownership_id, o.started_at AS registered_at,
		o.last_verified_log_at, COALESCE(o.last_verified_log_at, o.started_at) + interval '360 hours' AS reclaimable_at,
		k.id AS key_id FROM device_imei_aliases a JOIN devices d ON d.imei = a.primary_imei
		JOIN device_ownerships o ON o.imei = d.imei AND o.ended_at IS NULL
		JOIN device_keys k ON k.ownership_id = o.id AND k.revoked_at IS NULL
		WHERE a.imei = ? AND o.owner_issuer = ? AND o.owner_subject = ?`, imei, principal.Issuer, principal.Subject).Scan(ctx, &result)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	result.IMEIs = []string{}
	if err := db.NewRaw("SELECT imei FROM device_imei_aliases WHERE primary_imei = ? ORDER BY imei", result.IMEI).Scan(ctx, &result.IMEIs); err != nil {
		return nil, err
	}
	return &result, nil
}
