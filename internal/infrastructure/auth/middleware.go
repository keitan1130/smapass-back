package auth

import (
	"context"
	"errors"
	"github.com/labstack/echo/v5"
	"net/http"
	"smapass-back/internal/domain/entity"
	requestauth "smapass-back/internal/interface/auth"
	"strings"
)

func Middleware(authenticate func(context.Context, string) (entity.Principal, error)) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			parts := strings.Fields(c.Request().Header.Get(echo.HeaderAuthorization))
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !entity.ValidToken(parts[1], "at") {
				return entity.ErrUnauthenticated
			}
			p, err := authenticate(c.Request().Context(), parts[1])
			if errors.Is(err, entity.ErrUnauthenticated) {
				return err
			}
			if err != nil {
				return echo.NewHTTPError(http.StatusServiceUnavailable, "authentication unavailable")
			}
			c.SetRequest(c.Request().WithContext(requestauth.WithPrincipal(c.Request().Context(), p)))
			return next(c)
		}
	}
}
