//go:build integration

package device

import (
	"context"
	"testing"
	"time"

	"smapass-back/internal/domain/entity"
	"smapass-back/internal/infrastructure/database/testutil"
	"smapass-back/internal/usecase"
)

func TestLatestObservationsUseCollectionTimeThenSequence(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	r := NewRepository(db)
	u := usecase.NewDeviceUsecase(r, nil)
	_, principal, device, key := measurementSession(t, db, "490154203237518")
	latest := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	older := latest.Add(-time.Hour)
	oldest := older.Add(-time.Hour)
	payload := entity.MeasurementPayload{SchemaVersion: 1, DeviceID: device.DeviceID, KeyID: device.KeyID}
	send := func(at time.Time, spec *entity.SpecMeasurement, status *entity.StatusMeasurement) {
		t.Helper()
		payload.Sequence++
		payload.CollectedAt, payload.Spec, payload.Status = at, spec, status
		payload.Type = "status"
		if spec != nil {
			payload.Type = "spec"
		}
		receipt, err := u.Ingest(ctx, principal, device.DeviceID, signedMeasurement(t, key, payload))
		if err != nil {
			t.Fatal(err)
		}
		payload.PreviousHash = &receipt.PayloadHash
	}
	send(latest, &entity.SpecMeasurement{Manufacturer: "Google", Model: "latest"}, nil)
	send(latest, nil, &entity.StatusMeasurement{OSVersion: "15", SDKInt: 35})
	send(older, &entity.SpecMeasurement{Manufacturer: "Google", Model: "backfilled"}, nil)
	send(older, nil, &entity.StatusMeasurement{OSVersion: "14", SDKInt: 34})
	id := device.DeviceID
	state, err := r.FindState(ctx, id, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if state.Spec == nil || state.Spec.Model != "latest" || !state.Spec.CollectedAt.Equal(latest) || state.Status == nil || state.Status.Sequence != 2 || !state.Status.CollectedAt.Equal(latest) || state.LastCollectedAt == nil || !state.LastCollectedAt.Equal(latest) {
		t.Fatalf("late older observations replaced latest: %+v", state)
	}
	// Equal timestamps are resolved by the larger sequence, for both types.
	send(latest, &entity.SpecMeasurement{Manufacturer: "Google", Model: "same-time-newer-sequence"}, nil)
	send(latest, nil, &entity.StatusMeasurement{OSVersion: "16", SDKInt: 36})
	// A later received record still contributes to history, not to current state.
	send(oldest, nil, &entity.StatusMeasurement{OSVersion: "13", SDKInt: 33})
	state, err = r.FindState(ctx, id, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if state.Spec == nil || state.Spec.Model != "same-time-newer-sequence" || state.Spec.OSVersion == nil || *state.Spec.OSVersion != "16" || state.Status == nil || state.Status.Sequence != 6 || state.FirstCollectedAt == nil || !state.FirstCollectedAt.Equal(oldest) || state.LastCollectedAt == nil || !state.LastCollectedAt.Equal(latest) || state.RecordCount != 7 {
		t.Fatalf("collection-time selection or tie-break failed: %+v", state)
	}
	expected := []int64{2, 4, 6, 7}
	if state.History.Total != len(expected) || len(state.History.Items) != len(expected) {
		t.Fatalf("history lost observations: %+v", state.History)
	}
	for i, seq := range expected {
		if state.History.Items[i].Sequence != seq {
			t.Fatal("history no longer follows sequence order")
		}
	}
	summaries, err := r.ListStates(ctx, []string{id})
	if err != nil || len(summaries) != 1 || summaries[0].Status == nil || summaries[0].Status.Sequence != 6 || summaries[0].Spec == nil || summaries[0].Spec.Model != state.Spec.Model {
		t.Fatalf("summary uses a different latest observation: %+v %v", summaries, err)
	}
	records, total, err := r.Records(ctx, id, 100, 0)
	if err != nil || total != 7 || len(records) != 7 {
		t.Fatalf("evidence missing: %d %v", total, err)
	}
	for i, record := range records {
		if record.Sequence != int64(i+1) {
			t.Fatal("signed evidence reordered")
		}
		if i > 0 && (record.PreviousHash == nil || *record.PreviousHash != records[i-1].PayloadHash) {
			t.Fatal("hash chain broken")
		}
	}
}
