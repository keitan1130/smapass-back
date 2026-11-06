//go:build integration

package testutil

import (
	"context"
	"testing"

	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/infrastructure/attestation"
	"smapass-back/internal/infrastructure/database/keychallenge"
	"smapass-back/internal/testutil/attestationfixture"
	"smapass-back/internal/usecase"
)

func Registration(t testing.TB, db *bun.DB) *usecase.KeyRegistrationUsecase {
	t.Helper()
	v, err := attestation.NewFixtureVerifier()
	if err != nil {
		t.Fatal(err)
	}
	return usecase.NewKeyRegistrationUsecase(keychallenge.NewRepository(db), v)
}
func Proof(t testing.TB, c *entity.KeyChallenge) (string, entity.KeyProof) {
	t.Helper()
	a, err := attestationfixture.DefaultAuthority()
	if err != nil {
		t.Fatal(err)
	}
	public, proof, err := a.Proof(c, attestationfixture.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	return public, proof
}
func Enrollment(t testing.TB, db *bun.DB, imei string) entity.EnrollDevice {
	t.Helper()
	c, err := Registration(t, db).EnrollmentChallenge(context.Background(), entity.EnrollmentChallengeRequest{IMEI: imei})
	if err != nil {
		t.Fatal(err)
	}
	public, proof := Proof(t, c)
	return entity.EnrollDevice{IMEI: imei, PublicKey: public, Proof: proof}
}
func Rotation(t testing.TB, db *bun.DB, p entity.Principal, deviceID string) entity.RegisterDevice {
	t.Helper()
	c, err := Registration(t, db).RotationChallenge(context.Background(), p, entity.RotationChallengeRequest{DeviceID: deviceID})
	if err != nil {
		t.Fatal(err)
	}
	public, proof := Proof(t, c)
	return entity.RegisterDevice{DeviceID: deviceID, PublicKey: public, Proof: proof}
}

// IMEI resolves the private enrollment attribute in integration fixtures only.
func IMEI(t testing.TB, db *bun.DB, deviceID string) string {
	t.Helper()
	var imei string
	if err := db.NewRaw("SELECT imei FROM devices WHERE id=?", deviceID).Scan(context.Background(), &imei); err != nil {
		t.Fatal(err)
	}
	return imei
}

func ReEnrollment(t testing.TB, db *bun.DB, deviceID string) entity.EnrollDevice {
	t.Helper()
	return Enrollment(t, db, IMEI(t, db, deviceID))
}
