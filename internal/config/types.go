package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// TrustPolicy controls how the Crypto Manager verifies device keys.
type TrustPolicy string

const (
	TrustPolicyTOFU      TrustPolicy = "tofu"
	TrustPolicyAllowlist TrustPolicy = "allowlist"
)

// BackoffPolicy controls the retry interval growth strategy.
type BackoffPolicy string

const (
	BackoffPolicyExponential BackoffPolicy = "exponential"
	BackoffPolicyLinear      BackoffPolicy = "linear"
)

// LogFormat selects structured log output format.
type LogFormat string

const (
	LogFormatJSON LogFormat = "json"
	LogFormatText LogFormat = "text"
)

// Duration wraps time.Duration to support YAML unmarshaling from human-readable strings (e.g. "5s", "1m").
type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	dur, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value.Value, err)
	}
	*d = Duration(dur)
	return nil
}

// D returns the underlying time.Duration value.
func (d Duration) D() time.Duration {
	return time.Duration(d)
}
