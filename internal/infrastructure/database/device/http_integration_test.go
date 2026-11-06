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
	"smapass-back/internal/infrastructure/database/testutil"
	"smapass-back/internal/infrastructure/router"
	"smapass-back/internal/interface/controller"
	"smapass-back/internal/usecase"
	"testing"
)

func TestDeviceHTTPAuthenticationAndAuthorization(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	authUsecase := usecase.NewAuthUsecase(authdb.NewRepository(db), testutil.Registration(t, db))
	deviceUsecase := usecase.NewDeviceUsecase(NewRepository(db), testutil.Registration(t, db))
	app := router.New(controller.NewHealthController(usecase.NewHealthUsecase(db)), []string{"http://localhost:3000"})
	authenticate := auth.Middleware(authUsecase.Authenticate)
	router.AddDeviceRoutes(app, controller.NewDeviceController(deviceUsecase), authenticate)
	router.AddAuthRoutes(app, controller.NewAuthController(authUsecase))
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
	enrollment := func(imei string) entity.EnrollDevice {
		t.Helper()
		var challenge entity.KeyChallenge
		body := request(http.MethodPost, "/api/auth/challenges", "", entity.EnrollmentChallengeRequest{IMEI: imei}, 201)
		if err := json.Unmarshal(body, &challenge); err != nil {
			t.Fatal(err)
		}
		public, proof := testutil.Proof(t, &challenge)
		return entity.EnrollDevice{IMEI: imei, PublicKey: public, Proof: proof}
	}
	rotation := func(token string, imeis ...string) entity.RegisterDevice {
		t.Helper()
		var challenge entity.KeyChallenge
		body := request(http.MethodPost, "/api/devices/key-challenges", token, entity.RotationChallengeRequest{IMEIs: imeis}, 201)
		if err := json.Unmarshal(body, &challenge); err != nil {
			t.Fatal(err)
		}
		public, proof := testutil.Proof(t, &challenge)
		return entity.RegisterDevice{IMEIs: imeis, PublicKey: public, Proof: proof}
	}
	request(http.MethodGet, "/api/devices/"+publicID(t, db, "000000000094821"), "", nil, 200)
	request(http.MethodPost, "/api/devices/lookup", "", map[string]string{"imei": "490154203237518"}, 401)
	request(http.MethodPost, "/api/devices/key-challenges", "", entity.RotationChallengeRequest{IMEIs: []string{"490154203237518"}}, 401)
	request(http.MethodPost, "/api/auth/devices", "", entity.EnrollDevice{IMEI: "490154203237518"}, 400)
	request(http.MethodPost, "/api/auth/devices", "", map[string]string{"imei": "bad"}, 400)
	a := decodePair(request(http.MethodPost, "/api/auth/devices", "", enrollment("490154203237518"), 201))
	request(http.MethodPost, "/api/auth/devices", "", enrollment(a.IMEI), 409)
	request(http.MethodPost, "/api/devices/lookup", a.AccessToken, map[string]string{"imei": "490154203237518"}, 200)
	request(http.MethodGet, "/api/devices/"+publicID(t, db, a.IMEI), "", nil, 200)
	request(http.MethodGet, "/api/devices/"+publicID(t, db, a.IMEI)+"?limit=2", "", nil, 200)
	req := rotation(a.AccessToken, a.IMEI, "356938035643809")
	request(http.MethodPost, "/api/devices", "", req, 401)
	request(http.MethodPost, "/api/devices", a.IMEI, req, 401)
	var device entity.Device
	if err := json.Unmarshal(request(http.MethodPost, "/api/devices", a.AccessToken, req, 200), &device); err != nil {
		t.Fatal(err)
	}
	lookup := map[string]string{"imei": a.IMEI}
	request(http.MethodPost, "/api/devices/lookup", a.AccessToken, lookup, 200)
	request(http.MethodPost, "/api/devices", a.AccessToken, map[string]any{"imeis": req.IMEIs, "public_key": req.PublicKey, "owner_subject": "forged"}, 400)
	body := request(http.MethodGet, "/api/devices/"+publicID(t, db, "356938035643809"), "", nil, 200)
	var public map[string]any
	if err := json.Unmarshal(body, &public); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"imei", "imeis", "certificate", "snapshot", "ownership_id", "key_id", "owner_subject"} {
		if _, exists := public[field]; exists {
			t.Fatalf("private or stale field exposed: %s", field)
		}
	}
	request(http.MethodGet, "/api/devices?ids="+publicID(t, db, a.IMEI), "", nil, 200)
	request(http.MethodGet, "/api/certificates/"+a.IMEI, "", nil, 404)
	request(http.MethodPost, "/api/certificates/"+a.IMEI+"/revoke", a.AccessToken, nil, 404)
	b := decodePair(request(http.MethodPost, "/api/auth/devices", "", enrollment("000000000094821"), 201))
	request(http.MethodPost, "/api/devices/lookup", b.AccessToken, lookup, 404)
	request(http.MethodPost, "/api/devices/"+a.IMEI+"/revoke", b.AccessToken, nil, 404)
	request(http.MethodGet, "/api/devices/"+publicID(t, db, a.IMEI)+"/records", "", nil, 200)
	request(http.MethodPost, "/api/devices/lookup", a.AccessToken, lookup, 200)
	stale, err := authUsecase.Authenticate(ctx, a.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	next := decodePair(request(http.MethodPost, "/api/auth/refresh", "", entity.RefreshRequest{RefreshToken: a.RefreshToken}, 200))
	request(http.MethodPost, "/api/devices/lookup", a.AccessToken, map[string]string{"imei": "490154203237518"}, 401)
	if _, err = deviceUsecase.Register(ctx, stale, req); !errors.Is(err, entity.ErrUnauthenticated) {
		t.Fatalf("transaction accepted stale session: %v", err)
	}
	request(http.MethodPost, "/api/devices/lookup", next.AccessToken, map[string]string{"imei": "490154203237518"}, 200)
	request(http.MethodPost, "/api/devices/356938035643809/revoke", next.AccessToken, nil, 204)
	request(http.MethodPost, "/api/devices/lookup", next.AccessToken, map[string]string{"imei": "490154203237518"}, 401)
	var released entity.DeviceState
	if err := json.Unmarshal(request(http.MethodGet, "/api/devices/"+publicID(t, db, "356938035643809"), "", nil, 200), &released); err != nil || released.RegistrationStatus != "released" {
		t.Fatalf("sale interrupted public state: %v", err)
	}
	request(http.MethodGet, "/api/devices/"+publicID(t, db, a.IMEI)+"?limit=2", "", nil, 200)
	replacement := decodePair(request(http.MethodPost, "/api/auth/devices", "", enrollment(a.IMEI), 201))
	request(http.MethodPost, "/api/devices", replacement.AccessToken, rotation(replacement.AccessToken, a.IMEI), 200)
	request(http.MethodPost, "/api/devices/"+a.IMEI+"/revoke", next.AccessToken, nil, 401)
	request(http.MethodPost, "/api/auth/revoke", "", entity.RefreshRequest{RefreshToken: b.RefreshToken}, 204)
	request(http.MethodPost, "/api/devices/lookup", b.AccessToken, map[string]string{"imei": b.IMEI}, 401)
	request(http.MethodPost, "/api/auth/refresh", "", entity.RefreshRequest{RefreshToken: b.RefreshToken}, 401)
	request(http.MethodPost, "/api/auth/revoke", "", entity.RefreshRequest{RefreshToken: b.RefreshToken}, 204)
	request(http.MethodPost, "/api/auth/devices", "", enrollment(b.IMEI), 409)
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
