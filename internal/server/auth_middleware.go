package server

import (
	"github.com/znowdev/reqbouncer/internal/client/auth"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

func newAuthMiddleware(githubProvider auth.GithubUserProvider) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {

			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing Authorization header")
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				return echo.NewHTTPError(http.StatusUnauthorized, "malformed Authorization header")
			}

			githubUser, err := githubProvider(parts[1])
			if err != nil {
				slog.Error("error getting user from github", "error", err)
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid token")
			}

			subdomain := c.Get("subdomain").(string)
			if !isLocalhost(subdomain) && strings.ToLower(githubUser.Login) != strings.ToLower(subdomain) {
				return echo.NewHTTPError(http.StatusUnauthorized, "user not allowed to access this subdomain")
			}

			return next(c)
		}
	}
}

func isLocalhost(host string) bool {
	return strings.HasPrefix(host, "localhost:") || strings.HasPrefix(host, "127.0.0.1:")
}
