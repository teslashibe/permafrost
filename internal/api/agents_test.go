package api

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/teslashibe/permafrost/internal/config"
	"github.com/teslashibe/permafrost/internal/telemetry"
)

// TestListAgents_NoStore proves the handler returns 503 (not crash, not
// leaking nil) when the daemon was started without a database.
func TestListAgents_NoStore(t *testing.T) {
	cfg := &config.Config{
		Env:    config.EnvDev,
		Server: config.ServerConfig{Bind: "127.0.0.1:0"},
	}
	log := telemetry.NewLogger(config.LoggingConfig{Level: "error"}, config.EnvDev)
	s := NewServer(cfg, log, nil)

	req := httptest.NewRequest("GET", "/v1/agents", nil)
	resp, err := s.App().Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 503 {
		t.Errorf("expected 503 when no DB, got %d body=%s", resp.StatusCode, body)
	}
}

func TestListDecisions_NoStore(t *testing.T) {
	cfg := &config.Config{
		Env:    config.EnvDev,
		Server: config.ServerConfig{Bind: "127.0.0.1:0"},
	}
	log := telemetry.NewLogger(config.LoggingConfig{Level: "error"}, config.EnvDev)
	s := NewServer(cfg, log, nil)

	req := httptest.NewRequest("GET", "/v1/agents/ag_x/decisions", nil)
	resp, err := s.App().Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Errorf("expected 503 when no DB, got %d", resp.StatusCode)
	}
}

// TestAgentsRouteGatedByAuthOnNonLoopback proves the new routes are
// behind the auth middleware (i.e. they are NOT in publicRoutes).
func TestAgentsRouteGatedByAuthOnNonLoopback(t *testing.T) {
	cfg := &config.Config{
		Env:    config.EnvDev,
		Server: config.ServerConfig{Bind: "0.0.0.0:0"},
	}
	log := telemetry.NewLogger(config.LoggingConfig{Level: "error"}, config.EnvDev)
	s := NewServer(cfg, log, nil)

	req := httptest.NewRequest("GET", "/v1/agents", nil)
	resp, err := s.App().Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401 on /v1/agents without token, got %d", resp.StatusCode)
	}
}
