//go:build integration

package auth

import (
	"context"
	"errors"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/infrastructure/database/testutil"
	"smapass-back/internal/usecase"
	"sync"
	"testing"
)

func TestTokenRotationReplayRevocationAndExpiry(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	u := usecase.NewAuthUsecase(NewRepository(db), testutil.Registration(t, db))
	a, err := u.Enroll(ctx, testutil.Enrollment(t, db, "490154203237518"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := u.Authenticate(ctx, a.AccessToken)
	if err != nil || p.IMEI != a.IMEI || p.SessionID == "" {
		t.Fatalf("initial credentials: %v", err)
	}
	var plaintext bool
	if err = db.NewRaw("SELECT EXISTS(SELECT 1 FROM app_tokens WHERE access_hash=? OR refresh_hash=?)", []byte(a.AccessToken), []byte(a.RefreshToken)).Scan(ctx, &plaintext); err != nil || plaintext {
		t.Fatal("raw credential stored")
	}
	next, err := u.Refresh(ctx, entity.RefreshRequest{RefreshToken: a.RefreshToken})
	if err != nil {
		t.Fatal(err)
	}
	if next.AccessToken == a.AccessToken || next.RefreshToken == a.RefreshToken {
		t.Fatal("credentials not rotated")
	}
	if _, err = u.Authenticate(ctx, a.AccessToken); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("old access token: %v", err)
	}
	if _, err = u.Authenticate(ctx, next.AccessToken); err != nil {
		t.Fatal(err)
	}
	if _, err = u.Refresh(ctx, entity.RefreshRequest{RefreshToken: a.RefreshToken}); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("replay: %v", err)
	}
	if _, err = u.Authenticate(ctx, next.AccessToken); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("replay did not revoke replacement: %v", err)
	}
	if _, err = u.Refresh(ctx, entity.RefreshRequest{RefreshToken: next.RefreshToken}); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("replay family still refreshable: %v", err)
	}
	if err = u.Revoke(ctx, entity.RefreshRequest{RefreshToken: next.RefreshToken}); err != nil {
		t.Fatal(err)
	}
	if err = u.Revoke(ctx, entity.RefreshRequest{RefreshToken: entity.NewTokenMaterial().Refresh}); err != nil {
		t.Fatal(err)
	}
	// A second independent installation remains usable and can renew after access expiry.
	b, err := u.Enroll(ctx, testutil.Enrollment(t, db, "356938035643809"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE app_tokens SET created_at=clock_timestamp()-interval '1 day',access_expires_at=clock_timestamp()-interval '1 second' WHERE access_hash=?", entity.TokenDigest(b.AccessToken)); err != nil {
		t.Fatal(err)
	}
	if _, err = u.Authenticate(ctx, b.AccessToken); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("expired access: %v", err)
	}
	b, err = u.Refresh(ctx, entity.RefreshRequest{RefreshToken: b.RefreshToken})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE app_tokens SET created_at=clock_timestamp()-interval '40 days',access_expires_at=clock_timestamp()-interval '31 days',refresh_expires_at=clock_timestamp()-interval '1 day' WHERE access_hash=?", entity.TokenDigest(b.AccessToken)); err != nil {
		t.Fatal(err)
	}
	if _, err = u.Refresh(ctx, entity.RefreshRequest{RefreshToken: b.RefreshToken}); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("expired refresh: %v", err)
	}
}

func TestConcurrentRefreshDetectsReuse(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	u := usecase.NewAuthUsecase(NewRepository(db), testutil.Registration(t, db))
	a, err := u.Enroll(ctx, testutil.Enrollment(t, db, "490154203237518"))
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		pair *entity.TokenPair
		err  error
	}
	out := make(chan result, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := u.Refresh(ctx, entity.RefreshRequest{RefreshToken: a.RefreshToken})
			out <- result{p, err}
		}()
	}
	wg.Wait()
	close(out)
	success, denied := 0, 0
	var replacement *entity.TokenPair
	for r := range out {
		if r.err == nil {
			success++
			replacement = r.pair
		} else if errors.Is(r.err, entity.ErrUnauthenticated) {
			denied++
		} else {
			t.Fatal(r.err)
		}
	}
	if success != 1 || denied != 1 {
		t.Fatalf("refresh results: %d/%d", success, denied)
	}
	if _, err = u.Authenticate(ctx, replacement.AccessToken); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("reuse family active: %v", err)
	}
}
