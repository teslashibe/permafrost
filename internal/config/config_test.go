package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Env != EnvDev {
		t.Errorf("Env: got %q want dev", cfg.Env)
	}
	if cfg.Server.Bind != "127.0.0.1:8080" {
		t.Errorf("Server.Bind: got %q", cfg.Server.Bind)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level: got %q", cfg.Logging.Level)
	}
	if !cfg.IsLoopback() {
		t.Errorf("IsLoopback: want true for default bind")
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	contents := []byte(`env: prod
server:
  bind: 0.0.0.0:9000
  auth_token: secret
logging:
  level: warn
  format: json
database:
  url: postgres://u:p@db:5432/x
  max_conns: 8
  min_conns: 1
`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Env != EnvProd {
		t.Errorf("Env: got %q", cfg.Env)
	}
	if cfg.Server.Bind != "0.0.0.0:9000" {
		t.Errorf("Bind: got %q", cfg.Server.Bind)
	}
	if cfg.IsLoopback() {
		t.Errorf("IsLoopback: want false for 0.0.0.0")
	}
	if cfg.Database.MaxConns != 8 {
		t.Errorf("MaxConns: got %d", cfg.Database.MaxConns)
	}
}

func TestLoadInvalidEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("env: staging\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for invalid env")
	}
}

func TestEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("PERMAFROST_SERVER__BIND", "0.0.0.0:1234")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Bind != "0.0.0.0:1234" {
		t.Errorf("Server.Bind: got %q want 0.0.0.0:1234", cfg.Server.Bind)
	}
}
