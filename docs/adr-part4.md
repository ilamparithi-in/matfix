# Matrix Relay Daemon Specification - Part 4

Contains sections:
16. API Authentication and Authorization
17. Observability
18. Failure Semantics
19. Configuration Model
20. Extensibility Model

---

# 16. API Authentication and Authorization

The relay exposes application-facing APIs and therefore requires explicit authentication and authorization controls.

Authentication and authorization behavior must be independent from Matrix account authentication.

Applications authenticate to the relay, not directly to Matrix.

---

## Authentication Model

The relay uses API key authentication.

API keys are statically configured or administratively managed.

The relay does not expose anonymous APIs by default.

---

## API Key Structure

An API key conceptually represents:

* an application
* a service
* a user
* an automation workflow

API keys may contain associated policy metadata.

---

## API Key Permissions

API keys may define:

* allowed Matrix accounts
* allowed routes
* rate limits
* allowed rooms
* allowed event types

Authorization decisions should occur before queue insertion or correlation registration.

---

## Account Restrictions

API keys may restrict access to specific Matrix accounts.

Example conceptual behavior:

* one API key may only send using `@alerts:example.org`
* another key may access multiple accounts

This prevents unrestricted sender impersonation inside the relay.

---

## Route Restrictions

API keys may restrict:

* send APIs
* receive APIs
* ask APIs
* administrative APIs

Example:

* send-only automation key
* receive-only monitoring key
* administrative inspection key

---

## Room Restrictions

Future authorization models may restrict:

* accessible rooms
* accessible room aliases
* DM creation permissions

This should be architecturally supported even if initially unimplemented.

---

## Event Type Restrictions

API keys may restrict:

* message sending
* reactions
* redactions
* media uploads
* administrative event types

This prevents misuse of privileged Matrix functionality.

---

## Rate Limiting

The relay should support:

* global rate limits
* per-key rate limits
* per-route rate limits
* per-account rate limits

Rate limiting should occur before expensive operations where practical.

---

## Rate Limit Failure Semantics

Rate-limited requests should:

* fail predictably
* return structured errors
* avoid queue insertion

Rate limiting should not terminate existing active workflows.

---

## Administrative APIs

Administrative APIs may expose:

* queue inspection
* active subscriptions
* retry inspection
* account state
* crypto state
* metrics

Administrative APIs should require elevated permissions.

---

## API Key Storage

API keys should:

* be securely stored
* avoid plaintext exposure in logs
* support rotation

Hashing or secure storage mechanisms are strongly recommended.

---

## Future Authentication Models

The architecture should permit future support for:

* mTLS
* OAuth
* JWT authentication
* UNIX socket trust models

without redesigning authorization internals.

---

# 17. Observability

The relay is infrastructure software and therefore requires strong observability support.

Operational visibility is critical for:

* debugging
* reliability
* monitoring
* incident response
* performance analysis

---

## Logging

The relay should use structured logging.

Structured logs should include:

* timestamps
* account identifiers
* event IDs
* queue IDs
* correlation IDs
* retry metadata
* failure reasons

---

## Logging Principles

Logs should:

* be machine-readable
* avoid ambiguity
* preserve operational context
* avoid leaking secrets

Sensitive information should never be logged unintentionally.

---

## Sensitive Data Handling

Logs should avoid exposing:

* API keys
* access tokens
* session keys
* encryption keys
* sensitive message bodies where avoidable

Redaction support is recommended.

---

## Metrics

The relay should expose metrics suitable for Prometheus-style monitoring.

Metrics may include:

* queue depth
* retry counts
* active subscriptions
* event throughput
* sync latency
* encryption failures
* API request counts
* rate limit triggers

---

## Queue Metrics

Queue-related metrics should expose:

* queued job count
* retry queue size
* dead-letter queue size
* processing latency
* retry frequency

Queue metrics are important for detecting:

* federation issues
* congestion
* stuck jobs

---

## Correlation Metrics

Ask/receive metrics may include:

* active asks
* active subscriptions
* timeout frequency
* correlation failures
* response latency

---

## Account Metrics

Per-account metrics may include:

* sync status
* connectivity status
* encryption readiness
* account availability
* retry rates

---

## Event Bus Metrics

Event Bus metrics may include:

* subscriber counts
* internal queue depth
* dropped events
* event throughput
* processing latency

---

## Health Checks

The relay may expose health endpoints.

Health checks may include:

* database availability
* queue availability
* account availability
* sync health
* crypto initialization state

---

## Readiness vs Liveness

The architecture should distinguish:

* liveness state
* readiness state

Example:

* relay process alive but no usable accounts
* relay alive but database unavailable
* relay alive but crypto initialization incomplete

---

## Tracing

The architecture should permit future distributed tracing support.

Potential tracing targets include:

* outbound delivery flow
* retry flow
* ask-response flow
* sync processing
* encryption operations

---

## Administrative Inspection

Administrative tooling may expose:

* active queue contents
* retry metadata
* dead-letter contents
* active subscriptions
* active ask requests
* account state

This is especially important for debugging complex workflows.

---

# 18. Failure Semantics

Infrastructure software must define explicit failure behavior.

Undefined failure semantics lead to:

* inconsistent recovery
* unreliable automation
* operational confusion

---

## Startup Failure Semantics

If all configured Matrix accounts are unusable:

* the relay must terminate
* the relay must return an appropriate exit code

Examples:

* authentication failure across all accounts
* homeserver unreachability across all accounts
* crypto initialization failure across all accounts

---

## Partial Failure Semantics

If at least one Matrix account remains operational:

* the relay continues running
* failed accounts are isolated
* failures are logged
* operational accounts continue serving requests

Partial failures must not unnecessarily terminate the relay.

