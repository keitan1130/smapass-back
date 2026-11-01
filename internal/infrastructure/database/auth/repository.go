package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
	"smapass-back/internal/infrastructure/database/keychallenge"
)

type Repository struct{ db *bun.DB }

func NewRepository(db *bun.DB) *Repository { return &Repository{db: db} }

var _ repository.AuthRepository = (*Repository)(nil)

func (r *Repository) Enroll(ctx context.Context, input entity.EnrollDevice, material entity.TokenMaterial) (*entity.TokenPair, error) {
	imei := input.IMEI
	if input.Verified == nil {
		return nil, entity.ErrAttestationRejected
	}
	var pair *entity.TokenPair
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(?,0))", imei); err != nil {
			return err
		}
		primary := imei
		err := tx.NewRaw("SELECT primary_imei FROM device_imei_aliases WHERE imei=?", imei).Scan(ctx, &primary)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) {
			if _, err := tx.ExecContext(ctx, "INSERT INTO devices (imei,imei_suffix) VALUES (?,?)", imei, imei[11:]); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO device_imei_aliases (imei,primary_imei) VALUES (?,?)", imei, imei); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "SELECT imei FROM devices WHERE imei=? FOR UPDATE", primary); err != nil {
			return err
		}
		if _, err := keychallenge.Consume(ctx, tx, "enroll", primary, entity.Principal{}, []string{imei}, input.Verified); err != nil {
			return err
		}
		var now time.Time
		if err := tx.NewRaw("SELECT clock_timestamp()").Scan(ctx, &now); err != nil {
			return err
		}
		var old struct {
			ID                int64
			StartedAt         time.Time
			LastVerifiedLogAt *time.Time
		}
		err = tx.NewRaw("SELECT id,started_at,last_verified_log_at FROM device_ownerships WHERE imei=? AND ended_at IS NULL", primary).Scan(ctx, &old)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if old.ID != 0 {
			activity := old.StartedAt
			if old.LastVerifiedLogAt != nil {
				activity = *old.LastVerifiedLogAt
			}
			if now.Before(activity.Add(entity.DeviceReclaimAfter)) {
				return entity.ErrDeviceInUse
			}
			if _, err := tx.ExecContext(ctx, "UPDATE device_ownerships SET ended_at=? WHERE id=?", now, old.ID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE app_installations SET revoked_at=? WHERE ownership_id=? AND revoked_at IS NULL", now, old.ID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE device_keys SET revoked_at=? WHERE ownership_id=? AND revoked_at IS NULL", now, old.ID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE certificates SET status='revoked' WHERE ownership_id=? AND status='valid'", old.ID); err != nil {
				return err
			}
		}
		installation := uuid.NewString()
		var ownership int64
		if err := tx.NewRaw("INSERT INTO device_ownerships (imei,owner_issuer,owner_subject,started_at) VALUES (?,?,?,?) RETURNING id", primary, entity.DeviceIssuer, installation, now).Scan(ctx, &ownership); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO app_installations (id,imei,ownership_id,created_at) VALUES (?,?,?,?)", installation, primary, ownership, now); err != nil {
			return err
		}
		evidence, err := json.Marshal(input.Verified)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO device_keys (imei,ownership_id,public_key,signature_algorithm,attestation,registration_challenge_id,attestation_verified_at,imei_attested)
			VALUES (?,?,?,'ES256',?::jsonb,?,?,?)`, primary, ownership, input.PublicKey, string(evidence), input.Verified.ChallengeID, now, input.Verified.IMEIAttested); err != nil {
			return err
		}
		pair, err = issue(ctx, tx, installation, primary, material, now)
		return err
	})
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) && pgErr.Field('C') == "23505" {
		return nil, entity.ErrDeviceConflict
	}
	return pair, err
}

func issue(ctx context.Context, tx bun.Tx, installation, imei string, material entity.TokenMaterial, now time.Time) (*entity.TokenPair, error) {
	expires := now.Add(entity.RefreshLifetime)
	_, err := tx.ExecContext(ctx, `INSERT INTO app_tokens (installation_id,access_hash,refresh_hash,access_expires_at,refresh_expires_at,created_at)
		VALUES (?,?,?,?,?,?)`, installation, entity.TokenDigest(material.Access), entity.TokenDigest(material.Refresh), now.Add(entity.AccessLifetime), expires, now)
	if err != nil {
		return nil, err
	}
	return &entity.TokenPair{IMEI: imei, AccessToken: material.Access, RefreshToken: material.Refresh, TokenType: "Bearer", ExpiresIn: int(entity.AccessLifetime.Seconds()), RefreshExpiresAt: expires}, nil
}

type refreshRow struct {
	ID               string
	InstallationID   string
	IMEI             string
	RefreshExpiresAt time.Time
	RotatedAt        *time.Time
	RevokedAt        *time.Time
	EndedAt          *time.Time
}

// Every mutating path uses device -> installation -> token lock order.
func lockRefresh(ctx context.Context, tx bun.Tx, token string) (*refreshRow, error) {
	var imei string
	err := tx.NewRaw(`SELECT i.imei FROM app_tokens t JOIN app_installations i ON i.id=t.installation_id WHERE t.refresh_hash=?`, entity.TokenDigest(token)).Scan(ctx, &imei)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, entity.ErrUnauthenticated
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "SELECT imei FROM devices WHERE imei=? FOR UPDATE", imei); err != nil {
		return nil, err
	}
	var row refreshRow
	err = tx.NewRaw(`SELECT t.id,t.installation_id,i.imei,t.refresh_expires_at,t.rotated_at,i.revoked_at,o.ended_at
		FROM app_tokens t JOIN app_installations i ON i.id=t.installation_id
		JOIN device_ownerships o ON o.id=i.ownership_id WHERE t.refresh_hash=? FOR UPDATE OF i`, entity.TokenDigest(token)).Scan(ctx, &row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, entity.ErrUnauthenticated
	}
	return &row, err
}

func (r *Repository) Refresh(ctx context.Context, token string, material entity.TokenMaterial) (*entity.TokenPair, error) {
	var pair *entity.TokenPair
	var rejected bool
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		row, err := lockRefresh(ctx, tx, token)
		if err != nil {
			return err
		}
		var now time.Time
		if err := tx.NewRaw("SELECT clock_timestamp()").Scan(ctx, &now); err != nil {
			return err
		}
		if row.RotatedAt != nil {
			// Commit revocation even though the caller will receive 401.
			rejected = true
			_, err := tx.ExecContext(ctx, "UPDATE app_installations SET revoked_at=? WHERE id=? AND revoked_at IS NULL", now, row.InstallationID)
			return err
		}
		if row.RevokedAt != nil || row.EndedAt != nil || !now.Before(row.RefreshExpiresAt) {
			return entity.ErrUnauthenticated
		}
		if _, err := tx.ExecContext(ctx, "UPDATE app_tokens SET rotated_at=? WHERE id=?", now, row.ID); err != nil {
			return err
		}
		pair, err = issue(ctx, tx, row.InstallationID, row.IMEI, material, now)
		return err
	})
	if err == nil && rejected {
		return nil, entity.ErrUnauthenticated
	}
	return pair, err
}

func (r *Repository) Revoke(ctx context.Context, token string) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		row, err := lockRefresh(ctx, tx, token)
		if errors.Is(err, entity.ErrUnauthenticated) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE app_installations SET revoked_at=clock_timestamp() WHERE id=? AND revoked_at IS NULL", row.InstallationID)
		return err
	})
}

func (r *Repository) Authenticate(ctx context.Context, token string) (entity.Principal, error) {
	var p entity.Principal
	err := r.db.NewRaw(`SELECT t.id AS session_id, i.id AS subject,i.imei FROM app_tokens t
		JOIN app_installations i ON i.id=t.installation_id JOIN device_ownerships o ON o.id=i.ownership_id
		WHERE t.access_hash=? AND t.rotated_at IS NULL AND t.access_expires_at>clock_timestamp()
		AND i.revoked_at IS NULL AND o.ended_at IS NULL AND o.owner_issuer=? AND o.owner_subject=i.id::text`, entity.TokenDigest(token), entity.DeviceIssuer).Scan(ctx, &p)
	if errors.Is(err, sql.ErrNoRows) {
		return entity.Principal{}, entity.ErrUnauthenticated
	}
	p.Issuer = entity.DeviceIssuer
	return p, err
}
