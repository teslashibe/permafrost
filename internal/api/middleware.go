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

// authMiddleware enforces a static bearer token. Only mounted when the API
// is bound to a non-loopback address; loopback runs are trusted in v1
// (single-operator local-first model).
func authMiddleware(token string) fiber.Handler {
	expected := []byte("Bearer " + token)
	return func(c *fiber.Ctx) error {
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
