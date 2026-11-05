//go:build integration

package device

import (
	"context"
	"errors"
	"testing"

	"smapass-back/internal/domain/entity"
	"smapass-back/internal/infrastructure/database/certificate"
	"smapass-back/internal/infrastructure/database/testutil"
	"smapass-back/internal/usecase"
)

func TestLifetimeStateAndChainSurviveSale(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	r := NewRepository(db)
	u := usecase.NewDeviceUsecase(r, testutil.Registration(t, db))
	const imei = "000000000094821"
	const alias = "356938035643809"
	initial, err := r.FindState(ctx, imei)
	if err != nil || initial.RecordCount != 28 || initial.Status.Sequence != 28 {
		t.Fatalf("initial lifetime state: %+v %v", initial, err)
	}
	_, oldTokens, old := credentials(t, db, imei)
	first, err := u.Register(ctx, old, input(t, db, old, imei, alias))
	if err != nil || first.NextSequence != 29 || first.PreviousHash == nil {
		t.Fatalf("first continuation: %+v %v", first, err)
	}
	if err := u.Revoke(ctx, old, alias); err != nil {
		t.Fatal(err)
	}
	released, err := r.FindState(ctx, alias)
	if err != nil || released.RegistrationStatus != "released" || released.RecordCount != 28 || released.Certificate == nil || released.Certificate.ID != initial.Certificate.ID || released.Certificate.Listing != nil {
		t.Fatalf("sale hid history or retained old listing: %+v %v", released, err)
	}
	if items, total, err := r.StatusHistory(ctx, alias, 2, 27); err != nil || total != 28 || len(items) != 1 || items[0].Sequence != 28 {
		t.Fatalf("released history: %+v %d %v", items, total, err)
	}
	auth, _, next := credentials(t, db, alias)
	if _, err := auth.Authenticate(ctx, oldTokens.AccessToken); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("previous holder retained authority: %v", err)
	}
	second, err := u.Register(ctx, next, input(t, db, next, alias))
	if err != nil || second.IMEI != imei || second.OwnershipID == first.OwnershipID || second.NextSequence != 29 || second.PreviousHash == nil || *second.PreviousHash != *first.PreviousHash {
		t.Fatalf("next holder did not inherit chain position: %+v %v", second, err)
	}
	if err := insertLog(ctx, db, second, 1, true); err == nil {
		t.Fatal("sequence restarted after sale")
	}
	if err := insertLog(ctx, db, second, 30, true); err == nil {
		t.Fatal("skipped sequence accepted")
	}
	if err := insertLog(ctx, db, second, 29, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO device_status (log_id,imei,os_version,sdk_int,collected_at)
		SELECT id,imei,'16',36,collected_at FROM device_logs WHERE imei=? AND seq=29`, imei); err != nil {
		t.Fatal(err)
	}
	continued, err := r.FindState(ctx, imei)
	if err != nil || continued.RecordCount != 29 || continued.Status.Sequence != 29 || continued.Status.BatteryTempC != nil {
		t.Fatalf("continued observation or unknown measurement: %+v %v", continued, err)
	}
	if items, total, err := r.StatusHistory(ctx, imei, 500, 0); err != nil || total != 29 || len(items) != 29 || items[0].Sequence != 1 || items[28].Sequence != 29 {
		t.Fatalf("history reset after transfer: %d %d %v", len(items), total, err)
	}
	if err := insertLog(ctx, db, first, 30, true); err == nil {
		t.Fatal("previous holder's key appended after transfer")
	}
	// Publication is a fixture: issuance and verification APIs remain unimplemented.
	id := fixtureCertificate(t, db, second)
	if _, err := db.ExecContext(ctx, `INSERT INTO certificate_logs (certificate_id,imei,log_id)
		SELECT ?,imei,id FROM device_logs WHERE imei=? AND seq IN (28,29)`, id, imei); err != nil {
		t.Fatal(err)
	}
	items, total, err := certificate.NewRepository(db).Records(ctx, alias, 500, 0)
	if err != nil || total != 29 || len(items) != 29 || items[28].PreviousHash == nil || *items[28].PreviousHash != items[27].PayloadHash {
		t.Fatalf("published lifetime evidence lost, duplicated or disconnected: %d %d %v", len(items), total, err)
	}
}

func TestKnownDeviceStateBeforeIssuanceAndWithoutMeasurements(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	_, _, principal := credentials(t, db, "490154203237518")
	r := NewRepository(db)
	state, err := r.FindState(ctx, principal.IMEI)
	if err != nil || state.RecordCount != 0 || state.Spec != nil || state.Status != nil || state.Certificate != nil || state.FirstCollectedAt != nil || state.LastCollectedAt != nil {
		t.Fatalf("known device requires issuance or invented measurements: %+v %v", state, err)
	}
	if items, total, err := r.StatusHistory(ctx, principal.IMEI, 100, 0); err != nil || total != 0 || items == nil || len(items) != 0 {
		t.Fatalf("empty history: %+v %d %v", items, total, err)
	}
	if err := r.Revoke(ctx, principal, principal.IMEI); err != nil {
		t.Fatal(err)
	}
	if state, err := r.FindState(ctx, principal.IMEI); err != nil || state.RegistrationStatus != "released" {
		t.Fatalf("released device unavailable: %+v %v", state, err)
	}
}
