package controller

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/interface/auth"
	"smapass-back/internal/interface/schema"
	"smapass-back/internal/usecase"
)

func (c *DeviceController) Ingest(ctx *echo.Context) error {
	principal, ok := auth.Principal(ctx.Request().Context())
	if !ok {
		return entity.ErrUnauthenticated
	}
	data, err := io.ReadAll(http.MaxBytesReader(ctx.Response(), ctx.Request().Body, 128<<10))
	if err != nil {
		var oversized *http.MaxBytesError
		if errors.As(err, &oversized) {
			return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "measurement request exceeds 128 KiB")
		}
		return entity.ErrInvalidMeasurement
	}
	var input entity.SubmitMeasurement
	if err := entity.DecodeMeasurementJSON(data, &input); err != nil {
		return err
	}
	receipt, err := c.usecase.Ingest(ctx.Request().Context(), principal, ctx.Param("id"), input)
	if err != nil {
		return err
	}
	status := http.StatusCreated
	if receipt.Duplicate {
		status = http.StatusOK
	}
	return ctx.JSON(status, receipt)
}

type DeviceController struct{ usecase *usecase.DeviceUsecase }

func NewDeviceController(u *usecase.DeviceUsecase) *DeviceController {
	return &DeviceController{usecase: u}
}

func decodeDevice(c *echo.Context, value any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return echo.NewHTTPError(http.StatusBadRequest, "one JSON object is required")
	}
	return nil
}

func (c *DeviceController) Challenge(ctx *echo.Context) error {
	p, ok := auth.Principal(ctx.Request().Context())
	if !ok {
		return entity.ErrUnauthenticated
	}
	var input entity.RotationChallengeRequest
	if err := decodeDevice(ctx, &input); err != nil {
		return err
	}
	challenge, err := c.usecase.Challenge(ctx.Request().Context(), p, input)
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusCreated, challenge)
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
		DeviceID string `json:"device_id"`
	}
	if err := decodeDevice(ctx, &input); err != nil {
		return err
	}
	device, err := c.usecase.Lookup(ctx.Request().Context(), principal, input.DeviceID)
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
	if err := c.usecase.Revoke(ctx.Request().Context(), principal, ctx.Param("id")); err != nil {
		return err
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *DeviceController) State(ctx *echo.Context) error {
	limit, offset, err := pagination(ctx)
	if err != nil {
		return err
	}
	result, err := c.usecase.State(ctx.Request().Context(), ctx.Param("id"), limit, offset)
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, result)
}

func (c *DeviceController) Records(ctx *echo.Context) error {
	limit, offset, err := pagination(ctx)
	if err != nil {
		return err
	}
	items, total, err := c.usecase.Records(ctx.Request().Context(), ctx.Param("id"), limit, offset)
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, schema.Records{Items: items, Total: total, Limit: limit, Offset: offset})
}

func (c *DeviceController) List(ctx *echo.Context) error {
	items, err := c.usecase.List(ctx.Request().Context(), strings.Split(ctx.QueryParam("ids"), ","))
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, schema.Devices{Items: items})
}

func (c *DeviceController) Search(ctx *echo.Context) error {
	items, err := c.usecase.Search(ctx.Request().Context(), strings.Split(ctx.QueryParam("imeis"), ","))
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, schema.DeviceReferences{Items: items})
}
