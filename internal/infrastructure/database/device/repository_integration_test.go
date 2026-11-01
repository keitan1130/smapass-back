//go:build integration

package device

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
	authdb "smapass-back/internal/infrastructure/database/auth"
	"smapass-back/internal/infrastructure/database/certificate"
	"smapass-back/internal/infrastructure/database/testutil"
	"smapass-back/internal/usecase"
)

func input(t *testing.T, db *bun.DB, p entity.Principal, imeis ...string) entity.RegisterDevice {
	t.Helper()
	return testutil.Rotation(t, db, p, imeis...)
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

func credentials(t *testing.T, db *bun.DB, imei string) (*usecase.AuthUsecase, *entity.TokenPair, entity.Principal) {
	t.Helper()
	u := usecase.NewAuthUsecase(authdb.NewRepository(db), testutil.Registration(t, db))
	pair, err := u.Enroll(context.Background(), testutil.Enrollment(t, db, imei))
	if err != nil {
		t.Fatal(err)
	}
	p, err := u.Authenticate(context.Background(), pair.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	return u, pair, p
}

func TestRegistrationLookupAndReclaim(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	u := usecase.NewDeviceUsecase(NewRepository(db), testutil.Registration(t, db))
	auth, old, a := credentials(t, db, "490154203237518")
	request := input(t, db, a, "490154203237518", "356938035643809")
	first, err := u.Register(ctx, a, request)
	if err != nil {
		t.Fatal(err)
	}
	again, err := u.Register(ctx, a, request)
	if err != nil || again.OwnershipID != first.OwnershipID || !again.RegisteredAt.Equal(first.RegisteredAt) {
		t.Fatalf("idempotency failed: %v", err)
	}
	rotated, err := u.Register(ctx, a, input(t, db, a, request.IMEIs...))
	if err != nil || rotated.KeyID == first.KeyID || rotated.OwnershipID != first.OwnershipID {
		t.Fatalf("key rotation: %v", err)
	}
	if _, err = u.Lookup(ctx, a, request.IMEIs[1]); err != nil {
		t.Fatal(err)
	}
	_, _, other := credentials(t, db, "000000000094821")
	if _, err = u.Lookup(ctx, other, first.IMEI); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("other installation lookup: %v", err)
	}
	if _, err = u.Register(ctx, other, input(t, db, other, first.IMEI)); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("other installation write: %v", err)
	}
	if _, err = auth.Enroll(ctx, testutil.Enrollment(t, db, request.IMEIs[1])); !errors.Is(err, entity.ErrDeviceInUse) {
		t.Fatalf("early reclaim: %v", err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE device_ownerships SET started_at=clock_timestamp()-interval '15 days' WHERE id=?", first.OwnershipID); err != nil {
		t.Fatal(err)
	}
	next, err := auth.Enroll(ctx, testutil.Enrollment(t, db, request.IMEIs[1]))
	if err != nil || next.IMEI != first.IMEI {
		t.Fatalf("alias reclaim: %v", err)
	}
	b, err := auth.Authenticate(ctx, next.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := u.Register(ctx, b, input(t, db, b, request.IMEIs[1]))
	if err != nil || claimed.OwnershipID == first.OwnershipID {
		t.Fatalf("key after reclaim: %v", err)
	}
	if _, err = auth.Authenticate(ctx, old.AccessToken); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("old credential accepted: %v", err)
	}
	if _, err = u.Lookup(ctx, a, first.IMEI); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("old owner lookup: %v", err)
	}
	var active int
	if err = db.NewRaw("SELECT count(*) FROM device_keys WHERE imei=? AND revoked_at IS NULL", first.IMEI).Scan(ctx, &active); err != nil || active != 1 {
		t.Fatalf("active keys=%d: %v", active, err)
	}
}

