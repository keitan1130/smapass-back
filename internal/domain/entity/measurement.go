package entity

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/google/uuid"
)

const MeasurementCanonicalization = "RFC8785"
const MaxMeasurementBytes = 64 << 10
const MaxJSONSafeInteger int64 = 1<<53 - 1

var (
	ErrInvalidMeasurement   = errors.New("invalid measurement payload")
	ErrMeasurementSignature = errors.New("measurement signature or signing key rejected")
	ErrMeasurementConflict  = errors.New("measurement sequence or previous hash conflict")
)

type SubmitMeasurement struct {
	CanonicalPayload string `json:"canonical_payload"`
	SignatureDER     string `json:"signature_der"`
}

// MeasurementPayload is the complete public signing envelope. All fields,
// including null measurements, are required in the canonical representation.
type MeasurementPayload struct {
	SchemaVersion int                `json:"schema_version"`
	IMEI          string             `json:"imei"`
	KeyID         string             `json:"key_id"`
	Sequence      int64              `json:"sequence"`
	Type          string             `json:"type"`
	CollectedAt   time.Time          `json:"collected_at"`
	PreviousHash  *string            `json:"previous_hash"`
	Spec          *SpecMeasurement   `json:"spec"`
	Status        *StatusMeasurement `json:"status"`
}

type SpecMeasurement struct {
	Manufacturer     string   `json:"manufacturer"`
	Model            string   `json:"model"`
	SoC              *string  `json:"soc"`
	CPUCores         *int     `json:"cpu_cores"`
	RAMBytes         *int64   `json:"ram_bytes"`
	StorageBytes     *int64   `json:"storage_bytes"`
	ScreenSizeInch   *float64 `json:"screen_size_inch"`
	ScreenDensityDPI *int     `json:"screen_density_dpi"`
}

type StatusMeasurement struct {
	OSVersion            string   `json:"os_version"`
	SDKInt               int      `json:"sdk_int"`
	BatteryTempC         *float64 `json:"battery_temp_c"`
	BatteryCycleCount    *int     `json:"battery_cycle_count"`
	BatteryChargeMAh     *int     `json:"battery_charge_mah"`
	BatteryFullChargeMAh *int     `json:"battery_full_charge_mah"`
	BootCount            *int     `json:"boot_count"`
	HadAbnormalExit      *bool    `json:"had_abnormal_exit"`
	ThermalHeadroom      *float64 `json:"thermal_headroom"`
}

type MeasurementReceipt struct {
	IMEI              string    `json:"imei"`
	Sequence          int64     `json:"sequence"`
	PayloadHash       string    `json:"payload_hash"`
	ReceivedAt        time.Time `json:"received_at"`
	SignatureVerified bool      `json:"signature_verified"`
	Duplicate         bool      `json:"duplicate"`
}

// MeasurementLog is prepared from the signed bytes, never reconstructed from
// JSONB. The repository verifies the signature against the locked current key.
type MeasurementLog struct {
	Payload          MeasurementPayload
	CanonicalPayload string
	Hash             [32]byte
	Signature        []byte
}

// DecodeMeasurementJSON rejects duplicate members, excessive nesting, invalid
// UTF-8, unknown fields and multiple JSON documents before decoding typed data.
func DecodeMeasurementJSON(data []byte, dest any) error {
	if !utf8.Valid(data) {
		return ErrInvalidMeasurement
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 8 {
			return ErrInvalidMeasurement
		}
		token, err := d.Token()
		if err != nil {
			return ErrInvalidMeasurement
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					key, err := d.Token()
					name, ok := key.(string)
					if err != nil || !ok || seen[name] {
						return ErrInvalidMeasurement
					}
					seen[name] = true
					if err := value(depth + 1); err != nil {
						return err
					}
				}
			case '[':
				for d.More() {
					if err := value(depth + 1); err != nil {
						return err
					}
				}
			default:
				return ErrInvalidMeasurement
			}
			if _, err := d.Token(); err != nil {
				return ErrInvalidMeasurement
			}
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalidMeasurement
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dest); err != nil {
		return ErrInvalidMeasurement
	}
	return nil
}

