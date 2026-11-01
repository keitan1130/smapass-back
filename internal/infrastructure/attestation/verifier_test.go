package attestation

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/testutil/attestationfixture"
	"testing"
	"time"
)

type statusResult struct{ err error }

func (s statusResult) Check(context.Context, []*x509.Certificate) error { return s.err }

func fixtureVerifier(t *testing.T) (*Verifier, *attestationfixture.Authority, *entity.KeyChallenge, *entity.ChallengeContext) {
	t.Helper()
	a, err := attestationfixture.NewAuthority()
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(a.Root)
	v := &Verifier{policy: Policy{PackageName: attestationfixture.PackageName, SigningDigests: []string{attestationfixture.SigningDigest()}}, roots: pool, status: statusResult{}, now: time.Now}
	nonce := []byte("0123456789abcdef0123456789abcdef")
	payload := []byte(`{"protocol":"test-only signing payload","imei":"490154203237518"}`)
	c := &entity.KeyChallenge{ID: "11111111-1111-4111-8111-111111111111", AttestationChallenge: base64.StdEncoding.EncodeToString(nonce), SigningPayload: base64.StdEncoding.EncodeToString(payload), ExpiresAt: time.Now().Add(time.Minute)}
	ctx := &entity.ChallengeContext{ID: c.ID, IMEI: "490154203237518", IMEIs: []string{"490154203237518"}, Nonce: nonce, Payload: payload, ExpiresAt: c.ExpiresAt}
	return v, a, c, ctx
}

func TestAndroidHardwarePolicyAndPossession(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*attestationfixture.Options)
		bad    bool
	}{
		{"valid TEE", func(*attestationfixture.Options) {}, false},
		{"valid StrongBox", func(o *attestationfixture.Options) { o.Level = 2 }, false},
		{"software key", func(o *attestationfixture.Options) { o.Level = 0 }, true},
		{"unknown security level", func(o *attestationfixture.Options) { o.Level = 3 }, true},
		{"wrong application", func(o *attestationfixture.Options) { o.PackageName = "attacker.app" }, true},
		{"wrong signing certificate", func(o *attestationfixture.Options) { o.SigningDigest = make([]byte, 32) }, true},
		{"empty signing digest", func(o *attestationfixture.Options) { o.SigningDigest = nil }, true},
		{"unlocked bootloader", func(o *attestationfixture.Options) { o.Locked = false }, true},
		{"unverified boot", func(o *attestationfixture.Options) { o.BootState = 2 }, true},
		{"imported key", func(o *attestationfixture.Options) { o.Origin = 2 }, true},
		{"wrong attestation nonce", func(o *attestationfixture.Options) { o.ChallengeOverride = []byte("attacker nonce") }, true},
		{"duplicate authorization tag", func(o *attestationfixture.Options) { o.DuplicateTag = true }, true},
		{"malformed attestation", func(o *attestationfixture.Options) { o.RawExtension = []byte{0x30, 0} }, true},
		{"expired certificate", func(o *attestationfixture.Options) { o.NotAfter = time.Now().Add(-time.Minute) }, true},
		{"appended fake leaf attestation", func(o *attestationfixture.Options) { o.IntermediateAttestation = true }, true},
		{"attested matching IMEI", func(o *attestationfixture.Options) { o.AttestedIMEIs = []string{"490154203237518"} }, false},
		{"attested different IMEI", func(o *attestationfixture.Options) { o.AttestedIMEIs = []string{"356938035643809"} }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, a, c, challenge := fixtureVerifier(t)
			options := attestationfixture.DefaultOptions()
			tc.change(&options)
			public, proof, err := a.Proof(c, options)
			if err != nil {
				t.Fatal(err)
			}
			result, err := v.Verify(context.Background(), public, proof, challenge)
			if tc.bad {
				if !errors.Is(err, entity.ErrAttestationRejected) {
					t.Fatalf("policy accepted: %v", err)
				}
				return
			}
			if err != nil || result.PolicyVersion != entity.KeyPolicyVersion || result.IMEIAttested != (len(options.AttestedIMEIs) > 0) {
				t.Fatalf("valid attestation rejected: %+v %v", result, err)
			}
		})
	}
}

