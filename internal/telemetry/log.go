// Package telemetry provides logging primitives for Permafrost.
//
// We standardise on log/slog from the standard library. Format defaults to
// human-readable text in dev and JSON in prod, but can be overridden in
// configuration.
package telemetry

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/teslashibe/permafrost/internal/config"
)

// NewLogger constructs a *slog.Logger from a Config.
func NewLogger(cfg config.LoggingConfig, env config.Env) *slog.Logger {
	return NewLoggerTo(os.Stdout, cfg, env)
}

// NewLoggerTo is like NewLogger but writes to the supplied writer (useful for tests).
func NewLoggerTo(w io.Writer, cfg config.LoggingConfig, env config.Env) *slog.Logger {
	level := parseLevel(cfg.Level)

	format := strings.ToLower(cfg.Format)
	if format == "" {
		if env == config.EnvProd {
			format = "json"
		} else {
			format = "text"
		}
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	switch format {
	case "json":
		handler = slog.NewJSONHandler(w, opts)
	default:
		handler = slog.NewTextHandler(w, opts)
	}
	return slog.New(handler)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
