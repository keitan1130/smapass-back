//go:build integration

package attestation

import (
	"context"
	"crypto/x509"
	"smapass-back/internal/testutil/attestationfixture"
	"time"
)

type fixtureStatus struct{}

func (fixtureStatus) Check(context.Context, []*x509.Certificate) error { return nil }

// NewFixtureVerifier is compiled only with integration tests. Production DI
// always uses NewVerifier and the embedded Google roots with live revocation checks.
func NewFixtureVerifier() (*Verifier, error) {
	a, err := attestationfixture.DefaultAuthority()
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(a.Root)
	return &Verifier{policy: Policy{PackageName: attestationfixture.PackageName, SigningDigests: []string{attestationfixture.SigningDigest()}}, roots: pool, status: fixtureStatus{}, now: time.Now}, nil
}
