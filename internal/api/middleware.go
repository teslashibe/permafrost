package api

import (
	"crypto/subtle"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
)

// slogMiddleware logs each request after it completes with a request ID,
// duration, status, method, and path. Any panics are converted to 500s
// upstream by recover.New().
func slogMiddleware(log *slog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		log.Info("http",
			"request_id", c.Get(fiber.HeaderXRequestID),
			"method", c.Method(),
			"path", c.Path(),
			"status", c.Response().StatusCode(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return err
	}
}

// publicRoutes are exempt from auth even when the API is bound to a
// non-loopback address. /v1/health is the only one for v1 — Docker /
// load-balancer healthchecks must not require credentials.
var publicRoutes = map[string]struct{}{
	"/v1/health": {},
}

// authMiddleware enforces a static bearer token. Only mounted when the API
// is bound to a non-loopback address; loopback runs are trusted in v1
// (single-operator local-first model). Routes in publicRoutes are always
// allowed.
func authMiddleware(token string) fiber.Handler {
	expected := []byte("Bearer " + token)
	return func(c *fiber.Ctx) error {
		if _, ok := publicRoutes[c.Path()]; ok {
			return c.Next()
		}
		if token == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "auth token not configured")
		}
		got := c.Get(fiber.HeaderAuthorization)
		if subtle.ConstantTimeCompare([]byte(got), expected) != 1 {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid auth token")
		}
		return c.Next()
	}
}
