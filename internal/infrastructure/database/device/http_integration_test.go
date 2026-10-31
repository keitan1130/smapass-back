//go:build integration

package device

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/infrastructure/auth"
	"smapass-back/internal/infrastructure/database/certificate"
	"smapass-back/internal/infrastructure/database/testutil"
	"smapass-back/internal/infrastructure/router"
	"smapass-back/internal/interface/controller"
	"smapass-back/internal/usecase"
)

func TestDeviceHTTPAuthenticationAndReclaim(t *testing.T) {
	db := testutil.NewDatabase(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	public := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)})
	verifier, err := auth.NewVerifier(string(public), "test-issuer", "smapass")
	if err != nil {
		t.Fatal(err)
	}
	app := router.New(
		controller.NewCertificateController(usecase.NewCertificateUsecase(certificate.NewRepository(db))),
		controller.NewHealthController(usecase.NewHealthUsecase(db)), []string{"http://localhost:3000"})
	router.AddDeviceRoutes(app, controller.NewDeviceController(usecase.NewDeviceUsecase(NewRepository(db))), verifier.Middleware)
	token := func(subject string) string {
		t.Helper()
		value, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
			Issuer: "test-issuer", Subject: subject, Audience: jwt.ClaimStrings{"smapass"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		}).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	alice, bob := token("alice"), token("bob")
	requestNumber := 0
	request := func(method, path, bearer string, body any, status int) []byte {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(encoded))
		requestNumber++
		r.RemoteAddr = fmt.Sprintf("192.0.2.%d:12345", requestNumber)
		r.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d, want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("device response must not be cached")
		}
		return w.Body.Bytes()
	}
	request(http.MethodGet, "/certificates/000000000094821", "", nil, 200)
	request(http.MethodGet, "/certificates/1234", "", nil, 400)
	registration := input(t, "490154203237518")
	request(http.MethodPost, "/api/devices", "", registration, 401)
	response := request(http.MethodPost, "/api/devices", alice, registration, 200)
	var first entity.Device
	if err := json.Unmarshal(response, &first); err != nil || first.IMEI != registration.IMEIs[0] {
		t.Fatalf("invalid registration response: %v", err)
	}
	lookup := map[string]string{"imei": first.IMEI}
	request(http.MethodPost, "/api/devices/lookup", alice, lookup, 200)
	request(http.MethodPost, "/api/devices/lookup", bob, lookup, 404)
	request(http.MethodPost, "/api/devices", alice, map[string]any{
		"imeis": registration.IMEIs, "public_key": registration.PublicKey, "owner_subject": "bob",
	}, 400)
	bobRegistration := input(t, first.IMEI)
	request(http.MethodPost, "/api/devices", bob, bobRegistration, 409)
	if _, err := db.ExecContext(context.Background(), "UPDATE device_ownerships SET started_at = clock_timestamp() - interval '15 days' WHERE id = ?", first.OwnershipID); err != nil {
		t.Fatal(err)
	}
	request(http.MethodPost, "/api/devices", bob, bobRegistration, 200)
	request(http.MethodPost, "/api/devices/lookup", alice, lookup, 404)
	request(http.MethodPost, "/api/devices/lookup", bob, lookup, 200)
	request(http.MethodPost, "/api/devices/"+first.IMEI+"/revoke", "", nil, 401)
	request(http.MethodPost, "/api/devices/"+first.IMEI+"/revoke", alice, nil, 404)
	request(http.MethodPost, "/api/devices/"+first.IMEI+"/revoke", bob, nil, 204)
	request(http.MethodPost, "/api/devices/lookup", bob, lookup, 404)
	request(http.MethodPost, "/api/devices", alice, input(t, first.IMEI), 200)
	// An old registration must not be able to revoke the next owner's registration.
	request(http.MethodPost, "/api/devices/"+first.IMEI+"/revoke", bob, nil, 404)
	request(http.MethodPost, "/api/devices/lookup", alice, lookup, 200)
}