func PrepareMeasurement(input SubmitMeasurement, now time.Time) (*MeasurementLog, error) {
	data := []byte(input.CanonicalPayload)
	if len(data) == 0 || len(data) > MaxMeasurementBytes {
		return nil, ErrInvalidMeasurement
	}
	var payload MeasurementPayload
	if err := DecodeMeasurementJSON(data, &payload); err != nil {
		return nil, err
	}
	canonical, err := jsoncanonicalizer.Transform(data)
	if err != nil || !bytes.Equal(data, canonical) {
		return nil, ErrInvalidMeasurement
	}
	// A typed round-trip enforces exact spelling, required fields, and the
	// public allowlist without losing the original bytes used for verification.
	typed, err := json.Marshal(payload)
	if err != nil {
		return nil, ErrInvalidMeasurement
	}
	typed, err = jsoncanonicalizer.Transform(typed)
	if err != nil || !bytes.Equal(data, typed) {
		return nil, ErrInvalidMeasurement
	}
	imei, err := NormalizeIMEI(payload.IMEI)
	if err != nil || imei != payload.IMEI {
		return nil, ErrInvalidMeasurement
	}
	id, err := uuid.Parse(payload.KeyID)
	if err != nil || id.String() != payload.KeyID || id == uuid.Nil {
		return nil, ErrInvalidMeasurement
	}
	if payload.SchemaVersion != 1 || payload.Sequence < 1 || payload.Sequence >= MaxJSONSafeInteger ||
		payload.CollectedAt.Before(time.Unix(0, 0)) || payload.CollectedAt.After(now.Add(5*time.Minute)) ||
		payload.CollectedAt.Nanosecond()%1000 != 0 || !strings.HasSuffix(payload.CollectedAt.Format(time.RFC3339Nano), "Z") {
		return nil, ErrInvalidMeasurement
	}
	if payload.Sequence == 1 {
		if payload.PreviousHash != nil {
			return nil, ErrInvalidMeasurement
		}
	} else if payload.PreviousHash == nil || !validHash(*payload.PreviousHash) {
		return nil, ErrInvalidMeasurement
	}
	switch payload.Type {
	case "spec":
		if payload.Spec == nil || payload.Status != nil || !payload.Spec.valid() {
			return nil, ErrInvalidMeasurement
		}
	case "status":
		if payload.Status == nil || payload.Spec != nil || !payload.Status.valid() {
			return nil, ErrInvalidMeasurement
		}
	default:
		return nil, ErrInvalidMeasurement
	}
	if len(input.SignatureDER) > 112 {
		return nil, ErrInvalidMeasurement
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(input.SignatureDER)
	if err != nil || len(signature) < 8 || len(signature) > 72 || base64.StdEncoding.EncodeToString(signature) != input.SignatureDER {
		return nil, ErrInvalidMeasurement
	}
	return &MeasurementLog{Payload: payload, CanonicalPayload: input.CanonicalPayload, Hash: sha256.Sum256(data), Signature: signature}, nil
}

func (l *MeasurementLog) Verify(publicPEM string) error {
	block, rest := pem.Decode([]byte(publicPEM))
	if block == nil || block.Type != "PUBLIC KEY" || strings.TrimSpace(string(rest)) != "" {
		return ErrMeasurementSignature
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return ErrMeasurementSignature
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() || !ecdsa.VerifyASN1(key, l.Hash[:], l.Signature) {
		return ErrMeasurementSignature
	}
	return nil
}

func validHash(s string) bool {
	decoded, err := hex.DecodeString(s)
	return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == s
}

func measurementText(s string, max int) bool {
	return len(s) > 0 && len(s) <= max && strings.TrimSpace(s) == s && !strings.ContainsFunc(s, unicode.IsControl)
}

func intRange(p *int, min, max int) bool { return p == nil || (*p >= min && *p <= max) }
func floatRange(p *float64, min, max float64) bool {
	return p == nil || (!math.IsNaN(*p) && !math.IsInf(*p, 0) && *p >= min && *p <= max)
}

func (s *SpecMeasurement) valid() bool {
	return measurementText(s.Manufacturer, 128) && measurementText(s.Model, 128) &&
		(s.SoC == nil || measurementText(*s.SoC, 128)) && intRange(s.CPUCores, 1, 32767) &&
		(s.RAMBytes == nil || (*s.RAMBytes > 0 && *s.RAMBytes <= MaxJSONSafeInteger)) &&
		(s.StorageBytes == nil || (*s.StorageBytes > 0 && *s.StorageBytes <= MaxJSONSafeInteger)) &&
		floatRange(s.ScreenSizeInch, 0.01, 99.99) && decimalPlaces(s.ScreenSizeInch, 2) && intRange(s.ScreenDensityDPI, 1, 32767)
}

func (s *StatusMeasurement) valid() bool {
	return measurementText(s.OSVersion, 64) && s.SDKInt > 0 && s.SDKInt <= 32767 &&
		floatRange(s.BatteryTempC, -9999.9, 9999.9) && decimalPlaces(s.BatteryTempC, 1) &&
		intRange(s.BatteryCycleCount, 0, math.MaxInt32) && intRange(s.BatteryChargeMAh, 0, math.MaxInt32) &&
		intRange(s.BatteryFullChargeMAh, 1, math.MaxInt32) && intRange(s.BootCount, 0, math.MaxInt32) &&
		floatRange(s.ThermalHeadroom, -math.MaxFloat64, math.MaxFloat64)
}

// NUMERIC projections must not silently round a signed measurement.
func decimalPlaces(p *float64, digits int) bool {
	if p == nil {
		return true
	}
	text := strconv.FormatFloat(*p, 'f', -1, 64)
	_, fraction, hasFraction := strings.Cut(text, ".")
	return !hasFraction || len(fraction) <= digits
}
