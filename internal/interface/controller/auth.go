package controller

import (
	"github.com/labstack/echo/v5"
	"net/http"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/interface/auth"
	"smapass-back/internal/interface/schema"
	"smapass-back/internal/usecase"
)

type AuthController struct{ usecase *usecase.AuthUsecase }

func NewAuthController(u *usecase.AuthUsecase) *AuthController { return &AuthController{usecase: u} }
func (c *AuthController) Enroll(ctx *echo.Context) error {
	var input entity.EnrollDevice
	if err := decodeDevice(ctx, &input); err != nil {
		return err
	}
	pair, err := c.usecase.Enroll(ctx.Request().Context(), input)
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusCreated, pair)
}
func (c *AuthController) Refresh(ctx *echo.Context) error {
	var input entity.RefreshRequest
	if err := decodeDevice(ctx, &input); err != nil {
		return err
	}
	pair, err := c.usecase.Refresh(ctx.Request().Context(), input)
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, pair)
}
func (c *AuthController) Revoke(ctx *echo.Context) error {
	var input entity.RefreshRequest
	if err := decodeDevice(ctx, &input); err != nil {
		return err
	}
	if err := c.usecase.Revoke(ctx.Request().Context(), input); err != nil {
		return err
	}
	return ctx.NoContent(http.StatusNoContent)
}
func (c *AuthController) Session(ctx *echo.Context) error {
	p, ok := auth.Principal(ctx.Request().Context())
	if !ok {
		return entity.ErrUnauthenticated
	}
	return ctx.JSON(http.StatusOK, schema.Session{IMEI: p.IMEI})
}
