package router

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
	interfaceauth "smapass-back/internal/interface/auth"
	"smapass-back/internal/interface/controller"
	"smapass-back/internal/usecase"
)

type measurementRepository struct {
	repository.DeviceRepository
	publicKey string
	duplicate bool
	err       error
	calls     int
}

func (r *measurementRepository) AppendMeasurement(_ context.Context, _ entity.Principal, imei string, log *entity.MeasurementLog) (*entity.MeasurementReceipt, error) {
	r.calls++
	if err := log.Verify(r.publicKey); err != nil {
		return nil, err
	}
	if r.err != nil {
		return nil, r.err
	}
	return &entity.MeasurementReceipt{IMEI: imei, Sequence: log.Payload.Sequence, PayloadHash: hex.EncodeToString(log.Hash[:]),
		ReceivedAt: time.Now().UTC(), SignatureVerified: true, Duplicate: r.duplicate}, nil
}

func TestMeasurementRouteAuthenticationParsingAndReceipts(t *testing.T) {
	request, err := os.ReadFile("../../../docs/examples/measurement-request.json")
	if err != nil {
		t.Fatal(err)
	}
	vector, err := os.ReadFile("../../../docs/examples/measurement-vector.json")
	if err != nil {
		t.Fatal(err)
	}
	var key struct {
		PublicKey string `json:"public_key"`
	}
	if err := json.Unmarshal(vector, &key); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, body               string
		authenticated, duplicate bool
		repositoryError          error
		status, calls            int
	}{
		{name: "new", body: string(request), authenticated: true, status: 201, calls: 1},
		{name: "retry", body: string(request), authenticated: true, duplicate: true, status: 200, calls: 1},
		{name: "unauthenticated", body: string(request), status: 401},
		{name: "oversized", body: strings.Repeat(" ", 128<<10+1), authenticated: true, status: 413},
		{name: "duplicate members", body: `{"canonical_payload":"a","canonical_payload":"b"}`, authenticated: true, status: 400},
		{name: "private fields", body: `{"owner_subject":"private"}`, authenticated: true, status: 400},
		{name: "empty", body: `{}`, authenticated: true, status: 400},
		{name: "tampered", body: strings.Replace(string(request), `32.3`, `32.4`, 1), authenticated: true, status: 403, calls: 1},
		{name: "sequence conflict", body: string(request), authenticated: true, repositoryError: entity.ErrMeasurementConflict, status: 409, calls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &measurementRepository{publicKey: key.PublicKey, duplicate: test.duplicate, err: test.repositoryError}
			app := New(controller.NewHealthController(nil), nil)
			AddDeviceRoutes(app, controller.NewDeviceController(usecase.NewDeviceUsecase(repo, nil)), func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error {
					if !test.authenticated {
						return entity.ErrUnauthenticated
					}
					c.SetRequest(c.Request().WithContext(interfaceauth.WithPrincipal(c.Request().Context(), entity.Principal{Issuer: entity.DeviceIssuer, Subject: "installation", SessionID: "session", IMEI: "490154203237518"})))
					return next(c)
				}
			})
			response := httptest.NewRecorder()
			app.ServeHTTP(response, httptest.NewRequest("POST", "/api/devices/490154203237518/logs", strings.NewReader(test.body)))
			if response.Code != test.status || repo.calls != test.calls {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, repo.calls, response.Body.String())
			}
			if test.status < 300 {
				var receipt entity.MeasurementReceipt
				if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil {
					t.Fatal(err)
				}
				if receipt.Duplicate != test.duplicate || !receipt.SignatureVerified || receipt.IMEI != "490154203237518" {
					t.Fatalf("receipt: %+v", receipt)
				}
			}
		})
	}
}
