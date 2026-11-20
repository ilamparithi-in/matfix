package webhook

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ilamparithi-in/matfix/internal/correlation"
)

// WebhookRule represents a user-defined outbound webhook trigger.
type WebhookRule struct {
	ID          string                 `yaml:"id" json:"id"`
	URL         string                 `yaml:"url" json:"url"`
	Secret      string                 `yaml:"secret,omitempty" json:"secret,omitempty"`
	Headers     map[string]string      `yaml:"headers,omitempty" json:"headers,omitempty"`
	Timeout     string                 `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Filter      correlation.FilterNode `yaml:"filter" json:"filter"`
	RetryPolicy RetryPolicyConfig      `yaml:"retry_policy,omitempty" json:"retry_policy,omitempty"`
}

// RetryPolicyConfig defines the backoff policy for failed webhook deliveries.
type RetryPolicyConfig struct {
	MaxRetries      int    `yaml:"max_retries,omitempty" json:"max_retries,omitempty"`
	InitialInterval string `yaml:"initial_interval,omitempty" json:"initial_interval,omitempty"`
	MaxInterval     string `yaml:"max_interval,omitempty" json:"max_interval,omitempty"`
}

// WebhooksFile is the top-level structure of webhooks.yaml.
type WebhooksFile struct {
	Webhooks []WebhookRule `yaml:"webhooks" json:"webhooks"`
}

// Validate checks a WebhookRule for correctness.
func (r *WebhookRule) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return errors.New("webhook id is required")
	}
	if strings.TrimSpace(r.URL) == "" {
		return errors.New("webhook url is required")
	}
	parsedURL, err := url.ParseRequestURI(r.URL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return fmt.Errorf("invalid webhook url %q: must be http or https", r.URL)
	}

	if r.Timeout != "" {
		if _, err := time.ParseDuration(r.Timeout); err != nil {
			return fmt.Errorf("invalid timeout %q: %w", r.Timeout, err)
		}
	}

	if r.RetryPolicy.InitialInterval != "" {
		if _, err := time.ParseDuration(r.RetryPolicy.InitialInterval); err != nil {
			return fmt.Errorf("invalid initial_interval %q: %w", r.RetryPolicy.InitialInterval, err)
		}
	}
	if r.RetryPolicy.MaxInterval != "" {
		if _, err := time.ParseDuration(r.RetryPolicy.MaxInterval); err != nil {
			return fmt.Errorf("invalid max_interval %q: %w", r.RetryPolicy.MaxInterval, err)
		}
	}

	if r.RetryPolicy.MaxRetries < 0 {
		return errors.New("max_retries must be non-negative")
	}

	// Validate the FilterNode using correlation's validator
	if err := correlation.Validate(&r.Filter); err != nil {
		return fmt.Errorf("invalid webhook filter: %w", err)
	}

	return nil
}

// TimeoutDuration returns the effective request timeout duration.
func (r *WebhookRule) TimeoutDuration(defaultTimeout time.Duration) time.Duration {
	if r.Timeout != "" {
		if d, err := time.ParseDuration(r.Timeout); err == nil && d > 0 {
			return d
		}
	}
	if defaultTimeout > 0 {
		return defaultTimeout
	}
	return 10 * time.Second
}

// InitialIntervalDuration returns the initial retry backoff duration.
func (r *WebhookRule) InitialIntervalDuration() time.Duration {
	if r.RetryPolicy.InitialInterval != "" {
		if d, err := time.ParseDuration(r.RetryPolicy.InitialInterval); err == nil && d > 0 {
			return d
		}
	}
	return 5 * time.Second
}

// MaxIntervalDuration returns the upper bound retry backoff duration.
func (r *WebhookRule) MaxIntervalDuration() time.Duration {
	if r.RetryPolicy.MaxInterval != "" {
		if d, err := time.ParseDuration(r.RetryPolicy.MaxInterval); err == nil && d > 0 {
			return d
		}
	}
	return 1 * time.Hour
}

// EffectiveMaxRetries returns the max retry attempts.
func (r *WebhookRule) EffectiveMaxRetries() int {
	if r.RetryPolicy.MaxRetries > 0 {
		return r.RetryPolicy.MaxRetries
	}
	return 5
}
