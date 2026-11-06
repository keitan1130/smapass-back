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

func New(health *controller.HealthController, origins []string) *echo.Echo {
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
	public := e.Group("/api/devices", middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(10)))
	public.GET("", devices.List)
	public.GET("/search", devices.Search)
	public.GET("/:id", devices.State)
	public.GET("/:id/records", devices.Records)
	g := e.Group("/api/devices", middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(5)), authenticate)
	g.POST("", devices.Register)
	g.POST("/key-challenges", devices.Challenge)
	g.POST("/lookup", devices.Lookup)
	g.POST("/:imei/revoke", devices.Revoke)
	g.POST("/:imei/logs", devices.Ingest)
}

func AddAuthRoutes(e *echo.Echo, auth *controller.AuthController) {
	g := e.Group("/api/auth", middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(2)))
	g.POST("/devices", auth.Enroll)
	g.POST("/challenges", auth.Challenge)
	g.POST("/refresh", auth.Refresh)
	g.POST("/revoke", auth.Revoke)
}

func writeError(c *echo.Context, err error) {
	if response, _ := echo.UnwrapResponse(c.Response()); response != nil && response.Committed {
		return
	}
	status, code, message := http.StatusInternalServerError, "internal_error", "internal server error"
	var httpErr *echo.HTTPError
	var statusCoder echo.HTTPStatusCoder
	switch {
	case errors.Is(err, entity.ErrInvalidMeasurement):
		status, code, message = 400, "invalid_measurement", "invalid canonical measurement payload or signature encoding"
	case errors.Is(err, entity.ErrMeasurementSignature):
		status, code, message = 403, "measurement_signature_rejected", "measurement signature or current attested key rejected"
	case errors.Is(err, entity.ErrMeasurementConflict):
		status, code, message = 409, "measurement_conflict", "measurement sequence, previous hash or retry content conflict"
	case errors.Is(err, entity.ErrAttestationUnavailable):
		status, code, message = 503, "attestation_unavailable", "attestation verification is unavailable"
	case errors.Is(err, entity.ErrAttestationRejected):
		status, code, message = 403, "attestation_rejected", "key attestation or possession proof rejected"
	case errors.Is(err, entity.ErrChallengeInvalid):
		status, code, message = 409, "invalid_challenge", "registration challenge invalid, expired or already used"
	case errors.Is(err, entity.ErrUnauthenticated):
		status, code, message = 401, "unauthorized", "valid device credentials required"
	case errors.Is(err, entity.ErrInvalidDeviceID):
		status, code, message = 400, "invalid_id", "valid device UUID required"
	case errors.Is(err, entity.ErrInvalidDevice):
		status, code, message = 400, "invalid_device", "valid IMEIs and a P-256 public key are required"
	case errors.Is(err, entity.ErrDeviceInUse):
		status, code, message = 409, "device_in_use", "registration requires 3 days without verified logs"
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
