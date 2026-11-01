//go:build integration

package device

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/labstack/echo/v5"
	"net/http"
	"net/http/httptest"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/infrastructure/auth"
	authdb "smapass-back/internal/infrastructure/database/auth"
	"smapass-back/internal/infrastructure/database/certificate"
	"smapass-back/internal/infrastructure/database/testutil"
	"smapass-back/internal/infrastructure/router"
	"smapass-back/internal/interface/controller"
	"smapass-back/internal/usecase"
	"testing"
)

func TestDeviceHTTPAuthenticationAndAuthorization(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	authUsecase := usecase.NewAuthUsecase(authdb.NewRepository(db))
	deviceUsecase := usecase.NewDeviceUsecase(NewRepository(db))
	certs := certificate.NewRepository(db)
	cc := controller.NewCertificateController(usecase.NewCertificateUsecase(certs))
	app := router.New(cc, controller.NewHealthController(usecase.NewHealthUsecase(db)), []string{"http://localhost:3000"})
	authenticate := auth.Middleware(authUsecase.Authenticate)
	router.AddDeviceRoutes(app, controller.NewDeviceController(deviceUsecase), authenticate)
	router.AddAuthRoutes(app, controller.NewAuthController(authUsecase), cc, authenticate)
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
			t.Fatalf("%s %s got %d want %d", method, path, w.Code, status)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("response is cacheable")
		}
		return w.Body.Bytes()
	}
	decodePair := func(body []byte) entity.TokenPair {
		t.Helper()
		var p entity.TokenPair
		if err := json.Unmarshal(body, &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	request(http.MethodGet, "/certificates/000000000094821", "", nil, 200)
	request(http.MethodGet, "/api/auth/session", "", nil, 401)
	request(http.MethodPost, "/api/auth/devices", "", map[string]string{"imei": "bad"}, 400)
	a := decodePair(request(http.MethodPost, "/api/auth/devices", "", entity.EnrollDevice{IMEI: "490154203237518"}, 201))
	request(http.MethodPost, "/api/auth/devices", "", entity.EnrollDevice{IMEI: a.IMEI}, 409)
	request(http.MethodGet, "/api/auth/session", a.AccessToken, nil, 200)
	req := input(t, a.IMEI, "356938035643809")
	request(http.MethodPost, "/api/devices", "", req, 401)
	request(http.MethodPost, "/api/devices", a.IMEI, req, 401)
	var device entity.Device
	if err := json.Unmarshal(request(http.MethodPost, "/api/devices", a.AccessToken, req, 200), &device); err != nil {
		t.Fatal(err)
	}
	lookup := map[string]string{"imei": a.IMEI}
	request(http.MethodPost, "/api/devices/lookup", a.AccessToken, lookup, 200)
	request(http.MethodPost, "/api/devices", a.AccessToken, map[string]any{"imeis": req.IMEIs, "public_key": req.PublicKey, "owner_subject": "forged"}, 400)
	id := fixtureCertificate(t, db, &device)
	b := decodePair(request(http.MethodPost, "/api/auth/devices", "", entity.EnrollDevice{IMEI: "000000000094821"}, 201))
	request(http.MethodPost, "/api/devices/lookup", b.AccessToken, lookup, 404)
	request(http.MethodPost, "/api/devices/"+a.IMEI+"/revoke", b.AccessToken, nil, 404)
	request(http.MethodPost, "/api/certificates/"+id+"/revoke", b.AccessToken, nil, 404)
	request(http.MethodPost, "/api/certificates/"+id+"/revoke", "", nil, 401)
	request(http.MethodPost, "/api/certificates/02ABCDEFGH/revoke", a.AccessToken, nil, 204)
	request(http.MethodPost, "/api/certificates/"+id+"/revoke", a.AccessToken, nil, 204)
	historical, err := certs.FindByNumber(ctx, id)
	if err != nil || historical.Status != "revoked" {
		t.Fatalf("revocation missing: %v", err)
	}
	request(http.MethodPost, "/api/devices/lookup", a.AccessToken, lookup, 200)
	stale, err := authUsecase.Authenticate(ctx, a.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	next := decodePair(request(http.MethodPost, "/api/auth/refresh", "", entity.RefreshRequest{RefreshToken: a.RefreshToken}, 200))
	request(http.MethodGet, "/api/auth/session", a.AccessToken, nil, 401)
	if _, err = deviceUsecase.Register(ctx, stale, req); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("transaction accepted stale session: %v", err)
	}
	request(http.MethodGet, "/api/auth/session", next.AccessToken, nil, 200)
	request(http.MethodPost, "/api/devices/356938035643809/revoke", next.AccessToken, nil, 204)
	request(http.MethodGet, "/api/auth/session", next.AccessToken, nil, 401)
	replacement := decodePair(request(http.MethodPost, "/api/auth/devices", "", entity.EnrollDevice{IMEI: a.IMEI}, 201))
	request(http.MethodPost, "/api/devices", replacement.AccessToken, input(t, a.IMEI), 200)
	request(http.MethodPost, "/api/devices/"+a.IMEI+"/revoke", next.AccessToken, nil, 401)
	request(http.MethodPost, "/api/auth/revoke", "", entity.RefreshRequest{RefreshToken: b.RefreshToken}, 204)
	request(http.MethodGet, "/api/auth/session", b.AccessToken, nil, 401)
	request(http.MethodPost, "/api/auth/refresh", "", entity.RefreshRequest{RefreshToken: b.RefreshToken}, 401)
	request(http.MethodPost, "/api/auth/revoke", "", entity.RefreshRequest{RefreshToken: b.RefreshToken}, 204)
	request(http.MethodPost, "/api/auth/devices", "", entity.EnrollDevice{IMEI: b.IMEI}, 409)
	// A browser Origin never grants permission; Android requests above have no Origin.
	r := httptest.NewRequest(http.MethodPost, "/api/devices/"+a.IMEI+"/revoke", nil)
	r.Header.Set(echo.HeaderOrigin, "http://localhost:3000")
	r.RemoteAddr = "198.51.100.1:1234"
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	if w.Code != 401 || w.Header().Get(echo.HeaderAccessControlAllowOrigin) != "http://localhost:3000" {
		t.Fatal("CORS was used as authorization")
	}
}
