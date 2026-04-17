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
	Env       Env             `mapstructure:"env"`
	Server    ServerConfig    `mapstructure:"server"`
	Logging   LoggingConfig   `mapstructure:"logging"`
	Database  DatabaseConfig  `mapstructure:"database"`
	Inference InferenceConfig `mapstructure:"inference"`
	Wallet    WalletConfig    `mapstructure:"wallet"`
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

// InferenceConfig holds the named OpenAI-compatible providers and the default
// provider used when one is not specified.
type InferenceConfig struct {
	Default   string                            `mapstructure:"default"`
	Providers map[string]InferenceProviderConfig `mapstructure:"providers"`
}

// InferenceProviderConfig is one named provider entry. APIKeyEnv is the name
// of an environment variable holding the API key (empty for no-auth providers
// like Ollama). RequestTimeoutSecs is optional (default 60).
type InferenceProviderConfig struct {
	BaseURL            string `mapstructure:"base_url"`
	APIKeyEnv          string `mapstructure:"api_key_env"`
	APIKey             string `mapstructure:"api_key"` // direct value (use sparingly)
	RequestTimeoutSecs int    `mapstructure:"request_timeout_secs"`
}

// WalletConfig configures the local encrypted keystore.
type WalletConfig struct {
	KeystorePath  string `mapstructure:"keystore_path"`   // defaults to ~/.permafrost/keystore.json
	PassphraseEnv string `mapstructure:"passphrase_env"`  // env var holding the passphrase
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
	v.SetDefault("wallet.passphrase_env", "PERMAFROST_KEYSTORE_PASSPHRASE")

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
