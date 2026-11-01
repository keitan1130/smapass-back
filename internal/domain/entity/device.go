package entity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"slices"
	"strings"
	"time"
)

const DeviceReclaimAfter = 15 * 24 * time.Hour

var (
	ErrInvalidDevice  = errors.New("invalid device registration")
	ErrDeviceInUse    = errors.New("device registration is still active")
	ErrDeviceConflict = errors.New("device identifiers or key conflict")
)

// Principal is supplied only by the verified authentication adapter.
type Principal struct{ Issuer, Subject, SessionID, IMEI string }

type RegisterDevice struct {
	IMEIs     []string `json:"imeis"`
	PublicKey string   `json:"public_key"`
}

type Device struct {
	IMEI              string     `json:"imei"`
	IMEIs             []string   `json:"imeis"`
	OwnershipID       int64      `json:"ownership_id"`
	RegisteredAt      time.Time  `json:"registered_at"`
	LastVerifiedLogAt *time.Time `json:"last_verified_log_at"`
	ReclaimableAt     time.Time  `json:"reclaimable_at"`
	KeyID             string     `json:"key_id"`
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
	if len(r.IMEIs) < 1 || len(r.IMEIs) > 4 {
		return ErrInvalidDevice
	}
	unique := make([]string, 0, len(r.IMEIs))
	for _, input := range r.IMEIs {
		imei, err := NormalizeIMEI(input)
		if err != nil {
			return err
		}
		if !slices.Contains(unique, imei) {
			unique = append(unique, imei)
		}
	}
	r.IMEIs = unique
	block, rest := pem.Decode([]byte(r.PublicKey))
	if block == nil || block.Type != "PUBLIC KEY" || len(strings.TrimSpace(string(rest))) != 0 {
		return ErrInvalidDevice
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return ErrInvalidDevice
	}
	public, ok := key.(*ecdsa.PublicKey)
	if !ok || public.Curve != elliptic.P256() {
		return ErrInvalidDevice
	}
	encoded, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		return ErrInvalidDevice
	}
	r.PublicKey = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded}))
	return nil
}
