package server

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

func newAuthMiddleware(token string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if token == "" {
				return next(c)
			}
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing Authorization header")
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				return echo.NewHTTPError(http.StatusUnauthorized, "malformed Authorization header")
			}

			if parts[1] != token {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid token")
			}

			return next(c)
		}
	}
}
