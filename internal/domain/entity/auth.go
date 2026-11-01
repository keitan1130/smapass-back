package entity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

const (
	DeviceIssuer    = "smapass-device"
	AccessLifetime  = 15 * time.Minute
	RefreshLifetime = 30 * 24 * time.Hour
)

var ErrUnauthenticated = errors.New("invalid or expired credentials")

type EnrollDevice struct {
	IMEI string `json:"imei"`
}
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}
type TokenPair struct {
	IMEI             string    `json:"imei"`
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	TokenType        string    `json:"token_type"`
	ExpiresIn        int       `json:"expires_in"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

// TokenMaterial is returned once to the app. Only its digests may be persisted.
type TokenMaterial struct{ Access, Refresh string }

func NewTokenMaterial() TokenMaterial {
	return TokenMaterial{Access: "sp_at_" + randomToken(), Refresh: "sp_rt_" + randomToken()}
}

func randomToken() string {
	var value [32]byte
	_, _ = rand.Read(value[:]) // crypto/rand.Read is guaranteed to fill the buffer or terminate.
	return base64.RawURLEncoding.EncodeToString(value[:])
}

func ValidToken(value, kind string) bool {
	if len(value) != 49 || !strings.HasPrefix(value, "sp_"+kind+"_") {
		return false
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(value[6:])
	return err == nil && len(data) == 32
}

func TokenDigest(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	return digest[:]
}
