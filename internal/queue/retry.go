package queue

import (
	"math"
	"time"

	"github.com/ilamparithi-in/matfix/internal/config"
)

// NextScheduledAt computes the earliest time at which a job should next be
// attempted, given the configured retry policy and the new (already-incremented)
// retry count.
func NextScheduledAt(policy config.RetryPolicyConfig, retryCount int) time.Time {
	var interval time.Duration
	switch policy.BackoffPolicy {
	case config.BackoffPolicyLinear:
		interval = policy.InitialInterval.D() * time.Duration(retryCount)
	default: // exponential
		multiplier := math.Pow(2, float64(retryCount-1))
		interval = time.Duration(float64(policy.InitialInterval.D()) * multiplier)
	}

	if max := policy.MaxInterval.D(); max > 0 && interval > max {
		interval = max
	}
	return time.Now().Add(interval)
}

// IsExhausted reports whether retryCount has reached or exceeded the configured
// maximum. A MaxRetries value of 0 means no retries are permitted; the first
// failed delivery moves the job to dead_letter.
func IsExhausted(policy config.RetryPolicyConfig, retryCount int) bool {
	return retryCount > policy.MaxRetries
}