func TestTrustRootsPossessionAndIMEIPolicy(t *testing.T) {
	v, a, c, challenge := fixtureVerifier(t)
	public, proof, err := a.Proof(c, attestationfixture.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	production, err := NewVerifier(v.policy, statusResult{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = production.Verify(context.Background(), public, proof, challenge); !errors.Is(err, entity.ErrAttestationRejected) {
		t.Fatalf("client root gained trust: %v", err)
	}
	v.policy.RequireIMEI = true
	if _, err = v.Verify(context.Background(), public, proof, challenge); !errors.Is(err, entity.ErrAttestationRejected) {
		t.Fatalf("IMEI attestation missing: %v", err)
	}
	v.policy.RequireIMEI = false
	signature, _ := base64.StdEncoding.DecodeString(proof.SignatureDER)
	signature[len(signature)-1] ^= 1
	bad := proof
	bad.SignatureDER = base64.StdEncoding.EncodeToString(signature)
	if _, err = v.Verify(context.Background(), public, bad, challenge); !errors.Is(err, entity.ErrAttestationRejected) {
		t.Fatalf("bad signature accepted: %v", err)
	}
	otherPublic, _, err := a.Proof(c, attestationfixture.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.Verify(context.Background(), otherPublic, proof, challenge); !errors.Is(err, entity.ErrAttestationRejected) {
		t.Fatalf("swapped public key accepted: %v", err)
	}
	v.status = statusResult{entity.ErrAttestationRejected}
	if _, err = v.Verify(context.Background(), public, proof, challenge); !errors.Is(err, entity.ErrAttestationRejected) {
		t.Fatalf("revocation ignored: %v", err)
	}
	v.status = statusResult{entity.ErrAttestationUnavailable}
	if _, err = v.Verify(context.Background(), public, proof, challenge); !errors.Is(err, entity.ErrAttestationUnavailable) {
		t.Fatalf("outage accepted: %v", err)
	}
}

func TestUnconfiguredVerifierAndPolicy(t *testing.T) {
	v, err := NewVerifier(Policy{}, nil)
	if err != nil || v.Ready() {
		t.Fatalf("unconfigured verifier ready: %v", err)
	}
	if _, err = v.Verify(context.Background(), "", entity.KeyProof{}, nil); !errors.Is(err, entity.ErrAttestationUnavailable) {
		t.Fatalf("unconfigured verification: %v", err)
	}
	for _, policy := range []Policy{{PackageName: "single"}, {PackageName: "valid.app"}, {SigningDigests: []string{attestationfixture.SigningDigest()}}, {PackageName: "valid.app", SigningDigests: []string{"bad"}}} {
		if _, err := NewVerifier(policy, statusResult{}); err == nil {
			t.Fatal("invalid policy accepted")
		}
	}
	valid, err := NewVerifier(Policy{PackageName: "valid.app", SigningDigests: []string{attestationfixture.SigningDigest()}}, statusResult{})
	if err != nil || len(valid.roots.Subjects()) != 2 {
		t.Fatal("embedded Google roots unavailable")
	}
}

func TestHardwareIMEIAttestationIncludesCanonicalIMEI(t *testing.T) {
	v, a, c, challenge := fixtureVerifier(t)
	challenge.IMEIs = []string{"356938035643809"}
	options := attestationfixture.DefaultOptions()
	options.AttestedIMEIs = []string{"356938035643809"}
	public, proof, err := a.Proof(c, options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.Verify(context.Background(), public, proof, challenge); !errors.Is(err, entity.ErrAttestationRejected) {
		t.Fatalf("unattested canonical IMEI accepted: %v", err)
	}
	options.AttestedIMEIs = append(options.AttestedIMEIs, challenge.IMEI)
	public, proof, err = a.Proof(c, options)
	if err != nil {
		t.Fatal(err)
	}
	result, err := v.Verify(context.Background(), public, proof, challenge)
	if err != nil || !result.IMEIAttested {
		t.Fatalf("matching dual IMEIs rejected: %v", err)
	}
}

func FuzzAttestationDER(f *testing.F) {
	f.Add([]byte{0x30, 0})
	f.Add([]byte{})
	f.Add([]byte{0x30, 3, 0xa1, 1, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 65536 {
			return
		}
		var desc keyDescription
		if unmarshal(data, &desc) != nil {
			return
		}
		_, _ = parseList(desc.Software)
		_, _ = parseList(desc.Hardware)
	})
}
