//go:build integration

package device

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
	"smapass-back/internal/infrastructure/auth"
	authdb "smapass-back/internal/infrastructure/database/auth"
	"smapass-back/internal/infrastructure/database/testutil"
	"smapass-back/internal/infrastructure/router"
	"smapass-back/internal/interface/controller"
	"smapass-back/internal/usecase"
)

func TestVisibilityHTTPDefaultsAndAuthorization(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	r := NewRepository(db)
	a := usecase.NewAuthUsecase(authdb.NewRepository(db), testutil.Registration(t, db))
	u := usecase.NewDeviceUsecase(r, testutil.Registration(t, db))
	owner, err := a.Enroll(ctx, testutil.Enrollment(t, db, "490154203237518"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.Enroll(ctx, testutil.Enrollment(t, db, "356938035643809"))
	if err != nil {
		t.Fatal(err)
	}
	app := router.New(controller.NewHealthController(nil), nil)
	router.AddDeviceRoutes(app, controller.NewDeviceController(u), auth.Middleware(a.Authenticate))
	n := 0
	request := func(method, path, token string, input any, status int) []byte {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		n++
		req.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", n)
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		out := httptest.NewRecorder()
		app.ServeHTTP(out, req)
		if out.Code != status {
			t.Fatalf("%s %s: %d want %d: %s", method, path, out.Code, status, out.Body.String())
		}
		if out.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("visibility response may be cached")
		}
		return out.Body.Bytes()
	}
	path := "/api/devices/" + owner.DeviceID
	visibilityPath := path + "/visibility"
	assertHidden := func() {
		t.Helper()
		request("GET", path, "", nil, 404)
		request("GET", path+"/records", "", nil, 404)
		for _, listPath := range []string{"/api/devices?ids=" + owner.DeviceID, "/api/devices/search?imeis=490154203237518"} {
			var result struct {
				Items []json.RawMessage `json:"items"`
			}
			if err := json.Unmarshal(request("GET", listPath, "", nil, 200), &result); err != nil || len(result.Items) != 0 {
				t.Fatalf("private device was listed: %+v %v", result, err)
			}
		}
	}
	assertHidden()
	var owned entity.Device
	if err := json.Unmarshal(request("POST", "/api/devices/lookup", owner.AccessToken, map[string]string{"device_id": owner.DeviceID}, 200), &owned); err != nil || owned.Visibility != "private" {
		t.Fatalf("initial visibility: %+v %v", owned, err)
	}
	request("PATCH", visibilityPath, "", entity.UpdateDeviceVisibility{Visibility: "public"}, 401)
	request("PATCH", visibilityPath, other.AccessToken, entity.UpdateDeviceVisibility{Visibility: "public"}, 404)
	for _, input := range []any{map[string]string{}, map[string]string{"visibility": "other"}, map[string]string{"visibility": "public", "owner_subject": "forged"}} {
		request("PATCH", visibilityPath, owner.AccessToken, input, 400)
	}
	request("PATCH", "/api/devices/490154203237518/visibility", owner.AccessToken, entity.UpdateDeviceVisibility{Visibility: "public"}, 400)
	var published entity.DeviceVisibility
	if err := json.Unmarshal(request("PATCH", visibilityPath, owner.AccessToken, entity.UpdateDeviceVisibility{Visibility: "public"}, 200), &published); err != nil || published.DeviceID != owner.DeviceID || published.Visibility != "public" {
		t.Fatalf("publish response: %+v %v", published, err)
	}
	request("GET", path, "", nil, 200)
	request("GET", path+"/records", "", nil, 200)
	refs, err := r.Search(ctx, []string{"490154203237518"})
	if err != nil || len(refs) != 1 || refs[0].ID != owner.DeviceID {
		t.Fatalf("published search: %+v %v", refs, err)
	}
	summaries, err := r.ListStates(ctx, []string{owner.DeviceID, other.DeviceID})
	if err != nil || len(summaries) != 1 || summaries[0].ID != owner.DeviceID {
		t.Fatalf("mixed visibility list: %+v %v", summaries, err)
	}
	request("PATCH", visibilityPath, owner.AccessToken, entity.UpdateDeviceVisibility{Visibility: "private"}, 200)
	assertHidden()
	stale, err := a.Authenticate(ctx, owner.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Refresh(ctx, entity.RefreshRequest{RefreshToken: owner.RefreshToken}); err != nil {
		t.Fatal(err)
	}
	request("PATCH", visibilityPath, owner.AccessToken, entity.UpdateDeviceVisibility{Visibility: "public"}, 401)
	if _, err = u.SetVisibility(ctx, stale, owner.DeviceID, "public"); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("stale in-flight visibility accepted: %v", err)
	}
	assertHidden()
}

func TestVisibilityExpiryRetriesResumeAndTransfer(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	r := NewRepository(db)
	u := usecase.NewDeviceUsecase(r, testutil.Registration(t, db))
	a := usecase.NewAuthUsecase(authdb.NewRepository(db), testutil.Registration(t, db))
	tokens, p, device, key := measurementSession(t, db, "490154203237518")
	payload := entity.MeasurementPayload{SchemaVersion: 1, DeviceID: p.DeviceID, KeyID: device.KeyID, Sequence: 1, Type: "status", CollectedAt: time.Now().UTC().Truncate(time.Millisecond), Status: &entity.StatusMeasurement{OSVersion: "16", SDKInt: 36}}
	input := signedMeasurement(t, key, payload)
	first, err := u.Ingest(ctx, p, p.DeviceID, input)
	if err != nil {
		t.Fatal(err)
	}
	before, err := r.FindOwned(ctx, p, p.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = u.SetVisibility(ctx, p, p.DeviceID, "private"); err != nil {
		t.Fatal(err)
	}
	after, err := r.FindOwned(ctx, p, p.DeviceID)
	if err != nil || after.Visibility != "private" || !after.ReclaimableAt.Equal(before.ReclaimableAt) {
		t.Fatalf("visibility extended activity: %+v %v", after, err)
	}
	if _, err = r.FindState(ctx, p.DeviceID, 100, 0); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("active private device visible: %v", err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE device_ownerships SET last_verified_log_at=clock_timestamp()-interval '72 hours' WHERE id=?", device.OwnershipID); err != nil {
		t.Fatal(err)
	}
	assertPublic := func() {
		t.Helper()
		if state, err := r.FindState(ctx, p.DeviceID, 100, 0); err != nil || state.RecordCount != 1 {
			t.Fatalf("reclaimable private device hidden: %+v %v", state, err)
		}
		if records, total, err := r.Records(ctx, p.DeviceID, 100, 0); err != nil || total != 1 || records[0].CanonicalPayload != input.CanonicalPayload {
			t.Fatalf("evidence was changed/hidden: %d %v", total, err)
		}
		if refs, err := r.Search(ctx, []string{"490154203237518"}); err != nil || len(refs) != 1 {
			t.Fatalf("expired private search: %+v %v", refs, err)
		}
		if states, err := r.ListStates(ctx, []string{p.DeviceID}); err != nil || len(states) != 1 {
			t.Fatalf("expired private summary: %+v %v", states, err)
		}
	}
	assertPublic()
	owned, err := r.FindOwned(ctx, p, p.DeviceID)
	if err != nil || owned.Visibility != "public" {
		t.Fatalf("effective owner visibility: %+v %v", owned, err)
	}
	if _, err = u.SetVisibility(ctx, p, p.DeviceID, "private"); !errors.Is(err, entity.ErrVisibilityLocked) {
		t.Fatalf("reclaimable device became private: %v", err)
	}
	nextTokens, err := a.Refresh(ctx, entity.RefreshRequest{RefreshToken: tokens.RefreshToken})
	if err != nil {
		t.Fatal(err)
	}
	p, err = a.Authenticate(ctx, nextTokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate, err := u.Ingest(ctx, p, p.DeviceID, input); err != nil || !duplicate.Duplicate {
		t.Fatalf("exact retry: %+v %v", duplicate, err)
	}
	assertPublic()
	payload.Sequence, payload.PreviousHash = 2, &first.PayloadHash
	next := signedMeasurement(t, key, payload)
	invalid := next
	invalid.SignatureDER = "MAYCAQECAQE="
	if _, err = u.Ingest(ctx, p, p.DeviceID, invalid); !errors.Is(err, entity.ErrMeasurementSignature) {
		t.Fatalf("invalid signature changed visibility: %v", err)
	}
	assertPublic()
	if _, err = u.Ingest(ctx, p, p.DeviceID, next); err != nil {
		t.Fatal(err)
	}
	owned, err = r.FindOwned(ctx, p, p.DeviceID)
	if err != nil || owned.Visibility != "private" {
		t.Fatalf("resumption did not apply holder setting: %+v %v", owned, err)
	}
	if _, err = r.FindState(ctx, p.DeviceID, 100, 0); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("resumed private data exposed: %v", err)
	}
	if err = u.Revoke(ctx, p, p.DeviceID); err != nil {
		t.Fatal(err)
	}
	if state, err := r.FindState(ctx, p.DeviceID, 100, 0); err != nil || state.RecordCount != 2 || state.RegistrationStatus != "released" {
		t.Fatalf("released data hidden: %+v %v", state, err)
	}
	newTokens, err := a.Enroll(ctx, testutil.ReEnrollment(t, db, p.DeviceID))
	if err != nil || newTokens.DeviceID != p.DeviceID {
		t.Fatalf("transfer UUID: %+v %v", newTokens, err)
	}
	if _, err = r.FindState(ctx, p.DeviceID, 100, 0); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("new holder not private: %v", err)
	}
	if _, err = u.SetVisibility(ctx, p, p.DeviceID, "public"); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("former holder changed visibility: %v", err)
	}
}

