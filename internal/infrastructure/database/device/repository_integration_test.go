//go:build integration

package device

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
	"smapass-back/internal/infrastructure/database/certificate"
	"smapass-back/internal/infrastructure/database/testutil"
	"smapass-back/internal/usecase"
)

func input(t *testing.T, imeis ...string) entity.RegisterDevice {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return entity.RegisterDevice{IMEIs: imeis, PublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))}
}

func TestRegistrationLookupAndReclaim(t *testing.T) {
	db := testutil.NewDatabase(t)
	u := usecase.NewDeviceUsecase(NewRepository(db))
	ctx := context.Background()
	a := entity.Principal{Issuer: "test", Subject: "alice"}
	b := entity.Principal{Issuer: "test", Subject: "bob"}
	request := input(t, "490154203237518", "356938035643809")
	first, err := u.Register(ctx, a, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.IMEI != request.IMEIs[0] || len(first.IMEIs) != 2 {
		t.Fatalf("bad registered device: %+v", first)
	}
	again, err := u.Register(ctx, a, request)
	if err != nil || again.OwnershipID != first.OwnershipID || !again.RegisteredAt.Equal(first.RegisteredAt) {
		t.Fatalf("registration is not idempotent: %+v %v", again, err)
	}
	rotated, err := u.Register(ctx, a, input(t, request.IMEIs...))
	if err != nil || rotated.OwnershipID != first.OwnershipID || rotated.KeyID == first.KeyID {
		t.Fatalf("key rotation failed: %+v %v", rotated, err)
	}
	if _, err = u.Lookup(ctx, a, request.IMEIs[1]); err != nil {
		t.Fatal(err)
	}
	if _, err = u.Lookup(ctx, b, request.IMEIs[0]); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("another owner can search: %v", err)
	}
	if _, err = u.Register(ctx, b, input(t, request.IMEIs[1])); !errors.Is(err, entity.ErrDeviceInUse) {
		t.Fatalf("active registration was reclaimed: %v", err)
	}
	// Exactly 15 days is inclusive. A same-owner registration above did not renew activity.
	if _, err = db.ExecContext(ctx, "UPDATE device_ownerships SET started_at = clock_timestamp() - interval '15 days' WHERE id = ?", first.OwnershipID); err != nil {
		t.Fatal(err)
	}
	claimed, err := u.Register(ctx, b, input(t, request.IMEIs[1]))
	if err != nil || claimed.IMEI != first.IMEI || claimed.OwnershipID == first.OwnershipID {
		t.Fatalf("reclaim failed: %+v %v", claimed, err)
	}
	if _, err = u.Lookup(ctx, a, first.IMEI); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("previous owner retained access: %v", err)
	}
	var active int
	if err = db.NewRaw("SELECT count(*) FROM device_keys WHERE imei = ? AND revoked_at IS NULL", first.IMEI).Scan(ctx, &active); err != nil || active != 1 {
		t.Fatalf("active key count=%d: %v", active, err)
	}
}

