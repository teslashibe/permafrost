package telemetry

import (
	"bytes"
	"strings"
	"testing"

	"github.com/teslashibe/permafrost/internal/config"
)

func TestNewLogger_DevDefaultsToText(t *testing.T) {
	var buf bytes.Buffer
	log := NewLoggerTo(&buf, config.LoggingConfig{Level: "info"}, config.EnvDev)
	log.Info("hello", "k", "v")
	out := buf.String()
	if !strings.Contains(out, "hello") {
		t.Fatalf("expected log line, got %q", out)
	}
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("dev logger should be text, got JSON: %q", out)
	}
}

func TestNewLogger_ProdDefaultsToJSON(t *testing.T) {
	var buf bytes.Buffer
	log := NewLoggerTo(&buf, config.LoggingConfig{Level: "info"}, config.EnvProd)
	log.Info("hello")
	if !strings.HasPrefix(strings.TrimSpace(buf.String()), "{") {
		t.Fatalf("prod logger should default to JSON, got %q", buf.String())
	}
}

func TestNewLogger_LevelFilter(t *testing.T) {
	var buf bytes.Buffer
	log := NewLoggerTo(&buf, config.LoggingConfig{Level: "warn"}, config.EnvDev)
	log.Info("ignored")
	if buf.Len() != 0 {
		t.Fatalf("info should be filtered at warn level, got %q", buf.String())
	}
	log.Warn("kept")
	if !strings.Contains(buf.String(), "kept") {
		t.Fatalf("warn should be emitted, got %q", buf.String())
	}
}
