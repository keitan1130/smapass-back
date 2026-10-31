package router

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
	"smapass-back/internal/interface/controller"
	"smapass-back/internal/interface/schema"
)

func New(certificates *controller.CertificateController, health *controller.HealthController, origins []string) *echo.Echo {
	e := echo.New()
	e.IPExtractor = echo.ExtractIPDirect()
	e.SchemeExtractor = echo.ExtractSchemeDirect()
	e.HTTPErrorHandler = writeError
	e.Use(middleware.RequestID(), middleware.Recover(), apiPolicy)
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus: true, LogMethod: true, LogRequestID: true, HandleError: true,
		LogValuesFunc: func(_ *echo.Context, v middleware.RequestLoggerValues) error {
			slog.Info("request", "method", v.Method, "status", v.Status, "request_id", v.RequestID)
			return nil
		},
	}))
	if len(origins) > 0 {
		e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins: origins, AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
			AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderAccept, echo.HeaderContentType, echo.HeaderAuthorization},
		}))
	}
	e.GET("/health", health.Live)
	e.GET("/ready", health.Ready)
	api := e.Group("/api", middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(10)))
	api.GET("/certificates", certificates.List)
	api.GET("/certificates/:number", certificates.Get)
	api.GET("/certificates/:number/records", certificates.Records)
	e.GET("/certificates/:imei", certificates.CurrentByIMEI, middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(10)))
	return e
}

func apiPolicy(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		c.Response().Header().Set("X-Robots-Tag", "noindex, nofollow")
		ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Second)
		defer cancel()
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}

func AddDeviceRoutes(e *echo.Echo, devices *controller.DeviceController, authenticate echo.MiddlewareFunc) {
	g := e.Group("/api/devices", middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(5)), authenticate)
	g.POST("", devices.Register)
	g.POST("/lookup", devices.Lookup)
	g.POST("/:imei/revoke", devices.Revoke)
}

func writeError(c *echo.Context, err error) {
	if response, _ := echo.UnwrapResponse(c.Response()); response != nil && response.Committed {
		return
	}
	status, code, message := http.StatusInternalServerError, "internal_error", "internal server error"
	var httpErr *echo.HTTPError
	var statusCoder echo.HTTPStatusCoder
	switch {
	case errors.Is(err, entity.ErrInvalidNumber):
		status, code, message = 400, "invalid_number", "specify valid certificate numbers (1 to 50 ids for list)"
	case errors.Is(err, entity.ErrInvalidDevice):
		status, code, message = 400, "invalid_device", "valid IMEIs and a P-256 public key are required"
	case errors.Is(err, entity.ErrDeviceInUse):
		status, code, message = 409, "device_in_use", "registration requires 15 days without verified logs"
	case errors.Is(err, entity.ErrDeviceConflict):
		status, code, message = 409, "device_conflict", "device identifiers or key conflict"
	case errors.Is(err, repository.ErrNotFound):
		status, code, message = 404, "not_found", "resource not found"
	case errors.Is(err, context.DeadlineExceeded):
		status, code, message = 503, "unavailable", "request timed out"
	case errors.As(err, &statusCoder):
		status = statusCoder.StatusCode()
		code = map[int]string{400: "bad_request", 401: "unauthorized", 404: "not_found", 405: "method_not_allowed", 429: "rate_limited", 503: "unavailable"}[status]
		if code == "" {
			code = "http_error"
		}
		message = http.StatusText(status)
		if status < 500 && errors.As(err, &httpErr) && httpErr.Message != "" {
			message = httpErr.Message
		}
	}
	if status >= 500 {
		slog.Error("request failed", "status", status, "request_id", c.Response().Header().Get(echo.HeaderXRequestID))
	}
	_ = c.JSON(status, schema.Error{Error: schema.ErrorDetail{Code: code, Message: message}})
}
