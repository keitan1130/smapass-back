package device

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
	"smapass-back/internal/infrastructure/database/authorization"
	"smapass-back/internal/infrastructure/database/keychallenge"
)

type Repository struct{ db *bun.DB }

func NewRepository(db *bun.DB) *Repository { return &Repository{db: db} }

var _ repository.DeviceRepository = (*Repository)(nil)

func (r *Repository) Register(ctx context.Context, principal entity.Principal, input entity.RegisterDevice) (*entity.Device, error) {
	var result *entity.Device
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		deviceID := input.DeviceID
		if err := lockDevice(ctx, tx, deviceID); err != nil {
			return err
		}
		if err := authorization.RequireDevice(ctx, tx, principal, deviceID); err != nil {
			return err
		}
		var now time.Time
		if err := tx.NewRaw("SELECT clock_timestamp()").Scan(ctx, &now); err != nil {
			return err
		}
		var ownershipID int64
		if err := tx.NewRaw("SELECT id FROM device_ownerships WHERE device_id = ? AND ended_at IS NULL", deviceID).Scan(ctx, &ownershipID); err != nil {
			return err
		}
		var activeKey struct {
			ID, PublicKey           string
			RegistrationChallengeID *string
		}
		err := tx.NewRaw("SELECT id, public_key,registration_challenge_id FROM device_keys WHERE device_id = ? AND revoked_at IS NULL", deviceID).Scan(ctx, &activeKey)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		used, err := keychallenge.Consume(ctx, tx, "rotate", deviceID, principal, input.Verified)
		if err != nil {
			return err
		}
		if used {
			if activeKey.PublicKey != input.PublicKey || activeKey.RegistrationChallengeID == nil || *activeKey.RegistrationChallengeID != input.Verified.ChallengeID {
				return entity.ErrChallengeInvalid
			}
			result, err = findOwned(ctx, tx, principal, deviceID)
			return err
		}
		if activeKey.PublicKey == input.PublicKey {
			return entity.ErrDeviceConflict
		}
		if activeKey.PublicKey != input.PublicKey {
			var exists bool
			if err := tx.NewRaw("SELECT EXISTS (SELECT 1 FROM device_keys WHERE public_key = ?)", input.PublicKey).Scan(ctx, &exists); err != nil {
				return err
			}
			if exists {
				return entity.ErrDeviceConflict
			}
			if _, err := tx.ExecContext(ctx, "UPDATE device_keys SET revoked_at = ? WHERE device_id = ? AND revoked_at IS NULL", now, deviceID); err != nil {
				return err
			}
			evidence, err := json.Marshal(input.Verified)
			if err != nil {
				return err
			}
			if err := tx.NewRaw(`INSERT INTO device_keys (device_id, ownership_id, public_key, signature_algorithm,attestation,registration_challenge_id,attestation_verified_at,imei_attested) VALUES (?, ?, ?, 'ES256',?::jsonb,?,?,?) RETURNING id`, deviceID, ownershipID, input.PublicKey, string(evidence), input.Verified.ChallengeID, now, input.Verified.IMEIAttested).Scan(ctx, &activeKey.ID); err != nil {
				return err
			}
		}
		var errResult error
		result, errResult = findOwned(ctx, tx, principal, deviceID)
		return errResult
	})
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) && pgErr.Field('C') == "23505" {
		return nil, entity.ErrDeviceConflict
	}
	return result, err
}

func (r *Repository) FindOwned(ctx context.Context, principal entity.Principal, deviceID string) (*entity.Device, error) {
	return findOwned(ctx, r.db, principal, deviceID)
}

// Revoke releases the current registration immediately, preserving historical evidence.
func (r *Repository) Revoke(ctx context.Context, principal entity.Principal, deviceID string) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := lockDevice(ctx, tx, deviceID); err != nil {
			return err
		}
		if err := authorization.RequireDevice(ctx, tx, principal, deviceID); err != nil {
			return err
		}
		var id int64
		err := tx.NewRaw(`SELECT id FROM device_ownerships WHERE device_id = ? AND ended_at IS NULL
			AND owner_issuer = ? AND owner_subject = ?`, deviceID, principal.Issuer, principal.Subject).Scan(ctx, &id)
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
		return nil
	})
}

func findOwned(ctx context.Context, db bun.IDB, principal entity.Principal, deviceID string) (*entity.Device, error) {
	var result entity.Device
	err := db.NewRaw(`SELECT d.id AS device_id, o.id AS ownership_id, o.started_at AS registered_at,
		o.last_verified_log_at,
		COALESCE(last_log.seq,0)+1 AS next_sequence, encode(last_log.payload_hash,'hex') AS previous_hash,
        k.id AS key_id,(k.attestation_verified_at IS NOT NULL) AS key_attested,k.imei_attested FROM devices d
		JOIN device_ownerships o ON o.device_id = d.id AND o.ended_at IS NULL
		JOIN device_keys k ON k.ownership_id = o.id AND k.revoked_at IS NULL
		LEFT JOIN LATERAL (SELECT seq,payload_hash FROM device_logs WHERE device_id=d.id ORDER BY seq DESC LIMIT 1) last_log ON true
        WHERE d.id = ? AND o.owner_issuer = ? AND o.owner_subject = ?`, deviceID, principal.Issuer, principal.Subject).Scan(ctx, &result)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	result.ReclaimableAt = entity.DeviceReclaimableAt(result.RegisteredAt, result.LastVerifiedLogAt)
	return &result, nil
}
