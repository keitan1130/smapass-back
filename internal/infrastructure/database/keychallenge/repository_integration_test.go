//go:build integration

package keychallenge_test

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"

	"smapass-back/internal/domain/entity"
	authdb "smapass-back/internal/infrastructure/database/auth"
	"smapass-back/internal/infrastructure/database/device"
	"smapass-back/internal/infrastructure/database/testutil"
	"smapass-back/internal/usecase"
)

func TestChallengeReplayExpiryAndAtomicRollback(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	keys := testutil.Registration(t, db)
	auth := usecase.NewAuthUsecase(authdb.NewRepository(db), keys)
	input := testutil.Enrollment(t, db, "490154203237518")
	bad := input
	signature, _ := base64.StdEncoding.DecodeString(bad.Proof.SignatureDER)
	signature[len(signature)-1] ^= 1
	bad.Proof.SignatureDER = base64.StdEncoding.EncodeToString(signature)
	if _, err := auth.Enroll(ctx, bad); !errors.Is(err, entity.ErrAttestationRejected) {
		t.Fatalf("bad proof accepted: %v", err)
	}
	var consumed bool
	if err := db.NewRaw("SELECT consumed_at IS NOT NULL FROM key_registration_challenges WHERE id=?", input.Proof.ChallengeID).Scan(ctx, &consumed); err != nil || consumed {
		t.Fatal("failed attestation consumed challenge")
	}
	credentials, err := auth.Enroll(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Enroll(ctx, input); !errors.Is(err, entity.ErrChallengeInvalid) {
		t.Fatalf("enrollment replay issued credentials: %v", err)
	}
	p, err := auth.Authenticate(ctx, credentials.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	devices := usecase.NewDeviceUsecase(device.NewRepository(db), keys)
	before, err := devices.Lookup(ctx, p, p.IMEI)
	if err != nil || !before.KeyAttested || before.IMEIAttested {
		t.Fatalf("attestation state: %+v %v", before, err)
	}
	rotation := testutil.Rotation(t, db, p, p.IMEI)
	invalid := rotation
	invalid.Proof.SignatureDER = input.Proof.SignatureDER
	if _, err = devices.Register(ctx, p, invalid); !errors.Is(err, entity.ErrAttestationRejected) {
		t.Fatalf("invalid rotation accepted: %v", err)
	}
	unchanged, err := devices.Lookup(ctx, p, p.IMEI)
	if err != nil || unchanged.KeyID != before.KeyID {
		t.Fatal("failed rotation replaced active key")
	}
	after, err := devices.Register(ctx, p, rotation)
	if err != nil || after.KeyID == before.KeyID {
		t.Fatalf("valid rotation failed: %v", err)
	}
	// The original registration period and old public key must remain immutable.
	for _, query := range []string{"UPDATE device_keys SET public_key='tampered' WHERE id=?", "UPDATE device_keys SET revoked_at=NULL WHERE id=?", "DELETE FROM device_keys WHERE id=?"} {
		if _, err := db.ExecContext(ctx, query, before.KeyID); err == nil {
			t.Fatal("key history was modified")
		}
	}
	expired := testutil.Enrollment(t, db, "356938035643809")
	if _, err = db.ExecContext(ctx, "UPDATE key_registration_challenges SET created_at=clock_timestamp()-interval '10 minutes',expires_at=clock_timestamp()-interval '5 minutes' WHERE id=?", expired.Proof.ChallengeID); err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Enroll(ctx, expired); !errors.Is(err, entity.ErrChallengeInvalid) {
		t.Fatalf("expired challenge accepted: %v", err)
	}
	// A valid enroll proof cannot be used for a different purpose or IMEI.
	swapped := testutil.Enrollment(t, db, p.IMEI)
	if _, err = devices.Register(ctx, p, entity.RegisterDevice{IMEIs: []string{p.IMEI}, PublicKey: swapped.PublicKey, Proof: swapped.Proof}); !errors.Is(err, entity.ErrChallengeInvalid) {
		t.Fatalf("purpose substitution accepted: %v", err)
	}
	swapped.IMEI = "356938035643809"
	if _, err = auth.Enroll(ctx, swapped); !errors.Is(err, entity.ErrChallengeInvalid) {
		t.Fatalf("IMEI substitution accepted: %v", err)
	}
}

func TestConcurrentEnrollmentProofIsSingleUse(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	keys := testutil.Registration(t, db)
	auth := usecase.NewAuthUsecase(authdb.NewRepository(db), keys)
	input := testutil.Enrollment(t, db, "490154203237518")
	out := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := auth.Enroll(ctx, input); out <- err }()
	}
	wg.Wait()
	close(out)
	success, rejected := 0, 0
	for err := range out {
		if err == nil {
			success++
		} else if errors.Is(err, entity.ErrChallengeInvalid) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("proof replay results: %d/%d", success, rejected)
	}
	var count int
	if err := db.NewRaw("SELECT count(*) FROM app_installations WHERE imei=?", input.IMEI).Scan(ctx, &count); err != nil || count != 1 {
		t.Fatalf("duplicate credential issuance: %d %v", count, err)
	}
}
