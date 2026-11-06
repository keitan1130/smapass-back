package entity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

func TestSharedMeasurementSignatureVector(t *testing.T) {
	data, err := os.ReadFile("../../../docs/examples/measurement-vector.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		CanonicalPayload string             `json:"canonical_payload"`
		SignatureDER     string             `json:"signature_der"`
		PayloadHash      string             `json:"payload_hash"`
		PublicKey        string             `json:"public_key"`
		Payload          MeasurementPayload `json:"payload"`
	}
	if err := json.Unmarshal(data, &vector); err != nil {
		t.Fatal(err)
	}
	log, err := PrepareMeasurement(SubmitMeasurement{CanonicalPayload: vector.CanonicalPayload, SignatureDER: vector.SignatureDER}, vector.Payload.CollectedAt)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(log.Hash[:]) != vector.PayloadHash {
		t.Fatal("shared SHA-256 vector mismatch")
	}
	if err := log.Verify(vector.PublicKey); err != nil {
		t.Fatal(err)
	}
}

func TestSharedJCSVectors(t *testing.T) {
	data, err := os.ReadFile("../../../docs/examples/jcs-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Vectors []struct {
			Input     string
			Canonical string
		}
		Rejected []string
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, vector := range fixture.Vectors {
		canonical, err := jsoncanonicalizer.Transform([]byte(vector.Input))
		if err != nil || string(canonical) != vector.Canonical {
			t.Fatalf("JCS vector: got %s err=%v want %s", canonical, err, vector.Canonical)
		}
	}
	for _, input := range fixture.Rejected {
		if _, err := jsoncanonicalizer.Transform([]byte(input)); err == nil {
			t.Fatalf("JCS accepted invalid JSON: %s", input)
		}
	}
}

func testMeasurementPayload() MeasurementPayload {
	return MeasurementPayload{SchemaVersion: 1, DeviceID: "22222222-2222-4222-8222-222222222222", KeyID: "11111111-1111-4111-8111-111111111111",
		Sequence: 1, Type: "status", CollectedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Status: &StatusMeasurement{OSVersion: "16", SDKInt: 36}}
}

func canonicalMeasurement(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	data, err = jsoncanonicalizer.Transform(data)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestMeasurementSignatureCoversEnvelopeAndUnknownsStayNull(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := testMeasurementPayload()
	canonical := canonicalMeasurement(t, payload)
	hash := sha256.Sum256([]byte(canonical))
	signature, err := ecdsa.SignASN1(rand.Reader, key, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	input := SubmitMeasurement{CanonicalPayload: canonical, SignatureDER: base64.StdEncoding.EncodeToString(signature)}
	log, err := PrepareMeasurement(input, payload.CollectedAt)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	public := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded}))
	if err := log.Verify(public); err != nil {
		t.Fatal(err)
	}
	if log.CanonicalPayload != canonical || log.Hash != hash || log.Payload.Status.BatteryTempC != nil || log.Payload.Status.HadAbnormalExit != nil {
		t.Fatal("original bytes, hash or unknown measurements changed")
	}
	payload.Status.OSVersion = "17"
	input.CanonicalPayload = canonicalMeasurement(t, payload)
	changed, err := PrepareMeasurement(input, payload.CollectedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := changed.Verify(public); !errors.Is(err, ErrMeasurementSignature) {
		t.Fatalf("tampered measurement accepted: %v", err)
	}
	if err := log.Verify("not a key"); !errors.Is(err, ErrMeasurementSignature) {
		t.Fatal("invalid key accepted")
	}
}

