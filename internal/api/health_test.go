package api

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/teslashibe/permafrost/internal/config"
	"github.com/teslashibe/permafrost/internal/telemetry"
)

func TestHealthHandler_Unconfigured(t *testing.T) {
	cfg := &config.Config{
		Env:    config.EnvDev,
		Server: config.ServerConfig{Bind: "127.0.0.1:0"},
	}
	log := telemetry.NewLogger(config.LoggingConfig{Level: "error"}, config.EnvDev)
	s := NewServer(cfg, log, nil)

	req := httptest.NewRequest("GET", "/v1/health", nil)
	resp, err := s.App().Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Fatalf("status: got %d body=%s", resp.StatusCode, body)
	}
	var hr HealthResponse
	if err := json.Unmarshal(body, &hr); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	if hr.Status != "degraded" {
		t.Errorf("status: got %q want degraded (no DB)", hr.Status)
	}
	if hr.Checks["database"].Status != "unconfigured" {
		t.Errorf("database check: got %+v", hr.Checks["database"])
	}
	if hr.Service != "permafrostd" {
		t.Errorf("service: got %q", hr.Service)
	}
}
