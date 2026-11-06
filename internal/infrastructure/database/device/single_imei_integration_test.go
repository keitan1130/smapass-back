//go:build integration

package device

import (
	"context"
	"testing"

	"smapass-back/internal/domain/entity"
	"smapass-back/internal/infrastructure/database/testutil"
	"smapass-back/internal/usecase"
)

func TestRegisteredIMEIsHaveIndependentStableUUIDs(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	r := NewRepository(db)
	auth, _, first := credentials(t, db, "490154203237518")
	_, _, second := credentials(t, db, "356938035643809")
	refs, err := r.Search(ctx, []string{first.IMEI, second.IMEI, first.IMEI})
	if err != nil || len(refs) != 2 {
		t.Fatalf("batch search: %+v %v", refs, err)
	}
	ids := map[string]string{}
	for _, ref := range refs {
		ids[ref.IMEI] = ref.ID
	}
	if ids[first.IMEI] == "" || ids[second.IMEI] == "" || ids[first.IMEI] == ids[second.IMEI] {
		t.Fatalf("IMEIs were merged: %+v", refs)
	}
	u := usecase.NewDeviceUsecase(r, testutil.Registration(t, db))
	if _, err = u.Register(ctx, first, input(t, db, first, first.IMEI)); err != nil {
		t.Fatal(err)
	}
	if id := publicID(t, db, first.IMEI); id != ids[first.IMEI] {
		t.Fatal("rotation changed UUID")
	}
	if err = u.Revoke(ctx, first, first.IMEI); err != nil {
		t.Fatal(err)
	}
	pair, err := auth.Enroll(ctx, testutil.Enrollment(t, db, first.IMEI))
	if err != nil {
		t.Fatal(err)
	}
	if pair.IMEI != first.IMEI || publicID(t, db, first.IMEI) != ids[first.IMEI] || publicID(t, db, second.IMEI) != ids[second.IMEI] {
		t.Fatal("re-registration changed UUID mapping")
	}
	summaries, err := r.ListStates(ctx, []string{ids[first.IMEI], ids[second.IMEI]})
	if err != nil || len(summaries) != 2 {
		t.Fatalf("batch display: %+v %v", summaries, err)
	}
	// Database uniqueness protects the one-to-one mapping in both directions.
	if _, err = db.ExecContext(ctx, "INSERT INTO devices (imei) VALUES (?)", first.IMEI); err == nil {
		t.Fatal("duplicate IMEI accepted")
	}
	if _, err = db.ExecContext(ctx, "INSERT INTO devices (imei,public_id) VALUES (?,?)", "352099001761481", ids[first.IMEI]); err == nil {
		t.Fatal("duplicate public UUID accepted")
	}
	// The app identity also remains bound to its single registered IMEI.
	if _, err = u.Challenge(ctx, second, entity.RotationChallengeRequest{IMEI: first.IMEI}); err == nil {
		t.Fatal("other device allowed to request key rotation")
	}
}
