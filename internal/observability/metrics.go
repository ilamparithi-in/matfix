package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// # Metric definitions
//
// All metrics are registered with the default prometheus registry via promauto
// so they are automatically included in the /metrics scrape endpoint.

var (
	// Queue metrics

	// QueueDepth tracks the number of jobs currently in the outbound queue,
	// partitioned by account and job state.
	QueueDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "matfix",
		Name:      "queue_depth",
		Help:      "Current number of jobs in the outbound queue by state.",
	}, []string{"account_id", "state"})

	// RetryTotal counts outbound message retry attempts.
	RetryTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "matfix",
		Name:      "retry_total",
		Help:      "Total number of outbound message retry attempts.",
	}, []string{"account_id"})

	// DeadLetterTotal counts jobs moved to dead-letter status.
	DeadLetterTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "matfix",
		Name:      "dead_letter_total",
		Help:      "Total number of jobs moved to dead-letter after exhausting retries.",
	}, []string{"account_id"})

	// Subscription metrics

	// ActiveSubscriptions tracks the number of live correlation subscriptions.
	ActiveSubscriptions = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "matfix",
		Name:      "active_subscriptions",
		Help:      "Current number of active ask/receive correlation subscriptions.",
	}, []string{"type"}) // type: "ask" | "receive"

	// Event throughput metrics

	// InboundEventsTotal counts inbound Matrix events processed by the sync loop.
	InboundEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "matfix",
		Name:      "inbound_events_total",
		Help:      "Total number of inbound Matrix events processed.",
	}, []string{"account_id", "event_type"})

	// OutboundEventsTotal counts Matrix events successfully sent.
	OutboundEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "matfix",
		Name:      "outbound_events_total",
		Help:      "Total number of Matrix events successfully sent.",
	}, []string{"account_id"})

	// Sync metrics

	// SyncLatency records the round-trip duration of each /sync HTTP request.
	SyncLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "matfix",
		Name:      "sync_latency_seconds",
		Help:      "Duration of /sync HTTP requests in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"account_id"})

	// SyncErrorsTotal counts /sync request failures.
	SyncErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "matfix",
		Name:      "sync_errors_total",
		Help:      "Total number of /sync request errors.",
	}, []string{"account_id"})

	// Encryption metrics

	// EncryptionFailuresTotal counts E2EE decryption failures.
	EncryptionFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "matfix",
		Name:      "encryption_failures_total",
		Help:      "Total number of E2EE decryption failures.",
	}, []string{"account_id"})

	// API metrics

	// APIRequestsTotal counts HTTP API requests by route and status code.
	APIRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "matfix",
		Name:      "api_requests_total",
		Help:      "Total number of HTTP API requests.",
	}, []string{"route", "method", "status_code"})

	// APIRequestDuration records the latency of HTTP API requests.
	APIRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "matfix",
		Name:      "api_request_duration_seconds",
		Help:      "Duration of HTTP API requests in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"route", "method"})

	// RateLimitTotal counts requests rejected by the rate limiter.
	RateLimitTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "matfix",
		Name:      "rate_limit_total",
		Help:      "Total number of requests rejected by the rate limiter.",
	}, []string{"key_id"})
)
