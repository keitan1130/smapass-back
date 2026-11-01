package auth

import (
	"context"
	"errors"
	"github.com/labstack/echo/v5"
	"net/http/httptest"
	"smapass-back/internal/domain/entity"
	requestauth "smapass-back/internal/interface/auth"
	"testing"
)

func TestBearerMiddleware(t *testing.T) {
	token := entity.NewTokenMaterial().Access
	for _, tc := range []struct {
		header  string
		failure error
		status  int
	}{
		{"", nil, 401}, {"Bearer 490154203237518", nil, 401}, {"Bearer " + token, nil, 200}, {"Bearer " + token, entity.ErrUnauthenticated, 401}, {"Bearer " + token, errors.New("private database credentials"), 503},
	} {
		e := echo.New()
		e.GET("/private", func(c *echo.Context) error {
			p, ok := requestauth.Principal(c.Request().Context())
			if !ok || p.IMEI != "490154203237518" {
				t.Fatal("principal missing")
			}
			return c.NoContent(200)
		}, Middleware(func(_ context.Context, raw string) (entity.Principal, error) {
			if raw != token {
				t.Fatal("unparsed token")
			}
			return entity.Principal{Issuer: entity.DeviceIssuer, Subject: "installation", SessionID: "session", IMEI: "490154203237518"}, tc.failure
		}))
		// Use Echo's HTTP errors for this middleware-only test.
		e.HTTPErrorHandler = func(c *echo.Context, err error) {
			if errors.Is(err, entity.ErrUnauthenticated) {
				_ = c.NoContent(401)
				return
			}
			_ = c.NoContent(503)
		}
		r := httptest.NewRequest("GET", "/private", nil)
		r.Header.Set("Authorization", tc.header)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("status=%d want=%d", w.Code, tc.status)
		}
	}
}
