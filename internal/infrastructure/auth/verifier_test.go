package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	requestauth "smapass-back/internal/interface/auth"
)

func TestAccessTokenValidation(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), "smapass-auth", "smapass-back")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, issuer, audience, subject string
		expired, missingExpiry          bool
		status                          int
	}{
		{"valid", "smapass-auth", "smapass-back", "alice", false, false, 200},
		{"wrong issuer", "other", "smapass-back", "alice", false, false, 401},
		{"wrong audience", "smapass-auth", "other", "alice", false, false, 401},
		{"missing subject", "smapass-auth", "smapass-back", "", false, false, 401},
		{"expired", "smapass-auth", "smapass-back", "alice", true, false, 401},
		{"missing expiry", "smapass-auth", "smapass-back", "alice", false, true, 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := jwt.RegisteredClaims{Issuer: test.issuer, Audience: jwt.ClaimStrings{test.audience}, Subject: test.subject}
			if !test.missingExpiry {
				expiration := time.Now().Add(time.Hour)
				if test.expired {
					expiration = time.Now().Add(-time.Hour)
				}
				claims.ExpiresAt = jwt.NewNumericDate(expiration)
			}
			token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			app := echo.New()
			app.GET("/private", func(c *echo.Context) error {
				p, ok := requestauth.Principal(c.Request().Context())
				if !ok || p.Subject != "alice" {
					t.Fatal("unverified principal")
				}
				return c.NoContent(200)
			}, verifier.Middleware)
			req := httptest.NewRequest("GET", "/private", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()
			app.ServeHTTP(response, req)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	// An RSA public key must never be usable as an HMAC authentication secret.
	bad, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"iss": "smapass-auth", "aud": "smapass-back", "sub": "alice", "exp": time.Now().Add(time.Hour).Unix()}).SignedString(der)
	if err != nil {
		t.Fatal(err)
	}
	app := echo.New()
	app.GET("/private", func(c *echo.Context) error { t.Fatal("wrong algorithm accepted"); return nil }, verifier.Middleware)
	req := httptest.NewRequest("GET", "/private", nil)
	req.Header.Set("Authorization", "Bearer "+bad)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, req)
	if response.Code != 401 {
		t.Fatalf("algorithm-confused status=%d", response.Code)
	}
}

func TestUnconfiguredAuthenticationRejectsWrites(t *testing.T) {
	var verifier *Verifier
	app := echo.New()
	app.POST("/private", func(c *echo.Context) error { t.Fatal("authentication bypass"); return nil }, verifier.Middleware)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, httptest.NewRequest("POST", "/private", nil))
	if response.Code != 503 {
		t.Fatalf("status=%d", response.Code)
	}
	if _, err := NewVerifier("", "issuer", "audience"); err == nil {
		t.Fatal("partial auth configuration accepted")
	}
}
