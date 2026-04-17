// Package config loads runtime configuration from yaml + environment variables.
//
// Configuration precedence (highest to lowest):
//  1. environment variables prefixed with PERMAFROST_ (dots replaced with __)
//  2. values in the chosen config file
//  3. defaults set in this package
package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Env string

const (
	EnvDev  Env = "dev"
	EnvProd Env = "prod"
)

type Config struct {
	Env      Env            `mapstructure:"env"`
	Server   ServerConfig   `mapstructure:"server"`
	Logging  LoggingConfig  `mapstructure:"logging"`
	Database DatabaseConfig `mapstructure:"database"`
}

type ServerConfig struct {
	Bind      string `mapstructure:"bind"`
	AuthToken string `mapstructure:"auth_token"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level"`  // debug | info | warn | error
	Format string `mapstructure:"format"` // "" (auto) | text | json
}

type DatabaseConfig struct {
	URL      string `mapstructure:"url"`
	MaxConns int32  `mapstructure:"max_conns"`
	MinConns int32  `mapstructure:"min_conns"`
}

// Load reads configuration from the given path (optional) and environment.
// If path is empty, it looks for config.yaml in the current directory.
func Load(path string) (*Config, error) {
	v := viper.New()

	v.SetDefault("env", string(EnvDev))
	v.SetDefault("server.bind", "127.0.0.1:8080")
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "")
	v.SetDefault("database.url", "postgres://permafrost:permafrost@localhost:5432/permafrost?sslmode=disable")
	v.SetDefault("database.max_conns", int32(16))
	v.SetDefault("database.min_conns", int32(2))

	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
	}

	v.SetEnvPrefix("PERMAFROST")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "__"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, fmt.Errorf("read config: %w", err)
		}
		// no config file is OK — defaults + env are valid
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	switch c.Env {
	case EnvDev, EnvProd:
	default:
		return fmt.Errorf("invalid env %q: must be dev or prod", c.Env)
	}
	if c.Server.Bind == "" {
		return errors.New("server.bind is required")
	}
	if c.Database.URL == "" {
		return errors.New("database.url is required")
	}
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid logging.level %q", c.Logging.Level)
	}
	switch c.Logging.Format {
	case "", "text", "json":
	default:
		return fmt.Errorf("invalid logging.format %q", c.Logging.Format)
	}
	return nil
}

// IsLoopback reports whether the configured bind address is a loopback
// interface. The auth token is only required for non-loopback binds.
func (c *Config) IsLoopback() bool {
	host := c.Server.Bind
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	switch host {
	case "127.0.0.1", "::1", "localhost", "":
		return true
	}
	return false
}
