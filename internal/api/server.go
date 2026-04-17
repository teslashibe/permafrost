// Package api hosts the Fiber HTTP server and route registration.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/google/uuid"

	"github.com/teslashibe/permafrost/internal/config"
	"github.com/teslashibe/permafrost/internal/store"
)

// Server bundles the Fiber app and its dependencies.
type Server struct {
	app *fiber.App
	cfg *config.Config
	log *slog.Logger
	db  *store.DB
}

// NewServer constructs the server. db may be nil; the health endpoint will
// then report database state as "unconfigured".
func NewServer(cfg *config.Config, log *slog.Logger, db *store.DB) *Server {
	app := fiber.New(fiber.Config{
		AppName:               "permafrostd",
		DisableStartupMessage: true,
		ReadTimeout:           15 * time.Second,
		WriteTimeout:          15 * time.Second,
		IdleTimeout:           60 * time.Second,
	})

	s := &Server{app: app, cfg: cfg, log: log, db: db}
	s.registerMiddleware()
	s.registerRoutes()
	return s
}

func (s *Server) registerMiddleware() {
	s.app.Use(recover.New())
	s.app.Use(requestid.New(requestid.Config{
		Header: fiber.HeaderXRequestID,
		Generator: func() string {
			return uuid.NewString()
		},
	}))
	s.app.Use(slogMiddleware(s.log))
	if !s.cfg.IsLoopback() {
		s.app.Use(authMiddleware(s.cfg.Server.AuthToken))
	}
}

// Listen starts the HTTP server on the configured bind address.
// It blocks until the context is cancelled or the server errors.
func (s *Server) Listen(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("api listening", "bind", s.cfg.Server.Bind)
		if err := s.app.Listen(s.cfg.Server.Bind); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.log.Info("api shutting down")
		if err := s.app.ShutdownWithContext(shutdownCtx); err != nil {
			return err
		}
		return <-errCh
	case err := <-errCh:
		return err
	}
}

// App exposes the underlying fiber app — primarily for tests that want to
// drive requests via app.Test().
func (s *Server) App() *fiber.App { return s.app }