func TestMeasurementRejectsAmbiguousPrivateAndInvalidData(t *testing.T) {
	base := canonicalMeasurement(t, testMeasurementPayload())
	// A structurally sized DER placeholder is enough here: signature verification
	// is separate and no invalid payload may reach the repository.
	sig := base64.StdEncoding.EncodeToString([]byte{0x30, 6, 2, 1, 1, 2, 1, 1})
	tests := map[string]string{
		"noncanonical":         " " + base,
		"unsupported version":  strings.Replace(base, `"schema_version":1`, `"schema_version":2`, 1),
		"IMEI instead of UUID": strings.Replace(base, `"device_id":"22222222-2222-4222-8222-222222222222"`, `"imei":"490154203237518"`, 1),
		"IMEI injection":       strings.Replace(base, `"key_id":`, `"imei":"490154203237518","key_id":`, 1),
		"invalid device UUID":  strings.Replace(base, "22222222-2222-4222-8222-222222222222", "490154203237518", 1),
		"zero device UUID":     strings.Replace(base, "22222222-2222-4222-8222-222222222222", "00000000-0000-0000-0000-000000000000", 1),
		"duplicate envelope":   strings.Replace(base, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1),
		"duplicate nested":     strings.Replace(base, `"sdk_int":36`, `"sdk_int":36,"sdk_int":36`, 1),
		"private member":       strings.Replace(base, `"schema_version":1`, `"owner_subject":"private","schema_version":1`, 1),
		"missing null":         strings.Replace(base, `"had_abnormal_exit":null,`, "", 1),
		"wrong case":           strings.Replace(base, `"device_id"`, `"DEVICE_ID"`, 1),
		"invalid unicode":      strings.Replace(base, `"os_version":"16"`, `"os_version":"\ud800"`, 1),
		"invalid utf8":         strings.Replace(base, `"os_version":"16"`, `"os_version":"`+string([]byte{0xff})+`"`, 1),
		"unknown type":         strings.Replace(base, `"type":"status"`, `"type":"inspection"`, 1),
		"precision loss":       strings.Replace(base, `"battery_temp_c":null`, `"battery_temp_c":32.34`, 1),
		"overflow":             strings.Replace(base, `"battery_cycle_count":null`, `"battery_cycle_count":2147483648`, 1),
		"invalid first hash":   strings.Replace(base, `"previous_hash":null`, `"previous_hash":"`+strings.Repeat("a", 64)+`"`, 1),
		"unsafe sequence":      strings.Replace(base, `"sequence":1`, `"sequence":9007199254740992`, 1),
		"submicrosecond":       strings.Replace(base, `2026-01-01T00:00:00Z`, `2026-01-01T00:00:00.000000001Z`, 1),
		"future time":          strings.Replace(base, `2026-01-01T00:00:00Z`, `2026-01-01T00:06:00Z`, 1),
		"oversized":            strings.Repeat("x", MaxMeasurementBytes+1),
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := PrepareMeasurement(SubmitMeasurement{CanonicalPayload: payload, SignatureDER: sig}, testMeasurementPayload().CollectedAt); !errors.Is(err, ErrInvalidMeasurement) {
				t.Fatalf("invalid input accepted: %v", err)
			}
		})
	}
}

func TestMeasurementRequestRejectsDuplicateKeysAndTrailingDocuments(t *testing.T) {
	for _, input := range []string{`{"canonical_payload":"a","canonical_payload":"b","signature_der":"x"}`, `{} {}`, `{"owner_subject":"private"}`, strings.Repeat("[", 10) + strings.Repeat("]", 10)} {
		var request SubmitMeasurement
		if err := DecodeMeasurementJSON([]byte(input), &request); !errors.Is(err, ErrInvalidMeasurement) {
			t.Fatalf("accepted %q: %v", input, err)
		}
	}
}

func TestSpecMeasurementAcceptsOnlyPublicHardwareFields(t *testing.T) {
	p := testMeasurementPayload()
	p.Type, p.Status, p.Spec = "spec", nil, &SpecMeasurement{Manufacturer: "Google", Model: "Pixel 8"}
	sig := base64.StdEncoding.EncodeToString([]byte{0x30, 6, 2, 1, 1, 2, 1, 1})
	if _, err := PrepareMeasurement(SubmitMeasurement{CanonicalPayload: canonicalMeasurement(t, p), SignatureDER: sig}, p.CollectedAt); err != nil {
		t.Fatal(err)
	}
	unsafe := MaxJSONSafeInteger + 1
	p.Spec.RAMBytes = &unsafe
	if _, err := PrepareMeasurement(SubmitMeasurement{CanonicalPayload: canonicalMeasurement(t, p), SignatureDER: sig}, p.CollectedAt); !errors.Is(err, ErrInvalidMeasurement) {
		t.Fatalf("unsafe bytes accepted: %v", err)
	}
}
