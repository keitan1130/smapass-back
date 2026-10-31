package controller

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
	"smapass-back/internal/usecase"
)

type HealthController struct{ usecase *usecase.HealthUsecase }

func NewHealthController(u *usecase.HealthUsecase) *HealthController {
	return &HealthController{usecase: u}
}

func (c *HealthController) Live(ctx *echo.Context) error {
	return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (c *HealthController) Ready(ctx *echo.Context) error {
	requestCtx, cancel := context.WithTimeout(ctx.Request().Context(), 2*time.Second)
	defer cancel()
	if err := c.usecase.Ready(requestCtx); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "database unavailable")
	}
	return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