func TestVerifiedActivityAndConcurrentClaims(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	u := usecase.NewDeviceUsecase(NewRepository(db), testutil.Registration(t, db))
	auth, _, a := credentials(t, db, "490154203237518")
	first, err := u.Register(ctx, a, input(t, db, a, a.IMEI))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE device_ownerships SET started_at=now()-interval '30 days' WHERE id=?", first.OwnershipID); err != nil {
		t.Fatal(err)
	}
	if err = insertLog(ctx, db, first, 1, true); err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Enroll(ctx, testutil.Enrollment(t, db, first.IMEI)); !errors.Is(err, entity.ErrDeviceInUse) {
		t.Fatalf("verified activity ignored: %v", err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE device_ownerships SET last_verified_log_at=now()-interval '15 days' WHERE id=?", first.OwnershipID); err != nil {
		t.Fatal(err)
	}
	if err = insertLog(ctx, db, first, 2, false); err != nil {
		t.Fatal(err)
	}
	errorsCh := make(chan error, 2)
	var wg sync.WaitGroup
	requests := []entity.EnrollDevice{testutil.Enrollment(t, db, first.IMEI), testutil.Enrollment(t, db, first.IMEI)}
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := auth.Enroll(ctx, requests[i])
			errorsCh <- err
		}()
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
		t.Fatalf("claims %d/%d", success, rejected)
	}
	if err = insertLog(ctx, db, first, 3, true); err == nil {
		t.Fatal("old owner inserted log")
	}
}

func TestPublicCertificateStopsFollowingPreviousOwner(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	certs := certificate.NewRepository(db)
	before, err := certs.FindCurrentByIMEI(ctx, "000000000094821")
	if err != nil {
		t.Fatal(err)
	}
	credentials(t, db, before.IMEI)
	if _, err = certs.FindCurrentByIMEI(ctx, before.IMEI); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("old certificate current: %v", err)
	}
	if old, err := certs.FindByNumber(ctx, before.ID); err != nil || old.Status != "revoked" {
		t.Fatalf("history lost: %v", err)
	}
}

// A fixture certificate is created directly; this does not implement certificate issuance or signature verification.
func fixtureCertificate(t *testing.T, db *bun.DB, device *entity.Device) string {
	t.Helper()
	var id string
	err := db.NewRaw("INSERT INTO certificates (imei,ownership_id,number,issued_at,is_demo,snapshot) VALUES (?,?,'02ABCDEFGH',clock_timestamp(),true,'{}') RETURNING id", device.IMEI, device.OwnershipID).Scan(context.Background(), &id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestManualRevocationReleasesDeviceAndPreservesEvidence(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	u := usecase.NewDeviceUsecase(NewRepository(db), testutil.Registration(t, db))
	auth, old, a := credentials(t, db, "490154203237518")
	first, err := u.Register(ctx, a, input(t, db, a, a.IMEI, "356938035643809"))
	if err != nil {
		t.Fatal(err)
	}
	if err = insertLog(ctx, db, first, 1, true); err != nil {
		t.Fatal(err)
	}
	id := fixtureCertificate(t, db, first)
	if _, err = db.ExecContext(ctx, "INSERT INTO certificate_logs (certificate_id,imei,log_id) SELECT ?,imei,id FROM device_logs WHERE imei=?", id, a.IMEI); err != nil {
		t.Fatal(err)
	}
	certs := certificate.NewRepository(db)
	before, err := certs.FindByNumber(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	_, _, other := credentials(t, db, "000000000094821")
	if err = u.Revoke(ctx, other, a.IMEI); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("other installation revoked: %v", err)
	}
	if err = u.Revoke(ctx, a, "356938035643809"); err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Authenticate(ctx, old.AccessToken); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("revoked installation authenticated: %v", err)
	}
	historical, err := certs.FindByNumber(ctx, id)
	if err != nil || historical.Status != "revoked" || !historical.IssuedAt.Equal(before.IssuedAt) {
		t.Fatalf("history lost: %v", err)
	}
	_, count, err := certs.Records(ctx, id, 100, 0)
	if err != nil || count != 1 {
		t.Fatalf("evidence lost: %d %v", count, err)
	}
	if err = insertLog(ctx, db, first, 2, true); err == nil {
		t.Fatal("revoked key accepted")
	}
	next, err := auth.Enroll(ctx, testutil.Enrollment(t, db, a.IMEI))
	if err != nil {
		t.Fatalf("immediate reclaim: %v", err)
	}
	b, err := auth.Authenticate(ctx, next.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = u.Register(ctx, b, input(t, db, b, a.IMEI)); err != nil {
		t.Fatal(err)
	}
	if err = u.Revoke(ctx, a, a.IMEI); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("old session revoked new owner: %v", err)
	}
}
