package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Load reads a YAML config file at path, applies defaults, then overlays
// any MATFIX_* environment variables on top.
func Load(path string) (*Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", path, err)
	}

	applyEnvOverrides(cfg)

	resolvePaths(path, cfg)

	return cfg, nil
}

func defaults() *Config {
	return &Config{
		Server: ServerConfig{
			BindAddr: ":8080",
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			Path:   "matfix.db",
		},
		Admin: AdminConfig{
			SocketPath: "/run/matfix/admin.sock",
		},
		RetryPolicy: RetryPolicyConfig{
			MaxRetries:          5,
			BackoffPolicy:       BackoffPolicyExponential,
			InitialInterval:     Duration(time.Second),
			MaxInterval:         Duration(5 * time.Minute),
			DeadLetterThreshold: 5,
		},
		Queue: QueueConfig{
			Concurrency: 4,
			DepthLimit:  1000,
		},
		Crypto: CryptoConfig{
			TrustPolicy:           TrustPolicyTOFU,
			UndecryptableHandling: "log",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: LogFormatJSON,
			Redact: true,
		},
		Observability: ObservabilityConfig{
			MetricsBind: ":9090",
			HealthPath:  "/health",
		},
		Webhooks: WebhookConfig{
			File:           "webhooks.yaml",
			Concurrency:    4,
			DefaultTimeout: Duration(10 * time.Second),
		},
	}
}

// applyEnvOverrides maps MATFIX_* environment variables onto config fields.
// Only variables that are explicitly set override the loaded/default value.
func applyEnvOverrides(c *Config) {
	if v, ok := os.LookupEnv("MATFIX_SERVER_BIND_ADDR"); ok {
		c.Server.BindAddr = v
	}
	if v, ok := os.LookupEnv("MATFIX_DATABASE_DRIVER"); ok {
		c.Database.Driver = v
	}
	if v, ok := os.LookupEnv("MATFIX_DATABASE_PATH"); ok {
		c.Database.Path = v
	}
	if v, ok := os.LookupEnv("MATFIX_ADMIN_SOCKET_PATH"); ok {
		c.Admin.SocketPath = v
	}
	if v, ok := os.LookupEnv("MATFIX_LOGGING_LEVEL"); ok {
		c.Logging.Level = v
	}
	if v, ok := os.LookupEnv("MATFIX_LOGGING_FORMAT"); ok {
		c.Logging.Format = LogFormat(v)
	}
	if v, ok := os.LookupEnv("MATFIX_OBSERVABILITY_METRICS_BIND"); ok {
		c.Observability.MetricsBind = v
	}
	if v, ok := os.LookupEnv("MATFIX_OBSERVABILITY_HEALTH_PATH"); ok {
		c.Observability.HealthPath = v
	}
	if v, ok := os.LookupEnv("MATFIX_WEBHOOKS_FILE"); ok {
		c.Webhooks.File = v
	}
}

// resolvePaths resolves relative file paths in cfg against the directory of configPath.
// Absolute paths are left unchanged.
func resolvePaths(configPath string, cfg *Config) {
	configDir := filepath.Dir(configPath)
	if cfg.Webhooks.File == "" {
		cfg.Webhooks.File = "webhooks.yaml"
	}
	if !filepath.IsAbs(cfg.Webhooks.File) {
		cfg.Webhooks.File = filepath.Join(configDir, cfg.Webhooks.File)
	}
}
