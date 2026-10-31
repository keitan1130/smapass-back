package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
	"smapass-back/internal/interface/schema"
	"smapass-back/internal/usecase"
)

type CertificateController struct{ usecase *usecase.CertificateUsecase }

func NewCertificateController(u *usecase.CertificateUsecase) *CertificateController {
	return &CertificateController{usecase: u}
}

func (c *CertificateController) Get(ctx *echo.Context) error {
	result, err := c.usecase.Get(ctx.Request().Context(), ctx.Param("number"))
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, result)
}

func (c *CertificateController) List(ctx *echo.Context) error {
	result, err := c.usecase.List(ctx.Request().Context(), strings.Split(ctx.QueryParam("ids"), ","))
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, schema.Certificates{Items: result})
}

func (c *CertificateController) Records(ctx *echo.Context) error {
	limit, offset := 100, 0
	var err error
	if value := ctx.QueryParam("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 500 {
			return echo.NewHTTPError(http.StatusBadRequest, "limit must be 1 to 500")
		}
	}
	if value := ctx.QueryParam("offset"); value != "" {
		offset, err = strconv.Atoi(value)
		if err != nil || offset < 0 || offset > 1000000 {
			return echo.NewHTTPError(http.StatusBadRequest, "offset must be 0 to 1000000")
		}
	}
	items, total, err := c.usecase.Records(ctx.Request().Context(), ctx.Param("number"), limit, offset)
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, schema.Records{Items: items, Total: total, Limit: limit, Offset: offset})
}
