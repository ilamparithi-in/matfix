package config_test

import (
	"os"
	"testing"

	"github.com/ilamparithi-in/matfix/internal/config"
)

func minimalValidConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{BindAddr: ":8080"},
		Accounts: []config.AccountConfig{
			{
				ID:            "main",
				HomeserverURL: "https://matrix.example.com",
				UserID:        "@bot:example.com",
				AccessToken:   "token123",
			},
		},
		Database: config.DatabaseConfig{Driver: "sqlite", Path: "test.db"},
		Admin:    config.AdminConfig{SocketPath: "/tmp/test.sock"},
		Queue:    config.QueueConfig{Concurrency: 1},
		Crypto:   config.CryptoConfig{TrustPolicy: config.TrustPolicyTOFU},
		Logging:  config.LoggingConfig{Format: config.LogFormatJSON},
	}
}

func TestValidate_Valid(t *testing.T) {
	if err := config.Validate(minimalValidConfig()); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidate_NoAccounts(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Accounts = nil
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for empty accounts, got nil")
	}
}

func TestValidate_MissingAccountID(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Accounts[0].ID = ""
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for missing account id, got nil")
	}
}

func TestValidate_MissingHomeserverURL(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Accounts[0].HomeserverURL = ""
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for missing homeserver_url, got nil")
	}
}

func TestValidate_MissingAccessToken(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Accounts[0].AccessToken = ""
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for missing access_token, got nil")
	}
}

func TestValidate_InvalidTrustPolicy(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Crypto.TrustPolicy = "unknown"
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for invalid trust_policy, got nil")
	}
}

func TestValidate_InvalidLogFormat(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Logging.Format = "xml"
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for invalid log format, got nil")
	}
}

func TestValidate_ZeroConcurrency(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Queue.Concurrency = 0
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for zero concurrency, got nil")
	}
}

func TestValidate_NegativeMaxRetries(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.RetryPolicy.MaxRetries = -1
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for negative max_retries, got nil")
	}
}

func writeMinimalYAML(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("", "matfix-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	_, _ = f.WriteString(`accounts:
  - id: test
    homeserver_url: https://matrix.example.com
    user_id: "@bot:example.com"
    access_token: token123
`)
	f.Close()
	return f.Name()
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := config.Load(writeMinimalYAML(t))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Server.BindAddr != ":8080" {
		t.Errorf("default bind_addr: got %q, want %q", cfg.Server.BindAddr, ":8080")
	}
	if cfg.Admin.SocketPath != "/run/matfix/admin.sock" {
		t.Errorf("default socket_path: got %q, want %q", cfg.Admin.SocketPath, "/run/matfix/admin.sock")
	}
	if cfg.Queue.Concurrency != 4 {
		t.Errorf("default concurrency: got %d, want 4", cfg.Queue.Concurrency)
	}
	if cfg.Crypto.TrustPolicy != config.TrustPolicyTOFU {
		t.Errorf("default trust_policy: got %q, want %q", cfg.Crypto.TrustPolicy, config.TrustPolicyTOFU)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("MATFIX_SERVER_BIND_ADDR", ":9999")
	t.Setenv("MATFIX_LOGGING_LEVEL", "debug")

	cfg, err := config.Load(writeMinimalYAML(t))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Server.BindAddr != ":9999" {
		t.Errorf("env override bind_addr: got %q, want %q", cfg.Server.BindAddr, ":9999")
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("env override logging level: got %q, want %q", cfg.Logging.Level, "debug")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := config.Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	f, err := os.CreateTemp("", "matfix-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	_, _ = f.WriteString(": invalid: yaml: {{{{")
	f.Close()

	_, err = config.Load(f.Name())
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}
