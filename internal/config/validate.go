package config

import (
	"errors"
	"fmt"
	"strings"
)

// Validate performs fail-fast startup validation of the configuration.
// It collects all problems before returning so the operator sees every issue at once.
func Validate(c *Config) error {
	var errs []string

	if len(c.Accounts) == 0 {
		errs = append(errs, "at least one account must be configured under 'accounts'")
	}

	for i, a := range c.Accounts {
		if a.ID == "" {
			errs = append(errs, fmt.Sprintf("accounts[%d]: id is required", i))
		}
		if a.HomeserverURL == "" {
			errs = append(errs, fmt.Sprintf("accounts[%d]: homeserver_url is required", i))
		}
		if a.UserID == "" {
			errs = append(errs, fmt.Sprintf("accounts[%d]: user_id is required", i))
		}
		if a.AccessToken == "" {
			errs = append(errs, fmt.Sprintf("accounts[%d]: access_token is required", i))
		}
	}

	if c.Server.BindAddr == "" {
		errs = append(errs, "server.bind_addr is required")
	}

	if c.Database.Path == "" {
		errs = append(errs, "database.path is required")
	}

	if c.Admin.SocketPath == "" {
		errs = append(errs, "admin.socket_path is required")
	}

	if c.RetryPolicy.MaxRetries < 0 {
		errs = append(errs, "retry_policy.max_retries must be non-negative")
	}

	if c.Queue.Concurrency <= 0 {
		errs = append(errs, "queue.concurrency must be greater than zero")
	}

	switch c.Crypto.TrustPolicy {
	case TrustPolicyTOFU, TrustPolicyAllowlist:
	default:
		errs = append(errs, fmt.Sprintf("crypto.trust_policy must be %q or %q", TrustPolicyTOFU, TrustPolicyAllowlist))
	}

	switch c.Logging.Format {
	case LogFormatJSON, LogFormatText:
	default:
		errs = append(errs, fmt.Sprintf("logging.format must be %q or %q", LogFormatJSON, LogFormatText))
	}

	if len(errs) > 0 {
		return errors.New("config validation failed:\n  " + strings.Join(errs, "\n  "))
	}
	return nil
}
