package entity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"time"
)

const DeviceReclaimAfter = 3 * 24 * time.Hour

func DeviceReclaimableAt(registeredAt time.Time, lastVerifiedLogAt *time.Time) time.Time {
	if lastVerifiedLogAt != nil {
		return lastVerifiedLogAt.Add(DeviceReclaimAfter)
	}
	return registeredAt.Add(DeviceReclaimAfter)
}

var (
	ErrInvalidDevice  = errors.New("invalid device registration")
	ErrDeviceInUse    = errors.New("device registration is still active")
	ErrDeviceConflict = errors.New("device identifiers or key conflict")
)

// Principal is supplied only by the verified authentication adapter.
type Principal struct{ Issuer, Subject, SessionID, DeviceID string }

type RegisterDevice struct {
	DeviceID  string       `json:"device_id"`
	PublicKey string       `json:"public_key"`
	Proof     KeyProof     `json:"proof"`
	Verified  *VerifiedKey `json:"-"`
}

type Device struct {
	DeviceID          string     `json:"device_id"`
	OwnershipID       int64      `json:"ownership_id"`
	RegisteredAt      time.Time  `json:"registered_at"`
	LastVerifiedLogAt *time.Time `json:"last_verified_log_at"`
	ReclaimableAt     time.Time  `json:"reclaimable_at"`
	KeyID             string     `json:"key_id"`
	KeyAttested       bool       `json:"key_attested"`
	NextSequence      int64      `json:"next_sequence"`
	PreviousHash      *string    `json:"previous_hash"`
	IMEIAttested      bool       `json:"imei_attested"`
}

func NormalizeIMEI(input string) (string, error) {
	value := strings.TrimSpace(input)
	if len(value) != 15 || value == "000000000000000" {
		return "", ErrInvalidDevice
	}
	total := 0
	for i, char := range []byte(value) {
		if char < '0' || char > '9' {
			return "", ErrInvalidDevice
		}
		digit := int(char - '0')
		if i%2 == 1 {
			digit *= 2
			digit = digit/10 + digit%10
		}
		total += digit
	}
	if total%10 != 0 {
		return "", ErrInvalidDevice
	}
	return value, nil
}

func (r *RegisterDevice) Normalize() error {
	id, err := NormalizeDeviceID(r.DeviceID)
	if err != nil {
		return err
	}
	r.DeviceID = id
	key, err := NormalizePublicKey(r.PublicKey)
	if err != nil {
		return err
	}
	r.PublicKey = key
	return nil
}

func NormalizePublicKey(value string) (string, error) {
	block, rest := pem.Decode([]byte(value))
	if block == nil || block.Type != "PUBLIC KEY" || len(strings.TrimSpace(string(rest))) != 0 {
		return "", ErrInvalidDevice
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", ErrInvalidDevice
	}
	public, ok := key.(*ecdsa.PublicKey)
	if !ok || public.Curve != elliptic.P256() {
		return "", ErrInvalidDevice
	}
	encoded, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		return "", ErrInvalidDevice
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded})), nil
}

// DeviceState is public lifetime state, independent of the current registrant.
// Missing observations remain null; history spans every registration period.
type DeviceState struct {
	ID                 string        `json:"id"`
	RegistrationStatus string        `json:"registration_status"`
	RecordCount        int           `json:"record_count"`
	FirstCollectedAt   *time.Time    `json:"first_collected_at"`
	LastCollectedAt    *time.Time    `json:"last_collected_at"`
	Spec               *DeviceSpec   `json:"spec"`
	Status             *StatusRecord `json:"status"`
	IsDemo             bool          `json:"is_demo"`
	History            DeviceHistory `json:"history"`
}

// DeviceHistory is the paginated lifetime measurement history.
type DeviceHistory struct {
	Items  []StatusRecord `json:"items"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

// DeviceSummary omits history for bookmark lists and comparisons.
type DeviceSummary struct {
	ID                 string        `json:"id"`
	RegistrationStatus string        `json:"registration_status"`
	RecordCount        int           `json:"record_count"`
	FirstCollectedAt   *time.Time    `json:"first_collected_at"`
	LastCollectedAt    *time.Time    `json:"last_collected_at"`
	Spec               *DeviceSpec   `json:"spec"`
	Status             *StatusRecord `json:"status"`
	IsDemo             bool          `json:"is_demo"`
}

// DeviceReference is returned only by IMEI search.
type DeviceReference struct {
	IMEI string `json:"imei"`
	ID   string `json:"id"`
}
