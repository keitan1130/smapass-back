package entity

import (
	"errors"
	"slices"
	"time"
)

const KeyPolicyVersion = "android-hardware-v1"
const KeyChallengeLifetime = 5 * time.Minute

var (
	ErrAttestationRejected    = errors.New("key attestation or possession proof rejected")
	ErrAttestationUnavailable = errors.New("attestation verifier unavailable")
	ErrChallengeInvalid       = errors.New("registration challenge invalid, expired or already used")
)

type KeyProof struct {
	ChallengeID      string   `json:"challenge_id"`
	CertificateChain []string `json:"certificate_chain"`
	SignatureDER     string   `json:"signature_der"`
}

type KeyChallenge struct {
	ID                   string    `json:"id"`
	AttestationChallenge string    `json:"attestation_challenge"`
	SigningPayload       string    `json:"signing_payload"`
	ExpiresAt            time.Time `json:"expires_at"`
}

type EnrollmentChallengeRequest struct {
	IMEI string `json:"imei"`
}
type RotationChallengeRequest struct {
	IMEIs []string `json:"imeis"`
}

// ChallengeContext and VerifiedKey are internal; they must never be bound from JSON.
type ChallengeContext struct {
	ID, IMEI, Purpose, InstallationID string
	IMEIs                             []string
	Nonce, Payload                    []byte
	ExpiresAt                         time.Time
	ConsumedAt                        *time.Time
}

type VerifiedKey struct {
	PolicyVersion    string    `json:"policy_version"`
	ChallengeID      string    `json:"challenge_id"`
	VerifiedAt       time.Time `json:"verified_at"`
	ValidUntil       time.Time `json:"valid_until"`
	SecurityLevel    int       `json:"security_level"`
	PackageName      string    `json:"package_name"`
	SigningDigests   []string  `json:"signing_digests"`
	OSVersion        int       `json:"os_version"`
	OSPatchLevel     int       `json:"os_patch_level"`
	IMEIAttested     bool      `json:"imei_attested"`
	CertificateChain []string  `json:"certificate_chain"`
	SignatureDER     string    `json:"signature_der"`
}

func NormalizeIMEIs(values []string) ([]string, error) {
	if len(values) < 1 || len(values) > 4 {
		return nil, ErrInvalidDevice
	}
	unique := make([]string, 0, len(values))
	for _, v := range values {
		imei, err := NormalizeIMEI(v)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(unique, imei) {
			unique = append(unique, imei)
		}
	}
	slices.Sort(unique)
	return unique, nil
}
