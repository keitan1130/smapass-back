package router

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
	"smapass-back/internal/interface/controller"
	"smapass-back/internal/usecase"
)

type missingRepository struct{}

func (missingRepository) RevokeOwned(context.Context, entity.Principal, string) error {
	return repository.ErrNotFound
}

func (missingRepository) FindCurrentByIMEI(context.Context, string) (*entity.Certificate, error) {
	return nil, repository.ErrNotFound
}

func (missingRepository) FindByNumber(context.Context, string) (*entity.Certificate, error) {
	return nil, repository.ErrNotFound
}
func (missingRepository) FindByNumbers(context.Context, []string) ([]entity.Certificate, error) {
	return []entity.Certificate{}, nil
}
func (missingRepository) Records(context.Context, string, int, int) ([]entity.VerificationRecord, int, error) {
	panic("must check certificate existence before listing records")
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
		{"/api/certificates/01ABC-DEFGH", 404, `"code":"not_found"`},
		{"/api/certificates/01ABCDEFGH/records", 404, `"code":"not_found"`},
		{"/api/certificates/invalid", 400, `"code":"invalid_number"`},
		{"/api/certificates", 400, `"code":"invalid_number"`},
		{"/api/certificates?ids=01ABCDEFGH", 200, `"items":[]`},
		{"/api/certificates/01ABCDEFGH/records?limit=501", 400, `"error"`},
		{"/api/certificates/01ABCDEFGH/records?offset=-1", 400, `"error"`},
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