func TestVisibilityExactBoundaryWithoutVerifiedLogs(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	a := usecase.NewAuthUsecase(authdb.NewRepository(db), testutil.Registration(t, db))
	tokens, err := a.Enroll(ctx, testutil.Enrollment(t, db, "490154203237518"))
	if err != nil {
		t.Fatal(err)
	}
	err = db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for _, test := range []struct {
			age     string
			visible bool
		}{{"interval '72 hours' - interval '1 microsecond'", false}, {"interval '72 hours'", true}, {"interval '72 hours' + interval '1 microsecond'", true}} {
			if _, err := tx.ExecContext(ctx, "UPDATE device_ownerships SET started_at=transaction_timestamp()-("+test.age+") WHERE device_id=? AND ended_at IS NULL", tokens.DeviceID); err != nil {
				return err
			}
			var visible bool
			if err := tx.NewRaw("SELECT EXISTS(SELECT 1 FROM public_devices WHERE id=?)", tokens.DeviceID).Scan(ctx, &visible); err != nil {
				return err
			}
			if visible != test.visible {
				t.Fatalf("at %s visible=%t want %t", test.age, visible, test.visible)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestVisibilityChangeCannotCrossHolderTransfer(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	r := NewRepository(db)
	a, _, old := credentials(t, db, "490154203237518")
	u := usecase.NewDeviceUsecase(r, testutil.Registration(t, db))
	if _, err := db.ExecContext(ctx, "UPDATE device_ownerships SET started_at=clock_timestamp()-interval '72 hours' WHERE device_id=? AND ended_at IS NULL", old.DeviceID); err != nil {
		t.Fatal(err)
	}
	enrollment := testutil.ReEnrollment(t, db, old.DeviceID)
	type registrationResult struct {
		tokens *entity.TokenPair
		err    error
	}
	start := make(chan struct{})
	registration := make(chan registrationResult, 1)
	update := make(chan error, 1)
	go func() {
		<-start
		tokens, err := a.Enroll(ctx, enrollment)
		registration <- registrationResult{tokens, err}
	}()
	go func() { <-start; _, err := u.SetVisibility(ctx, old, old.DeviceID, "public"); update <- err }()
	close(start)
	next := <-registration
	if next.err != nil {
		t.Fatal(next.err)
	}
	if err := <-update; err != nil && !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("racing visibility: %v", err)
	}
	p, err := a.Authenticate(ctx, next.tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := r.FindOwned(ctx, p, p.DeviceID)
	if err != nil || owned.Visibility != "private" {
		t.Fatalf("old holder changed new holder visibility: %+v %v", owned, err)
	}
	if _, err = r.FindState(ctx, p.DeviceID, 100, 0); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("new holder data exposed: %v", err)
	}
	if _, err = u.SetVisibility(ctx, old, old.DeviceID, "public"); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("former holder visibility: %v", err)
	}
}
