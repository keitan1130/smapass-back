//go:build integration

package testutil

import (
	"context"
	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/infrastructure/attestation"
	"smapass-back/internal/infrastructure/database/keychallenge"
	"smapass-back/internal/testutil/attestationfixture"
	"smapass-back/internal/usecase"
	"testing"
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
func Rotation(t testing.TB, db *bun.DB, p entity.Principal, imeis ...string) entity.RegisterDevice {
	t.Helper()
	c, err := Registration(t, db).RotationChallenge(context.Background(), p, entity.RotationChallengeRequest{IMEIs: imeis})
	if err != nil {
		t.Fatal(err)
	}
	public, proof := Proof(t, c)
	return entity.RegisterDevice{IMEIs: imeis, PublicKey: public, Proof: proof}
}