---

## Queue Failure Semantics

Queue failures should:

* preserve persistent state where possible
* avoid uncontrolled duplicate sends
* avoid queue corruption

Critical unrecoverable queue corruption may require process termination.

---

## Database Failure Semantics

Database failures may require:

* degraded operation
* write suspension
* process termination

depending on severity.

Persistent state corruption is considered a critical failure.

---

## Sync Failure Semantics

Temporary sync failures should:

* trigger reconnect attempts
* preserve sync continuity
* avoid unnecessary process termination

Examples:

* network interruptions
* federation instability
* homeserver timeouts

---

## Crypto Failure Semantics

Encryption failures should:

* be logged
* expose structured metadata
* avoid crashing the relay where possible

Potential crypto failures:

* undecryptable events
* missing sessions
* trust failures
* device desynchronization

---

## Event Bus Failure Semantics

Failure in one event consumer should not:

* terminate unrelated consumers
* stop sync processing
* corrupt internal event flow

Internal failures should remain isolated where practical.

---

## Ask/Receive Failure Semantics

Potential failures include:

* timeout expiration
* invalid filters
* account unavailability
* subscription failures

Timeout expiration is not considered an internal relay failure.

---

## Federation Failure Semantics

Federation instability should:

* trigger retries
* preserve queue state
* avoid process termination

Federation failures are considered expected operational behavior.

---

## Restart Recovery

After restart, the relay must restore:

* queue state
* sync state
* crypto state
* retry state
* correlation state where practical

Recovery behavior should minimize:

* duplicate delivery
* orphaned jobs
* sync discontinuity

---

## Duplicate Delivery

Exactly-once delivery is not guaranteed.

The relay should instead aim for:

* best-effort deduplication
* idempotent workflows where practical

Duplicate delivery may still occur due to:

* federation retries
* reconnect behavior
* partial acknowledgements

---

## Crash Safety

Critical state transitions should be durable where practical.

Crash recovery should avoid:

* queue corruption
* orphaned retries
* lost sync state

---

# 19. Configuration Model

The relay uses persistent static configuration.

Configuration should remain explicit and predictable.

---

## Configuration Sources

The relay should support:

* static configuration files
* environment variable overrides

Environment overrides are useful for:

* container deployments
* orchestration systems
* secret injection

---

## Configuration Categories

Potential configuration sections include:

* homeserver configuration
* Matrix account configuration
* API configuration
* retry policies
* queue policies
* crypto policies
* logging configuration
* observability configuration

---

## Matrix Account Configuration

Matrix account configuration may include:

* homeserver URL
* user ID
* access token
* device ID
* crypto settings
* retry policies

Accounts are externally created and statically configured.

---

## API Configuration

API configuration may include:

* bind addresses
* TLS settings
* authentication configuration
* rate limits
* administrative API exposure

---

## Retry Configuration

Retry configuration may include:

* retry ceilings
* backoff policies
* cooldown intervals
* dead-letter policies

---

## Queue Configuration

Queue configuration may include:

* concurrency limits
* queue depth limits
* retention policies
* recovery behavior

---

## Crypto Configuration

Crypto configuration may include:

* trust policies
* verification strategies
* undecryptable event handling
* device trust rules

---

## Logging Configuration

Logging configuration may include:

* log levels
* structured output formats
* redaction policies
* log destinations

---

## Configuration Validation

Configuration should be validated during startup.

Invalid configuration should:

* fail startup
* produce explicit errors
* avoid partial undefined initialization

---

## Hot Reloading

Future support may permit:

* configuration reloading
* runtime policy updates
* dynamic observability changes

The architecture should permit this without major redesign.

---

# 20. Extensibility Model

The relay is designed for long-term extensibility.

The architecture should avoid assumptions that prevent future protocol or deployment evolution.

---

## Internal API Stability

Internal abstractions should:

* preserve extensibility
* avoid leaking SDK internals
* minimize tight coupling

Internal modules should communicate through stable interfaces where practical.

---

## Future Transport APIs

The architecture should permit future support for:

* WebSockets
* Server-Sent Events (SSE)
* gRPC
* streaming APIs

without redesigning the internal event system.

---

## Plugin Systems

The architecture should permit future plugin support.

Potential plugin use cases:

* custom routing
* custom correlation logic
* message transforms
* observability extensions
* policy engines

---

## Distributed Workers

Future versions may support:

* distributed queue workers
* clustered deployments
* horizontal scaling

The architecture should avoid assumptions requiring single-process-only execution.

---

## External Event Streaming

Future support may include:

* external event subscriptions
* streaming APIs
* webhook forwarding
* external event consumers

Internal event semantics should remain reusable for external streaming systems.

---

## Policy Engines

Future policy systems may support:

* routing policies
* trust policies
* moderation policies
* retry policies
* room restrictions

The architecture should permit centralized policy evaluation.

---

## Federation-Aware Routing

Future versions may optimize:

* remote homeserver selection
* retry scheduling
* federation cooldown behavior

This should remain architecturally possible.

---

## Templating

Future support may include:

* message templates
* structured notification rendering
* localization support

The relay should avoid assumptions that prevent templated workflows.

---

## Event Transformation

Future transformation systems may support:

* message rewriting
* metadata injection
* event enrichment
* filtering pipelines

Transformation systems should preserve Matrix compatibility.

---

## Webhook Compatibility

Future versions may support:

* webhook ingestion
* webhook-to-Matrix routing
* third-party integrations

This should remain compatible with the queue and correlation architecture.

---

## Long-Term Architectural Principle

Future extensibility must not compromise:

* reliability
* persistence guarantees
* Matrix semantic compatibility
* internal ownership boundaries
