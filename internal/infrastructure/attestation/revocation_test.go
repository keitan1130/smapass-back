package attestation

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"math/big"
	"net/http"
	"smapass-back/internal/domain/entity"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRevocationCacheAndFailClosed(t *testing.T) {
	calls := 0
	unavailable := false
	s := NewGoogleStatus()
	s.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != statusURL {
			t.Fatal("unexpected status endpoint")
		}
		if unavailable {
			return nil, errors.New("offline")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Cache-Control": []string{"public, max-age=86400"}}, Body: io.NopCloser(strings.NewReader(`{"entries":{"64":{"status":"REVOKED"},"65":{"status":"SUSPENDED"}}}`))}, nil
	})}
	good := []*x509.Certificate{{SerialNumber: big.NewInt(10)}}
	for range 2 {
		if err := s.Check(context.Background(), good); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("status list not cached")
	}
	for _, serial := range []int64{100, 101} {
		if err := s.Check(context.Background(), []*x509.Certificate{{SerialNumber: big.NewInt(serial)}}); !errors.Is(err, entity.ErrAttestationRejected) {
			t.Fatalf("revoked certificate accepted: %v", err)
		}
	}
	s.expires = time.Now().Add(-time.Second)
	unavailable = true
	if err := s.Check(context.Background(), good); !errors.Is(err, entity.ErrAttestationUnavailable) {
		t.Fatalf("stale status used during outage: %v", err)
	}
}

func TestMalformedRevocationResponses(t *testing.T) {
	for _, body := range []string{`{}`, `{"entries":null}`, `{"entries":{"10":{"status":"GOOD"}}}`, `not json`, strings.Repeat("x", (4<<20)+1)} {
		s := NewGoogleStatus()
		s.client = &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		if err := s.Check(context.Background(), []*x509.Certificate{{SerialNumber: big.NewInt(10)}}); !errors.Is(err, entity.ErrAttestationUnavailable) {
			t.Fatal("malformed status accepted")
		}
	}
}

func TestRevocationCacheRespectsAgeAndNoCache(t *testing.T) {
	for _, tc := range []struct {
		control, age string
		unavailable  bool
	}{
		{"public, max-age=86400", "86400", true},
		{"no-cache, max-age=3600", "0", false},
		{"no-store, max-age=3600", "0", false},
	} {
		calls := 0
		s := NewGoogleStatus()
		s.client = &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Header: http.Header{"Cache-Control": []string{tc.control}, "Age": []string{tc.age}}, Body: io.NopCloser(strings.NewReader(`{"entries":{}}`))}, nil
		})}
		for range 2 {
			err := s.Check(context.Background(), []*x509.Certificate{{SerialNumber: big.NewInt(10)}})
			if tc.unavailable {
				if !errors.Is(err, entity.ErrAttestationUnavailable) {
					t.Fatal("expired HTTP status list accepted")
				}
			} else if err != nil {
				t.Fatal(err)
			}
		}
		if calls != 2 {
			t.Fatal("no-cache or expired status was reused")
		}
	}
}
