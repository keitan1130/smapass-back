package router

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/labstack/echo/v5"
	"net/http/httptest"
	"strings"
	"testing"

	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
	"smapass-back/internal/infrastructure/attestation"
	"smapass-back/internal/interface/controller"
	"smapass-back/internal/usecase"
)

type publicDeviceRepository struct {
	repository.DeviceRepository
}

func (publicDeviceRepository) FindState(_ context.Context, imei string) (*entity.DeviceState, error) {
	if imei != "490154203237518" {
		return nil, repository.ErrNotFound
	}
	return &entity.DeviceState{IMEI: imei, IMEIs: []string{imei}, RegistrationStatus: "released"}, nil
}

func (publicDeviceRepository) StatusHistory(_ context.Context, imei string, _, _ int) ([]entity.StatusRecord, int, error) {
	if imei != "490154203237518" {
		return nil, 0, repository.ErrNotFound
	}
	return []entity.StatusRecord{}, 0, nil
}

func TestPublicDeviceStateDoesNotRequireCurrentHolderOrCertificate(t *testing.T) {
	for _, test := range []struct {
		path   string
		status int
	}{
		{"/api/devices/490154203237518", 200},
		{"/api/devices/490154203237518/history", 200},
		{"/api/devices/490154203237518/history?limit=501", 400},
		{"/api/devices/490154203237518/history?offset=-1", 400},
		{"/api/devices/356938035643809", 404},
		{"/api/devices/invalid", 400},
	} {
		t.Run(test.path, func(t *testing.T) {
			app := New(controller.NewCertificateController(nil), controller.NewHealthController(nil), nil)
			AddDeviceRoutes(app, controller.NewDeviceController(usecase.NewDeviceUsecase(publicDeviceRepository{}, nil)), func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error { return echo.NewHTTPError(401, "authentication required") }
			})
			w := httptest.NewRecorder()
			app.ServeHTTP(w, httptest.NewRequest("GET", test.path, nil))
			if w.Code != test.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if test.path == "/api/devices/490154203237518" {
				var result map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result["registration_status"] != "released" || result["certificate"] != nil || result["status"] != nil {
					t.Fatalf("released state or missing measurements: %v", result)
				}
			}
		})
	}
}

type missingRepository struct{}

func (missingRepository) RevokeOwned(context.Context, entity.Principal, string) error {
	return repository.ErrNotFound
}

func TestUnconfiguredAttestationFailsClosed(t *testing.T) {
	v, err := attestation.NewVerifier(attestation.Policy{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys := usecase.NewKeyRegistrationUsecase(nil, v)
	auth := controller.NewAuthController(usecase.NewAuthUsecase(nil, keys))
	certificates := controller.NewCertificateController(usecase.NewCertificateUsecase(missingRepository{}))
	app := New(certificates, controller.NewHealthController(nil), nil)
	AddAuthRoutes(app, auth, certificates, func(next echo.HandlerFunc) echo.HandlerFunc { return next })
	for i, path := range []string{"/api/auth/challenges", "/api/auth/devices"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{"imei":"490154203237518"}`))
		r.RemoteAddr = []string{"192.0.2.1:1234", "192.0.2.2:1234"}[i]
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		if w.Code != 503 || !strings.Contains(w.Body.String(), "attestation_unavailable") {
			t.Fatalf("unconfigured key registration status=%d", w.Code)
		}
	}
}

func (missingRepository) FindLatestByIMEI(context.Context, string) (*entity.Certificate, error) {
	return nil, repository.ErrNotFound
}

func (missingRepository) FindLatestByIMEIs(context.Context, []string) ([]entity.Certificate, error) {
	return []entity.Certificate{}, nil
}
func (missingRepository) Records(context.Context, string, int, int) ([]entity.VerificationRecord, int, error) {
	return nil, 0, repository.ErrNotFound
}

type unavailableDatabase struct{}

func (unavailableDatabase) PingContext(context.Context) error {
	return errors.New("private database error with credentials")
}

func TestPublicAPIErrorContract(t *testing.T) {
	for _, test := range []struct {
		path     string
		status   int
		contains string
	}{
		{"/health", 200, `"status":"ok"`},
		{"/missing", 404, `"code":"not_found"`},
		{"/ready", 503, `"error"`},
		{"/api/certificates/490154203237518", 404, `"code":"not_found"`},
		{"/api/certificates/490154203237518/records", 404, `"code":"not_found"`},
		{"/api/certificates/01ABCDEFGH", 400, `"code":"invalid_device"`},
		{"/api/certificates/50982a98-31c1-595e-a66d-c3688af07155", 400, `"code":"invalid_device"`},
		{"/api/certificates?ids=490154203237518", 400, `"code":"invalid_device"`},
		{"/api/certificates/invalid", 400, `"code":"invalid_device"`},
		{"/api/certificates", 400, `"code":"invalid_device"`},
		{"/api/certificates?imeis=490154203237518", 200, `"items":[]`},
		{"/api/certificates/490154203237518/records?limit=501", 400, `"error"`},
		{"/api/certificates/490154203237518/records?offset=-1", 400, `"error"`},
	} {
		t.Run(test.path, func(t *testing.T) {
			app := New(controller.NewCertificateController(usecase.NewCertificateUsecase(missingRepository{})), controller.NewHealthController(usecase.NewHealthUsecase(unavailableDatabase{})), []string{"http://localhost:3000"})
			request := httptest.NewRequest("GET", test.path, nil)
			request.Header.Set("Origin", "http://localhost:3000")
			response := httptest.NewRecorder()
			app.ServeHTTP(response, request)
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.contains) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "credentials") {
				t.Fatal("private error leaked")
			}
			if response.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
				t.Fatal("configured browser origin not allowed")
			}
		})
	}
}
