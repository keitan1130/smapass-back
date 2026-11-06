package keychallenge

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
	"smapass-back/internal/infrastructure/database/authorization"
)

type Repository struct{ db *bun.DB }

func NewRepository(db *bun.DB) *Repository { return &Repository{db: db} }

var _ repository.KeyChallengeRepository = (*Repository)(nil)

func (r *Repository) Create(ctx context.Context, purpose string, identity string, p entity.Principal) (*entity.KeyChallenge, error) {
	var result *entity.KeyChallenge
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var installation, enrollmentIMEI, deviceID any
		if purpose == "enroll" {
			enrollmentIMEI = identity
		}
		if purpose == "rotate" {
			installation = p.Subject
			deviceID = identity
			if _, err := tx.ExecContext(ctx, "SELECT id FROM devices WHERE id=? FOR UPDATE", identity); err != nil {
				return err
			}
			if err := authorization.RequireDevice(ctx, tx, p, identity); err != nil {
				return err
			}
		}
		id := uuid.NewString()
		var nonce [32]byte
		_, _ = rand.Read(nonce[:])
		// This exact byte string is signed by Android. It is not a JCS/log envelope.
		payload, err := json.Marshal(struct {
			Protocol       string `json:"protocol"`
			Purpose        string `json:"purpose"`
			ID             string `json:"id"`
			IMEI           any    `json:"imei,omitempty"`
			DeviceID       any    `json:"device_id,omitempty"`
			InstallationID string `json:"installation_id"`
			Nonce          string `json:"nonce"`
		}{"smapass-key-registration-v3", purpose, id, enrollmentIMEI, deviceID, p.Subject, base64.StdEncoding.EncodeToString(nonce[:])})
		if err != nil {
			return err
		}
		var now time.Time
		if err := tx.NewRaw("SELECT clock_timestamp()").Scan(ctx, &now); err != nil {
			return err
		}
		expires := now.Add(entity.KeyChallengeLifetime)
		if _, err := tx.ExecContext(ctx, `INSERT INTO key_registration_challenges (id,imei,device_id,purpose,installation_id,nonce,signing_payload,created_at,expires_at) VALUES (?,?,?,?,?,?,?,?,?)`, id, enrollmentIMEI, deviceID, purpose, installation, nonce[:], payload, now, expires); err != nil {
			return err
		}
		result = &entity.KeyChallenge{ID: id, AttestationChallenge: base64.StdEncoding.EncodeToString(nonce[:]), SigningPayload: base64.StdEncoding.EncodeToString(payload), ExpiresAt: expires}
		return nil
	})
	return result, err
}

type record struct {
	ID, IMEI, DeviceID, Purpose string
	InstallationID              *string
	Nonce, SigningPayload       []byte
	ExpiresAt                   time.Time
	ConsumedAt                  *time.Time
}

func find(ctx context.Context, db bun.IDB, id string, lock bool) (*entity.ChallengeContext, error) {
	var row record
	query := "SELECT c.id,COALESCE(c.imei,d.imei,'') AS imei,COALESCE(c.device_id::text,'') AS device_id,c.purpose,c.installation_id,c.nonce,c.signing_payload,c.expires_at,c.consumed_at FROM key_registration_challenges c LEFT JOIN devices d ON d.id=c.device_id WHERE c.id=?"
	if lock {
		query += " FOR UPDATE OF c"
	}
	err := db.NewRaw(query, id).Scan(ctx, &row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, entity.ErrChallengeInvalid
	}
	if err != nil {
		return nil, err
	}
	result := &entity.ChallengeContext{ID: row.ID, IMEI: row.IMEI, DeviceID: row.DeviceID, Purpose: row.Purpose, Nonce: row.Nonce, Payload: row.SigningPayload, ExpiresAt: row.ExpiresAt, ConsumedAt: row.ConsumedAt}
	if row.InstallationID != nil {
		result.InstallationID = *row.InstallationID
	}
	return result, nil
}
func (r *Repository) Find(ctx context.Context, id string) (*entity.ChallengeContext, error) {
	return find(ctx, r.db, id, false)
}

// Consume runs after the device/installation locks in the transaction that saves
// the key. A rollback leaves the challenge reusable; successful issuance uses it once.
func Consume(ctx context.Context, tx bun.Tx, purpose string, identity string, p entity.Principal, verified *entity.VerifiedKey) (bool, error) {
	if verified == nil || verified.PolicyVersion != entity.KeyPolicyVersion {
		return false, entity.ErrAttestationRejected
	}
	challenge, err := find(ctx, tx, verified.ChallengeID, true)
	if err != nil {
		return false, err
	}
	var now time.Time
	if err := tx.NewRaw("SELECT clock_timestamp()").Scan(ctx, &now); err != nil {
		return false, err
	}
	if !now.Before(verified.ValidUntil) {
		return false, entity.ErrAttestationRejected
	}
	if !now.Before(challenge.ExpiresAt) || challenge.Purpose != purpose || (purpose == "enroll" && challenge.IMEI != identity) || (purpose == "rotate" && challenge.DeviceID != identity) {
		return false, entity.ErrChallengeInvalid
	}
	if purpose == "rotate" && (challenge.InstallationID != p.Subject || challenge.DeviceID != p.DeviceID) {
		return false, entity.ErrChallengeInvalid
	}
	if purpose == "enroll" && (challenge.ConsumedAt != nil || challenge.InstallationID != "") {
		return false, entity.ErrChallengeInvalid
	}
	if challenge.ConsumedAt != nil {
		return true, nil
	}
	_, err = tx.ExecContext(ctx, "UPDATE key_registration_challenges SET consumed_at=clock_timestamp() WHERE id=?", challenge.ID)
	return false, err
}
