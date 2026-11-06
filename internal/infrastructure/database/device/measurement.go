package device

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/infrastructure/database/authorization"
)

func (r *Repository) AppendMeasurement(ctx context.Context, principal entity.Principal, imei string, log *entity.MeasurementLog) (*entity.MeasurementReceipt, error) {
	var result *entity.MeasurementReceipt
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		primary, err := primaryIMEIByAlias(ctx, tx, imei)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "SELECT imei FROM devices WHERE imei=? FOR UPDATE", primary); err != nil {
			return err
		}
		if err := authorization.RequireDevice(ctx, tx, principal, primary); err != nil {
			return err
		}
		if log.Payload.IMEI != primary {
			return entity.ErrInvalidMeasurement
		}
		var ownership int64
		if err := tx.NewRaw(`SELECT id FROM device_ownerships WHERE imei=? AND ended_at IS NULL
			AND owner_issuer=? AND owner_subject=?`, primary, principal.Issuer, principal.Subject).Scan(ctx, &ownership); err != nil {
			return err
		}
		var existing struct {
			CanonicalPayload    string
			Signature           []byte
			OwnershipID         int64
			KeyID               string
			ReceivedAt          time.Time
			SignatureVerifiedAt *time.Time
		}
		err = tx.NewRaw(`SELECT canonical_payload,signature,ownership_id,key_id,received_at,signature_verified_at
			FROM device_logs WHERE imei=? AND seq=?`, primary, log.Payload.Sequence).Scan(ctx, &existing)
		if err == nil {
			// Exact retries are read-only: do not renew the 3-day activity timer.
			if existing.OwnershipID != ownership || existing.KeyID != log.Payload.KeyID || existing.SignatureVerifiedAt == nil ||
				existing.CanonicalPayload != log.CanonicalPayload || !bytes.Equal(existing.Signature, log.Signature) {
				return entity.ErrMeasurementConflict
			}
			result = &entity.MeasurementReceipt{IMEI: primary, Sequence: log.Payload.Sequence, PayloadHash: hex.EncodeToString(log.Hash[:]),
				ReceivedAt: existing.ReceivedAt, SignatureVerified: true, Duplicate: true}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var key struct {
			PublicKey             string
			SignatureAlgorithm    string
			AttestationVerifiedAt *time.Time
		}
		err = tx.NewRaw(`SELECT public_key,signature_algorithm,attestation_verified_at FROM device_keys
			WHERE id=? AND imei=? AND ownership_id=? AND revoked_at IS NULL`, log.Payload.KeyID, primary, ownership).Scan(ctx, &key)
		if errors.Is(err, sql.ErrNoRows) {
			return entity.ErrMeasurementSignature
		}
		if err != nil {
			return err
		}
		if key.SignatureAlgorithm != "ES256" || key.AttestationVerifiedAt == nil {
			return entity.ErrMeasurementSignature
		}
		if err := log.Verify(key.PublicKey); err != nil {
			return err
		}
		var previous struct {
			Seq         int64
			PayloadHash []byte
		}
		err = tx.NewRaw("SELECT seq,payload_hash FROM device_logs WHERE imei=? ORDER BY seq DESC LIMIT 1", primary).Scan(ctx, &previous)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var prevHash []byte
		if log.Payload.PreviousHash != nil {
			prevHash, _ = hex.DecodeString(*log.Payload.PreviousHash)
		}
		if log.Payload.Sequence != previous.Seq+1 || !bytes.Equal(prevHash, previous.PayloadHash) {
			return entity.ErrMeasurementConflict
		}
		var stored struct {
			ID         string
			ReceivedAt time.Time
		}
		err = tx.NewRaw(`INSERT INTO device_logs
			(imei,ownership_id,key_id,seq,type,canonicalization,canonical_payload,payload,payload_hash,prev_hash,signature,collected_at,signature_verified_at)
			VALUES (?,?,?,?,?,?,?,?::jsonb,?,?,?,?,clock_timestamp()) RETURNING id,received_at`,
			primary, ownership, log.Payload.KeyID, log.Payload.Sequence, log.Payload.Type, entity.MeasurementCanonicalization,
			log.CanonicalPayload, log.CanonicalPayload, log.Hash[:], prevHash, log.Signature, log.Payload.CollectedAt).Scan(ctx, &stored)
		if err != nil {
			return err
		}
		if err := insertMeasurementProjection(ctx, tx, stored.ID, primary, log.Payload); err != nil {
			return err
		}
		result = &entity.MeasurementReceipt{IMEI: primary, Sequence: log.Payload.Sequence, PayloadHash: hex.EncodeToString(log.Hash[:]),
			ReceivedAt: stored.ReceivedAt, SignatureVerified: true}
		return nil
	})
	return result, err
}

func insertMeasurementProjection(ctx context.Context, tx bun.Tx, id, imei string, p entity.MeasurementPayload) error {
	if s := p.Spec; s != nil {
		_, err := tx.ExecContext(ctx, `INSERT INTO device_specs
			(log_id,imei,manufacturer,model,soc,cpu_cores,ram_bytes,storage_bytes,screen_size_inch,screen_density_dpi,collected_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`, id, imei, s.Manufacturer, s.Model, s.SoC, s.CPUCores, s.RAMBytes, s.StorageBytes, s.ScreenSizeInch, s.ScreenDensityDPI, p.CollectedAt)
		return err
	}
	s := p.Status
	_, err := tx.ExecContext(ctx, `INSERT INTO device_status
		(log_id,imei,os_version,sdk_int,battery_temp_c,battery_cycle_count,battery_charge_mah,battery_full_charge_mah,boot_count,had_abnormal_exit,thermal_headroom,collected_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, id, imei, s.OSVersion, s.SDKInt, s.BatteryTempC, s.BatteryCycleCount, s.BatteryChargeMAh, s.BatteryFullChargeMAh, s.BootCount, s.HadAbnormalExit, s.ThermalHeadroom, p.CollectedAt)
	return err
}
