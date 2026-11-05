//go:build integration

package device

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/infrastructure/auth"
	authdb "smapass-back/internal/infrastructure/database/auth"
	"smapass-back/internal/infrastructure/database/certificate"
	"smapass-back/internal/infrastructure/database/testutil"
	"smapass-back/internal/infrastructure/router"
	"smapass-back/internal/interface/controller"
	"smapass-back/internal/testutil/attestationfixture"
	"smapass-back/internal/usecase"
)

func measurementSession(t *testing.T, db *bun.DB, imei string) (*entity.TokenPair, entity.Principal, *entity.Device, *ecdsa.PrivateKey) {
	t.Helper()
	ctx := context.Background()
	keys := testutil.Registration(t, db)
	challenge, err := keys.EnrollmentChallenge(ctx, entity.EnrollmentChallengeRequest{IMEI: imei})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := attestationfixture.DefaultAuthority()
	if err != nil {
		t.Fatal(err)
	}
	options := attestationfixture.DefaultOptions()
	options.SigningKey = key
	public, proof, err := authority.Proof(challenge, options)
	if err != nil {
		t.Fatal(err)
	}
	u := usecase.NewAuthUsecase(authdb.NewRepository(db), keys)
	pair, err := u.Enroll(ctx, entity.EnrollDevice{IMEI: imei, PublicKey: public, Proof: proof})
	if err != nil {
		t.Fatal(err)
	}
	p, err := u.Authenticate(ctx, pair.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	device, err := NewRepository(db).FindOwned(ctx, p, imei)
	if err != nil {
		t.Fatal(err)
	}
	return pair, p, device, key
}

func signedMeasurement(t *testing.T, key *ecdsa.PrivateKey, payload entity.MeasurementPayload) entity.SubmitMeasurement {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	data, err = jsoncanonicalizer.Transform(data)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	signature, err := ecdsa.SignASN1(rand.Reader, key, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	return entity.SubmitMeasurement{CanonicalPayload: string(data), SignatureDER: base64.StdEncoding.EncodeToString(signature)}
}

func TestMeasurementHTTPPersistenceVerificationRetriesAndTransfer(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	r := NewRepository(db)
	u := usecase.NewDeviceUsecase(r, testutil.Registration(t, db))
	a := usecase.NewAuthUsecase(authdb.NewRepository(db), testutil.Registration(t, db))
	cc := controller.NewCertificateController(usecase.NewCertificateUsecase(certificate.NewRepository(db)))
	app := router.New(cc, controller.NewHealthController(nil), nil)
	router.AddDeviceRoutes(app, controller.NewDeviceController(u), auth.Middleware(a.Authenticate))
	tokens, p, device, key := measurementSession(t, db, "490154203237518")
	requestNumber := 0
	request := func(path, token string, body []byte, status int) []byte {
		t.Helper()
		requestNumber++
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		r.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", requestNumber)
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("status=%d want=%d body=%s", w.Code, status, w.Body.String())
		}
		return w.Body.Bytes()
	}
	send := func(input entity.SubmitMeasurement, status int) entity.MeasurementReceipt {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		response := request("/api/devices/"+device.IMEI+"/logs", tokens.AccessToken, body, status)
		var receipt entity.MeasurementReceipt
		if status < 300 {
			if err := json.Unmarshal(response, &receipt); err != nil {
				t.Fatal(err)
			}
		}
		return receipt
	}
	activity := func() time.Time {
		t.Helper()
		var at time.Time
		if err := db.NewRaw("SELECT last_verified_log_at FROM device_ownerships WHERE id=?", device.OwnershipID).Scan(ctx, &at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	path := "/api/devices/" + device.IMEI + "/logs"
	request(path, "", []byte(`{}`), 401)
	request(path, tokens.AccessToken, []byte(`{"canonical_payload":"a","canonical_payload":"b"}`), 400)
	request(path, tokens.AccessToken, []byte(strings.Repeat(" ", 128<<10+1)), 413)
	request("/api/devices/000000000094821/logs", tokens.AccessToken, []byte(`{}`), 400)
	payload := entity.MeasurementPayload{SchemaVersion: 1, IMEI: device.IMEI, KeyID: device.KeyID, Sequence: 1, Type: "spec",
		CollectedAt: time.Now().UTC().Truncate(time.Millisecond), Spec: &entity.SpecMeasurement{Manufacturer: "Google", Model: "Pixel 8"}}
	// Three inactive days permit a transfer, but do not revoke the original
	// installation. It can refresh expired access credentials and resume logging
	// until another registrant actually takes over the device.
	if _, err := db.ExecContext(ctx, "UPDATE device_ownerships SET started_at=clock_timestamp()-interval '4 days' WHERE id=?", device.OwnershipID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE app_tokens SET created_at=clock_timestamp()-interval '4 days',
		access_expires_at=clock_timestamp()-interval '3 days' WHERE id=?`, p.SessionID); err != nil {
		t.Fatal(err)
	}
	request(path, tokens.AccessToken, []byte(`{}`), 401)
	resumedTokens, err := a.Refresh(ctx, entity.RefreshRequest{RefreshToken: tokens.RefreshToken})
	if err != nil {
		t.Fatalf("original installation could not refresh after inactivity: %v", err)
	}
	tokens = resumedTokens
	p, err = a.Authenticate(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	dormant, err := r.FindOwned(ctx, p, device.IMEI)
	if err != nil || dormant.OwnershipID != device.OwnershipID || dormant.ReclaimableAt.After(time.Now()) {
		t.Fatalf("original registration lost or refresh extended inactivity: %+v %v", dormant, err)
	}
	firstInput := signedMeasurement(t, key, payload)
	first := send(firstInput, 201)
	resumed, err := r.FindOwned(ctx, p, device.IMEI)
	if err != nil || resumed.OwnershipID != device.OwnershipID || resumed.LastVerifiedLogAt == nil ||
		!resumed.LastVerifiedLogAt.Equal(first.ReceivedAt) || !resumed.ReclaimableAt.Equal(first.ReceivedAt.Add(72*time.Hour)) {
		t.Fatalf("resumed communication did not renew the original registration: %+v %v", resumed, err)
	}
	if _, err := a.Enroll(ctx, testutil.Enrollment(t, db, device.IMEI)); !errors.Is(err, entity.ErrDeviceInUse) {
		t.Fatalf("transfer allowed after the original installation resumed: %v", err)
	}
	before := activity()
	if first.Duplicate || !first.SignatureVerified {
		t.Fatal("new log receipt")
	}
	if replay := send(firstInput, 200); !replay.Duplicate || !replay.ReceivedAt.Equal(first.ReceivedAt) || !activity().Equal(before) {
		t.Fatal("retry changed evidence or registration activity")
	}
	body, _ := json.Marshal(firstInput)
	request("/api/devices/000000000094821/logs", tokens.AccessToken, body, 404)
	temperature, thermal := 32.3, 0.3123456789012345
	charge, fullCharge := 3200, 4400
	payload.Sequence, payload.Type, payload.Spec, payload.PreviousHash = 2, "status", nil, &first.PayloadHash
	payload.Status = &entity.StatusMeasurement{OSVersion: "16", SDKInt: 36, BatteryTempC: &temperature, BatteryChargeMAh: &charge, BatteryFullChargeMAh: &fullCharge, ThermalHeadroom: &thermal}
	secondInput := signedMeasurement(t, key, payload)
	second := send(secondInput, 201)
	state, err := r.FindState(ctx, device.IMEI)
	if err != nil || state.RecordCount != 2 || state.Spec == nil || state.Spec.Model != "Pixel 8" || state.Status == nil || state.Status.BatteryTempC == nil || *state.Status.BatteryTempC != temperature || state.Status.ThermalHeadroom == nil || *state.Status.ThermalHeadroom != thermal || state.Status.HadAbnormalExit != nil {
		t.Fatalf("stored projection: %+v %v", state, err)
	}
	if rows, total, err := r.StatusHistory(ctx, device.IMEI, 100, 0); err != nil || total != 1 || len(rows) != 1 || rows[0].BatteryFullChargeMAh == nil || *rows[0].BatteryFullChargeMAh != fullCharge {
		t.Fatalf("history: %+v %d %v", rows, total, err)
	}
	before = activity()
	payload.Sequence, payload.PreviousHash = 3, &second.PayloadHash
	bad := signedMeasurement(t, key, payload)
	bad.SignatureDER = base64.StdEncoding.EncodeToString([]byte{0x30, 6, 2, 1, 1, 2, 1, 1})
	send(bad, 403)
	payload.Sequence = 4
	send(signedMeasurement(t, key, payload), 409)
	payload.Sequence = 3
	wrong := strings.Repeat("0", 64)
	payload.PreviousHash = &wrong
	send(signedMeasurement(t, key, payload), 409)
	payload.Sequence, payload.PreviousHash = 2, &first.PayloadHash
	payload.Status.OSVersion = "17"
	send(signedMeasurement(t, key, payload), 409)
	if !activity().Equal(before) {
		t.Fatal("invalid writes renewed the registration")
	}
	// Force a projection failure to prove the raw log and activity update roll back.
	if _, err := db.ExecContext(ctx, `ALTER TABLE device_status ADD CONSTRAINT measurement_fixture_reject CHECK (os_version <> 'reject-fixture')`); err != nil {
		t.Fatal(err)
	}
	payload.Sequence, payload.PreviousHash, payload.Status.OSVersion = 3, &second.PayloadHash, "reject-fixture"
	send(signedMeasurement(t, key, payload), 500)
	if state, err := r.FindState(ctx, device.IMEI); err != nil || state.RecordCount != 2 || !activity().Equal(before) {
		t.Fatalf("partial write: %+v %v", state, err)
	}
	payload.Status.OSVersion = "16"
	third := send(signedMeasurement(t, key, payload), 201)
	var stored struct {
		CanonicalPayload    string
		PayloadHash         []byte
		Signature           []byte
		SignatureVerifiedAt *time.Time
	}
	if err := db.NewRaw("SELECT canonical_payload,payload_hash,signature,signature_verified_at FROM device_logs WHERE imei=? AND seq=2", device.IMEI).Scan(ctx, &stored); err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256([]byte(secondInput.CanonicalPayload))
	wantSignature, _ := base64.StdEncoding.DecodeString(secondInput.SignatureDER)
	if stored.CanonicalPayload != secondInput.CanonicalPayload || !bytes.Equal(stored.PayloadHash, wantHash[:]) || !bytes.Equal(stored.Signature, wantSignature) || stored.SignatureVerifiedAt == nil {
		t.Fatal("signed original changed or verification missing")
	}
	if err := u.Revoke(ctx, p, device.IMEI); err != nil {
		t.Fatal(err)
	}
	send(secondInput, 401)
	tokens, p, device, newKey := measurementSession(t, db, payload.IMEI)
	if device.NextSequence != 4 || device.PreviousHash == nil || *device.PreviousHash != third.PayloadHash {
		t.Fatal("transfer reset the chain")
	}
	payload.Sequence, payload.PreviousHash = 4, &third.PayloadHash
	send(signedMeasurement(t, key, payload), 403)
	payload.KeyID = device.KeyID
	fourth := send(signedMeasurement(t, newKey, payload), 201)
	if rows, total, err := r.StatusHistory(ctx, device.IMEI, 100, 0); err != nil || total != 3 || len(rows) != 3 {
		t.Fatalf("lifetime history: %d %d %v", len(rows), total, err)
	}
	if _, err := a.Refresh(ctx, entity.RefreshRequest{RefreshToken: tokens.RefreshToken}); err != nil {
		t.Fatal(err)
	}
	payload.Sequence, payload.PreviousHash = 5, &fourth.PayloadHash
	if _, err := u.Ingest(ctx, p, device.IMEI, signedMeasurement(t, newKey, payload)); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("stale in-flight credentials accepted: %v", err)
	}
}

func TestConcurrentMeasurementRetryIsSavedOnce(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	_, p, device, key := measurementSession(t, db, "490154203237518")
	u := usecase.NewDeviceUsecase(NewRepository(db), testutil.Registration(t, db))
	input := signedMeasurement(t, key, entity.MeasurementPayload{SchemaVersion: 1, IMEI: device.IMEI, KeyID: device.KeyID, Sequence: 1, Type: "status",
		CollectedAt: time.Now().UTC().Truncate(time.Millisecond), Status: &entity.StatusMeasurement{OSVersion: "16", SDKInt: 36}})
	var wg sync.WaitGroup
	results := make(chan *entity.MeasurementReceipt, 2)
	errors := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); r, err := u.Ingest(ctx, p, device.IMEI, input); results <- r; errors <- err }()
	}
	wg.Wait()
	duplicates := 0
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
		if (<-results).Duplicate {
			duplicates++
		}
	}
	state, err := NewRepository(db).FindState(ctx, device.IMEI)
	if err != nil || duplicates != 1 || state.RecordCount != 1 {
		t.Fatalf("concurrent retry duplicated records: %+v %d %v", state, duplicates, err)
	}
}
