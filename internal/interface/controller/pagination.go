package controller

import (
	"github.com/labstack/echo/v5"
	"strconv"
)

func pagination(ctx *echo.Context) (int, int, error) {
	limit, offset := 100, 0
	var err error
	if value := ctx.QueryParam("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 500 {
			return 0, 0, echo.NewHTTPError(http.StatusBadRequest, "limit must be 1 to 500")
		}
	}
	if value := ctx.QueryParam("offset"); value != "" {
		offset, err = strconv.Atoi(value)
		if err != nil || offset < 0 || offset > 1000000 {
			return 0, 0, echo.NewHTTPError(http.StatusBadRequest, "offset must be 0 to 1000000")
		}
	}
	return limit, offset, nil
}
