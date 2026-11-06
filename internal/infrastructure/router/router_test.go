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

func (publicDeviceRepository) FindState(_ context.Context, imei string, limit, offset int) (*entity.DeviceState, error) {
	if imei != "11111111-1111-4111-8111-111111111111" {
		return nil, repository.ErrNotFound
	}
	return &entity.DeviceState{ID: imei, RegistrationStatus: "released", History: entity.DeviceHistory{Items: []entity.StatusRecord{{Sequence: 1}}, Total: 1, Limit: limit, Offset: offset}}, nil
}

func (publicDeviceRepository) Records(_ context.Context, imei string, _, _ int) ([]entity.VerificationRecord, int, error) {
	if imei != "11111111-1111-4111-8111-111111111111" {
		return nil, 0, repository.ErrNotFound
	}
	return []entity.VerificationRecord{}, 0, nil
}

func (publicDeviceRepository) ListStates(_ context.Context, imeis []string) ([]entity.DeviceSummary, error) {
	items := make([]entity.DeviceSummary, 0)
	for _, imei := range imeis {
		if imei == "11111111-1111-4111-8111-111111111111" {
			items = append(items, entity.DeviceSummary{ID: imei})
		}
	}
	return items, nil
}

func (publicDeviceRepository) Search(_ context.Context, imeis []string) ([]entity.DeviceReference, error) {
	items := make([]entity.DeviceReference, 0)
	for _, imei := range imeis {
		if imei == "490154203237518" {
			items = append(items, entity.DeviceReference{IMEI: imei, ID: "11111111-1111-4111-8111-111111111111"})
		}
	}
	return items, nil
}

func TestPublicDeviceStateWithoutAuthentication(t *testing.T) {
	for _, test := range []struct {
		path   string
		status int
	}{
		{"/api/devices/11111111-1111-4111-8111-111111111111", 200},
		{"/api/devices/11111111-1111-4111-8111-111111111111?limit=1&offset=0", 200},
		{"/api/devices/11111111-1111-4111-8111-111111111111/records", 200},
		{"/api/devices/11111111-1111-4111-8111-111111111111/unknown", 404},
		{"/api/devices/11111111-1111-4111-8111-111111111111?limit=501", 400},
		{"/api/devices/11111111-1111-4111-8111-111111111111?offset=-1", 400},
		{"/api/devices/22222222-2222-4222-8222-222222222222", 404},
		{"/api/devices/invalid", 400},
		{"/api/devices/490154203237518", 400},
		{"/api/devices/search?imeis=490154203237518,356938035643809", 200},
		{"/api/devices/search?imeis=bad", 400},
		{"/api/devices/search", 400},
		{"/api/devices/search?imeis=490154203237518,", 400},
		{"/api/devices/search?imeis=" + strings.TrimSuffix(strings.Repeat("490154203237518,", 51), ","), 400},
		{"/api/devices/11111111-1111-4111-8111-111111111111/records?limit=501", 400},
		{"/api/devices/11111111-1111-4111-8111-111111111111/records?offset=-1", 400},
		{"/api/devices/11111111-1111-4111-8111-111111111111/records?offset=1000001", 400},
		{"/api/devices?ids=11111111-1111-4111-8111-111111111111,22222222-2222-4222-8222-222222222222", 200},
		{"/api/devices?ids=22222222-2222-4222-8222-222222222222", 200},
		{"/api/devices", 400},
		{"/api/devices?ids=11111111-1111-4111-8111-111111111111,", 400},
		{"/api/devices?ids=bad", 400},
		{"/api/devices?ids=" + strings.TrimSuffix(strings.Repeat("11111111-1111-4111-8111-111111111111,", 51), ","), 400},
	} {
		t.Run(test.path, func(t *testing.T) {
			app := New(controller.NewHealthController(nil), nil)
			AddDeviceRoutes(app, controller.NewDeviceController(usecase.NewDeviceUsecase(publicDeviceRepository{}, nil)), func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error { return echo.NewHTTPError(401, "authentication required") }
			})
			w := httptest.NewRecorder()
			app.ServeHTTP(w, httptest.NewRequest("GET", test.path, nil))
			if w.Code != test.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if test.path == "/api/devices/11111111-1111-4111-8111-111111111111" {
				var result map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if _, ok := result["imei"]; ok {
					t.Fatal("display leaked IMEI")
				}
				if _, ok := result["imeis"]; ok {
					t.Fatal("display leaked IMEIs")
				}
				if result["registration_status"] != "released" || result["history"].(map[string]any)["total"] != float64(1) || result["status"] != nil {
					t.Fatalf("released state or missing measurements: %v", result)
				}
			}
		})
	}
}

func TestUnconfiguredAttestationFailsClosed(t *testing.T) {
	v, err := attestation.NewVerifier(attestation.Policy{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys := usecase.NewKeyRegistrationUsecase(nil, v)
	auth := controller.NewAuthController(usecase.NewAuthUsecase(nil, keys))
	app := New(controller.NewHealthController(nil), nil)
	AddAuthRoutes(app, auth)
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
	} {
		t.Run(test.path, func(t *testing.T) {
			app := New(controller.NewHealthController(usecase.NewHealthUsecase(unavailableDatabase{})), []string{"http://localhost:3000"})
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
