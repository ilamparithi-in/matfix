package config

// Config is the top-level configuration for matfix.
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Accounts      []AccountConfig     `yaml:"accounts"`
	Database      DatabaseConfig      `yaml:"database"`
	Admin         AdminConfig         `yaml:"admin"`
	RetryPolicy   RetryPolicyConfig   `yaml:"retry_policy"`
	Queue         QueueConfig         `yaml:"queue"`
	Crypto        CryptoConfig        `yaml:"crypto"`
	Logging       LoggingConfig       `yaml:"logging"`
	Observability ObservabilityConfig `yaml:"observability"`
}

// ServerConfig controls the HTTP API listener.
type ServerConfig struct {
	BindAddr string `yaml:"bind_addr"`
}

// AccountConfig describes a single Matrix account managed by the relay.
type AccountConfig struct {
	ID            string `yaml:"id"`
	HomeserverURL string `yaml:"homeserver_url"`
	UserID        string `yaml:"user_id"`
	AccessToken   string `yaml:"access_token"`
	DeviceID      string `yaml:"device_id"`
}

// DatabaseConfig selects the persistence back-end.
type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	Path   string `yaml:"path"`
}

// AdminConfig controls the privileged admin UNIX socket.
type AdminConfig struct {
	SocketPath string `yaml:"socket_path"`
}

// RetryPolicyConfig governs outbound delivery retries.
type RetryPolicyConfig struct {
	MaxRetries          int           `yaml:"max_retries"`
	BackoffPolicy       BackoffPolicy `yaml:"backoff_policy"`
	InitialInterval     Duration      `yaml:"initial_interval"`
	MaxInterval         Duration      `yaml:"max_interval"`
	DeadLetterThreshold int           `yaml:"dead_letter_threshold"`
}

// QueueConfig controls the outbound worker pool.
type QueueConfig struct {
	Concurrency int `yaml:"concurrency"`
	DepthLimit  int `yaml:"depth_limit"`
}

// CryptoConfig controls E2EE behaviour.
type CryptoConfig struct {
	TrustPolicy           TrustPolicy `yaml:"trust_policy"`
	UndecryptableHandling string      `yaml:"undecryptable_handling"`
}

// LoggingConfig controls log output.
type LoggingConfig struct {
	Level  string    `yaml:"level"`
	Format LogFormat `yaml:"format"`
	Redact bool      `yaml:"redact"`
}

// ObservabilityConfig controls metrics and health endpoints.
type ObservabilityConfig struct {
	MetricsBind string `yaml:"metrics_bind"`
	HealthPath  string `yaml:"health_path"`
}