func insertLog(ctx context.Context, db *bun.DB, device *entity.Device, seq int, verified bool) error {
	var marker any
	if verified {
		marker = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	// This is a database-level fixture, not a signature-verification implementation.
	_, err := db.ExecContext(ctx, `INSERT INTO device_logs (imei, ownership_id, key_id, seq, type,
		canonicalization, canonical_payload, payload, payload_hash, prev_hash, signature, collected_at, received_at, signature_verified_at)
		VALUES (?, ?, ?, ?, 'status', 'test', ?, ?::jsonb, sha256(convert_to(?, 'UTF8')),
		CASE WHEN ? = 1 THEN NULL ELSE decode(repeat('ab',32), 'hex') END, decode('01','hex'), '2000-01-01', '2000-01-01', ?)`,
		device.IMEI, device.OwnershipID, device.KeyID, seq, `{"seq":`+strconv.Itoa(seq)+`}`, `{"seq":`+strconv.Itoa(seq)+`}`,
		`{"seq":`+strconv.Itoa(seq)+`}`, seq, marker)
	return err
}

func TestVerifiedActivityAndConcurrentClaims(t *testing.T) {
	db := testutil.NewDatabase(t)
	u := usecase.NewDeviceUsecase(NewRepository(db))
	ctx := context.Background()
	a := entity.Principal{Issuer: "test", Subject: "alice"}
	first, err := u.Register(ctx, a, input(t, "490154203237518"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE device_ownerships SET started_at = now() - interval '30 days' WHERE id = ?", first.OwnershipID); err != nil {
		t.Fatal(err)
	}
	if err = insertLog(ctx, db, first, 1, true); err != nil {
		t.Fatal(err)
	}
	if _, err = u.Register(ctx, entity.Principal{Issuer: "test", Subject: "bob"}, input(t, first.IMEI)); !errors.Is(err, entity.ErrDeviceInUse) {
		t.Fatalf("verified activity ignored: %v", err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE device_ownerships SET last_verified_log_at = now() - interval '15 days' WHERE id = ?", first.OwnershipID); err != nil {
		t.Fatal(err)
	}
	if err = insertLog(ctx, db, first, 2, false); err != nil {
		t.Fatal(err)
	}
	// Unverified data must not renew the 15-day timer.
	requests := []entity.RegisterDevice{input(t, first.IMEI), input(t, first.IMEI)}
	errorsCh := make(chan error, 2)
	var wg sync.WaitGroup
	for i, subject := range []string{"bob", "carol"} {
		wg.Add(1)
		go func(i int, subject string) {
			defer wg.Done()
			_, err := u.Register(ctx, entity.Principal{Issuer: "test", Subject: subject}, requests[i])
			errorsCh <- err
		}(i, subject)
	}
	wg.Wait()
	close(errorsCh)
	success, rejected := 0, 0
	for err := range errorsCh {
		if err == nil {
			success++
		} else if errors.Is(err, entity.ErrDeviceInUse) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("concurrent claim results: %d / %d", success, rejected)
	}
	if err = insertLog(ctx, db, first, 3, true); err == nil {
		t.Fatal("previous owner inserted a log after reclaim")
	}
}

func TestPublicCertificateStopsFollowingPreviousOwner(t *testing.T) {
	db := testutil.NewDatabase(t)
	certs := certificate.NewRepository(db)
	ctx := context.Background()
	before, err := certs.FindCurrentByIMEI(ctx, "000000000094821")
	if err != nil || before.Number != "01ABCDEFGH" {
		t.Fatalf("demo pass unavailable: %+v %v", before, err)
	}
	u := usecase.NewDeviceUsecase(NewRepository(db))
	if _, err = u.Register(ctx, entity.Principal{Issuer: "test", Subject: "new-owner"}, input(t, before.IMEI)); err != nil {
		t.Fatal(err)
	}
	if _, err = certs.FindCurrentByIMEI(ctx, before.IMEI); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("previous owner's pass is current: %v", err)
	}
	if historical, err := certs.FindByNumber(ctx, before.ID); err != nil || historical.Status != "revoked" {
		t.Fatalf("historical certificate must remain revoked: %+v %v", historical, err)
	}
}

func TestManualRevocationReleasesDeviceAndPreservesEvidence(t *testing.T) {
	db := testutil.NewDatabase(t)
	u := usecase.NewDeviceUsecase(NewRepository(db))
	ctx := context.Background()
	alice := entity.Principal{Issuer: "test", Subject: "alice"}
	bob := entity.Principal{Issuer: "test", Subject: "bob"}
	const imei = "000000000094821"
	// Assign the existing demo fixture to the test principal without changing its signed evidence.
	if _, err := db.ExecContext(ctx, "UPDATE device_ownerships SET owner_issuer = ?, owner_subject = ?, started_at = clock_timestamp() WHERE imei = ?", alice.Issuer, alice.Subject, imei); err != nil {
		t.Fatal(err)
	}
	first, err := u.Lookup(ctx, alice, imei)
	if err != nil {
		t.Fatal(err)
	}
	certs := certificate.NewRepository(db)
	before, err := certs.FindCurrentByIMEI(ctx, imei)
	if err != nil {
		t.Fatal(err)
	}
	if err = u.Revoke(ctx, bob, imei); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("another owner revoked a registration: %v", err)
	}
	if err = u.Revoke(ctx, alice, imei); err != nil {
		t.Fatal(err)
	}
	if _, err = certs.FindCurrentByIMEI(ctx, imei); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("revoked certificate is public current: %v", err)
	}
	historical, err := certs.FindByNumber(ctx, before.ID)
	if err != nil || historical.Status != "revoked" || !historical.IssuedAt.Equal(before.IssuedAt) {
		t.Fatalf("historical certificate lost: %+v %v", historical, err)
	}
	_, count, err := certs.Records(ctx, before.ID, 100, 0)
	if err != nil || count != 28 {
		t.Fatalf("historical evidence lost: %d %v", count, err)
	}
	if err = insertLog(ctx, db, first, 29, true); err == nil {
		t.Fatal("revoked registration inserted a log")
	}
	claimed, err := u.Register(ctx, bob, input(t, imei))
	if err != nil || claimed.OwnershipID == first.OwnershipID {
		t.Fatalf("manual revocation did not release immediately: %+v %v", claimed, err)
	}
	if err = u.Revoke(ctx, alice, imei); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("previous owner revoked new registration: %v", err)
	}
}
