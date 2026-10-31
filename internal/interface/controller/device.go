package controller

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/labstack/echo/v5"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/interface/auth"
	"smapass-back/internal/usecase"
)

type DeviceController struct{ usecase *usecase.DeviceUsecase }

func NewDeviceController(u *usecase.DeviceUsecase) *DeviceController {
	return &DeviceController{usecase: u}
}

func decodeDevice(c *echo.Context, value any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return echo.NewHTTPError(http.StatusBadRequest, "one JSON object is required")
	}
	return nil
}

func (c *DeviceController) Register(ctx *echo.Context) error {
	principal, ok := auth.Principal(ctx.Request().Context())
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	var input entity.RegisterDevice
	if err := decodeDevice(ctx, &input); err != nil {
		return err
	}
	device, err := c.usecase.Register(ctx.Request().Context(), principal, input)
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, device)
}

func (c *DeviceController) Lookup(ctx *echo.Context) error {
	principal, ok := auth.Principal(ctx.Request().Context())
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	var input struct {
		IMEI string `json:"imei"`
	}
	if err := decodeDevice(ctx, &input); err != nil {
		return err
	}
	device, err := c.usecase.Lookup(ctx.Request().Context(), principal, input.IMEI)
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, device)
}

func (c *DeviceController) Revoke(ctx *echo.Context) error {
	principal, ok := auth.Principal(ctx.Request().Context())
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	if err := c.usecase.Revoke(ctx.Request().Context(), principal, ctx.Param("imei")); err != nil {
		return err
	}
	return ctx.NoContent(http.StatusNoContent)
}
