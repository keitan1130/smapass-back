// Package attestation verifies a deliberately strict Android hardware-key policy.
package attestation

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"smapass-back/internal/domain/entity"
)

var keyDescriptionOID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 11129, 2, 1, 17}

//go:embed google_roots.json
var googleRootJSON []byte

type Policy struct {
	PackageName    string
	SigningDigests []string
	RequireIMEI    bool
}
type RevocationChecker interface {
	Check(context.Context, []*x509.Certificate) error
}
type Verifier struct {
	policy Policy
	roots  *x509.CertPool
	status RevocationChecker
	now    func() time.Time
}

func NewVerifier(policy Policy, status RevocationChecker) (*Verifier, error) {
	if policy.PackageName == "" && len(policy.SigningDigests) == 0 {
		return &Verifier{}, nil
	}
	if !regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)+$`).MatchString(policy.PackageName) || len(policy.SigningDigests) == 0 {
		return nil, fmt.Errorf("ATTESTATION_PACKAGE_NAME and ATTESTATION_SIGNING_SHA256 must both be configured")
	}
	for i, digest := range policy.SigningDigests {
		digest = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(digest), ":", ""))
		data, err := hex.DecodeString(digest)
		if err != nil || len(data) != 32 {
			return nil, fmt.Errorf("ATTESTATION_SIGNING_SHA256 must contain SHA-256 certificate fingerprints")
		}
		policy.SigningDigests[i] = digest
	}
	var roots []string
	if err := json.Unmarshal(googleRootJSON, &roots); err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	for _, root := range roots {
		if !pool.AppendCertsFromPEM([]byte(root)) {
			return nil, fmt.Errorf("invalid embedded Android attestation root")
		}
	}
	if status == nil {
		return nil, fmt.Errorf("revocation checker is required")
	}
	return &Verifier{policy: policy, roots: pool, status: status, now: time.Now}, nil
}

func (v *Verifier) Ready() bool { return v != nil && v.roots != nil && v.status != nil }

func (v *Verifier) Verify(ctx context.Context, publicPEM string, proof entity.KeyProof, challenge *entity.ChallengeContext) (*entity.VerifiedKey, error) {
	if !v.Ready() {
		return nil, entity.ErrAttestationUnavailable
	}
	if challenge == nil || len(proof.CertificateChain) < 2 || len(proof.CertificateChain) > 8 {
		return nil, entity.ErrAttestationRejected
	}
	var certs []*x509.Certificate
	for _, encoded := range proof.CertificateChain {
		if len(encoded) > 11000 {
			return nil, entity.ErrAttestationRejected
		}
		der, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || len(der) > 8192 {
			return nil, entity.ErrAttestationRejected
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, entity.ErrAttestationRejected
		}
		certs = append(certs, cert)
	}
	leaf := certs[0]
	if leaf.IsCA || (leaf.KeyUsage != 0 && leaf.KeyUsage&x509.KeyUsageDigitalSignature == 0) {
		return nil, entity.ErrAttestationRejected
	}
	block, _ := pem.Decode([]byte(publicPEM))
	if block == nil || !bytes.Equal(block.Bytes, leaf.RawSubjectPublicKeyInfo) {
		return nil, entity.ErrAttestationRejected
	}
	key, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, entity.ErrAttestationRejected
	}
	// Only a chain supplied leaf-first in signing order is accepted. Trust anchors
	// come exclusively from Google; the client's final certificate adds no trust.
	for i := 0; i < len(certs)-1; i++ {
		if err := certs[i].CheckSignatureFrom(certs[i+1]); err != nil {
			return nil, entity.ErrAttestationRejected
		}
	}
	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}
	now := v.now()
	paths, err := leaf.Verify(x509.VerifyOptions{Roots: v.roots, Intermediates: intermediates, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
	if err != nil || len(paths) == 0 {
		return nil, entity.ErrAttestationRejected
	}
	path := paths[0]
	// The attested public key must itself be the first attestation extension
	// encountered from the trust anchor; appended/forged leaf attestations fail.
	var extension []byte
	for i := len(path) - 1; i >= 0; i-- {
		for _, ext := range path[i].Extensions {
			if ext.Id.Equal(keyDescriptionOID) {
				if extension != nil || i != 0 {
					return nil, entity.ErrAttestationRejected
				}
				extension = ext.Value
			}
		}
	}
	if extension == nil {
		return nil, entity.ErrAttestationRejected
	}
	var desc keyDescription
	if err := unmarshal(extension, &desc); err != nil {
		return nil, entity.ErrAttestationRejected
	}
	if desc.AttestationVersion < 2 || desc.KeymasterVersion < 3 || !hardwareLevel(int(desc.AttestationSecurityLevel)) || !hardwareLevel(int(desc.KeymasterSecurityLevel)) || !bytes.Equal(desc.Challenge, challenge.Nonce) {
		return nil, entity.ErrAttestationRejected
	}
	sw, err := parseList(desc.Software)
	if err != nil {
		return nil, entity.ErrAttestationRejected
	}
	hw, err := parseList(desc.Hardware)
	if err != nil {
		return nil, entity.ErrAttestationRejected
	}
	required := slices.Clone(challenge.IMEIs)
	if !slices.Contains(required, challenge.IMEI) {
		required = append(required, challenge.IMEI)
	}
	result, err := v.authorizations(sw, hw, required)
	if err != nil {
		return nil, entity.ErrAttestationRejected
	}
	if len(proof.SignatureDER) > 112 {
		return nil, entity.ErrAttestationRejected
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(proof.SignatureDER)
	digest := sha256.Sum256(challenge.Payload)
	if err != nil || !ecdsa.VerifyASN1(key, digest[:], signature) {
		return nil, entity.ErrAttestationRejected
	}
	if err := v.status.Check(ctx, path); err != nil {
		return nil, err
	}
	result.ValidUntil = path[0].NotAfter
	for _, cert := range path[1:] {
		if cert.NotAfter.Before(result.ValidUntil) {
			result.ValidUntil = cert.NotAfter
		}
	}
	if !v.now().Before(result.ValidUntil) {
		return nil, entity.ErrAttestationRejected
	}
	result.PolicyVersion = entity.KeyPolicyVersion
	result.ChallengeID = challenge.ID
	result.VerifiedAt = now
	result.SecurityLevel = int(desc.KeymasterSecurityLevel)
	result.PackageName = v.policy.PackageName
	result.CertificateChain = slices.Clone(proof.CertificateChain)
	result.SignatureDER = proof.SignatureDER
	return result, nil
}

func hardwareLevel(value int) bool { return value == 1 || value == 2 }

func (v *Verifier) authorizations(sw, hw authorizationList, imeis []string) (*entity.VerifiedKey, error) {
	for _, tag := range []int{1, 2, 3, 5, 10, 702, 704, 705, 706} {
		if _, shadow := sw[tag]; shadow {
			return nil, entity.ErrAttestationRejected
		}
	}
	if _, exists := hw[709]; exists {
		return nil, entity.ErrAttestationRejected
	}
	for _, list := range []authorizationList{sw, hw} {
		if _, exists := list[600]; exists {
			return nil, entity.ErrAttestationRejected
		}
	}
	for tag, expected := range map[int]int{2: 3, 3: 256, 10: 1, 702: 0} {
		value, err := hw.integer(tag)
		if err != nil || value != expected {
			return nil, entity.ErrAttestationRejected
		}
	}
	purposes, err := hw.integers(1)
	if err != nil || !slices.Contains(purposes, 2) {
		return nil, entity.ErrAttestationRejected
	}
	for _, purpose := range purposes {
		if purpose != 2 && purpose != 3 {
			return nil, entity.ErrAttestationRejected
		}
	}
	digests, err := hw.integers(5)
	if err != nil || len(digests) != 1 || digests[0] != 4 {
		return nil, entity.ErrAttestationRejected
	}
	var boot rootOfTrust
	root, ok := hw[704]
	if !ok || unmarshal(root.FullBytes, &boot) != nil || !boot.DeviceLocked || boot.VerifiedBootState != 0 || len(boot.VerifiedBootKey) < 32 {
		return nil, entity.ErrAttestationRejected
	}
	osVersion, err := hw.integer(705)
	if err != nil || osVersion < 80000 {
		return nil, entity.ErrAttestationRejected
	}
	patch, err := hw.integer(706)
	if err != nil || patch < 201701 || patch > 999912 || patch%100 < 1 || patch%100 > 12 {
		return nil, entity.ErrAttestationRejected
	}
	encoded, err := sw.octets(709)
	if err != nil {
		return nil, err
	}
	var app applicationID
	if unmarshal(encoded, &app) != nil || len(app.Packages) != 1 || string(app.Packages[0].Name) != v.policy.PackageName || app.Packages[0].Version < 0 || len(app.Digests) < 1 || len(app.Digests) > 8 {
		return nil, entity.ErrAttestationRejected
	}
	result := &entity.VerifiedKey{OSVersion: osVersion, OSPatchLevel: patch}
	for _, digest := range app.Digests {
		value := hex.EncodeToString(digest)
		if len(digest) != 32 || !slices.Contains(v.policy.SigningDigests, value) {
			return nil, entity.ErrAttestationRejected
		}
		result.SigningDigests = append(result.SigningDigests, value)
	}
	var attested []string
	for _, tag := range []int{714, 723} {
		if _, exists := sw[tag]; exists {
			return nil, entity.ErrAttestationRejected
		}
		if _, exists := hw[tag]; exists {
			data, err := hw.octets(tag)
			if err != nil {
				return nil, err
			}
			imei, err := entity.NormalizeIMEI(string(data))
			if err != nil {
				return nil, err
			}
			attested = append(attested, imei)
		}
	}
	if len(attested) > 0 {
		for _, imei := range imeis {
			if !slices.Contains(attested, imei) {
				return nil, entity.ErrAttestationRejected
			}
		}
		result.IMEIAttested = true
	}
	if v.policy.RequireIMEI && !result.IMEIAttested {
		return nil, entity.ErrAttestationRejected
	}
	return result, nil
}
