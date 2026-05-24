# Matrix Relay Daemon Specification - Part 2

Contains sections:
6. Event Flow Model
7. Persistence Specification
8. Multi-Account Architecture
9. Message Model
10. Sending API

---

# 6. Event Flow Model

This section defines how Matrix events move through the relay internally.

This is one of the most important architectural sections because incorrect event ownership causes:

* race conditions
* duplicate processing
* sync corruption
* broken correlation logic
* inconsistent retries
* crypto desynchronization

---

## Sync Ownership

The Sync Manager exclusively owns:

* Matrix `/sync` loops
* sync token advancement
* inbound event ingestion

No other component may directly consume Matrix sync streams.

This rule is mandatory.

Reasons:

* prevents multiple sync consumers
* prevents token corruption
* ensures deterministic event flow
* simplifies debugging
* simplifies replay logic
* simplifies correlation logic

---

## Inbound Event Lifecycle

Inbound Matrix events follow this lifecycle:

1. Matrix homeserver sends sync response
2. Sync Manager receives sync response
3. Sync Manager validates sync state
4. Sync Manager advances sync token
5. Sync Manager normalizes inbound events
6. Sync Manager publishes internal events
7. Internal subscribers consume relevant events

---

## Internal Event Distribution

Inbound events are distributed through the internal Event Bus.

Consumers subscribe internally for:

* message events
* encrypted events
* relation events
* membership events
* receipt events
* retry events
* queue events

Internal components must communicate through the Event Bus where possible instead of directly invoking each other.

This reduces coupling.

---

## Event Ordering Guarantees

### Per-Room Ordering

Per-room event ordering should be preserved where possible.

This is especially important for:

* replies
* edits
* reactions
* ask-response correlation
* retry reconciliation

---

### Global Ordering

Global ordering is not guaranteed.

Matrix federation itself does not guarantee globally consistent event ordering.

The relay must not assume globally ordered delivery semantics.

---

## Event Deduplication

The relay should support event deduplication.

Potential duplicate sources include:

* sync retries
* reconnections
* federation retries
* application retries

Deduplication should preferably use:

* event IDs
* transaction IDs
* internal correlation identifiers

---

## Internal Event Types

The Event Bus should support typed internal events.

Examples:

* inbound_message
* outbound_message
* retry_scheduled
* retry_exhausted
* ask_registered
* ask_resolved
* encryption_failure
* delivery_acknowledged

---

## Backpressure Handling

The Event Bus architecture must consider backpressure.

Potential strategies:

* bounded queues
* subscriber buffering
* overflow policies
* worker throttling

The relay should avoid unbounded in-memory event growth.

---

## Failure Isolation

Failure in one event consumer should not:

* stop sync processing
* block unrelated subscribers
* terminate the entire relay

Internal event consumers should fail independently where possible.

---

# 7. Persistence Specification

The relay is designed as a persistent stateful system.

Stateless operation is not a design goal.

Persistent storage is required for:

* encryption continuity
* retry persistence
* sync continuity
* correlation tracking
* queue durability

---

## Database Goals

The persistence layer must:

* use SQLite by default
* support future PostgreSQL compatibility
* avoid SQLite-specific SQL features where possible
* support schema migrations
* support transactional consistency
* support crash recovery

---

## Database Design Principles

### Durable State First

Critical operational state must survive:

* process restarts
* crashes
* temporary outages

---

### Minimal Volatile State

Volatile in-memory state should be minimized where practical.

Critical workflow state should be reconstructable from persistent storage.

---

### Transactional Integrity

Operations involving:

* queue transitions
* sync advancement
* correlation registration
* retry scheduling

should preferably occur transactionally where practical.

---

## Required Persistent State

### Sync State

Must persist:

* sync tokens
* next batch tokens
* sync metadata

Without persistent sync state:

* duplicate event processing occurs
* sync continuity breaks
* receive semantics become unreliable

---

### Crypto State

Must persist:

* device keys
* Olm sessions
* Megolm sessions
* device trust state
* verification metadata

Without persistent crypto state:

* encrypted rooms become unusable after restart
* device trust becomes inconsistent
* session renegotiation becomes excessive

---

### Queue State

Must persist:

* outbound jobs
* retry counters
* delivery state
* timestamps
* failure metadata
* dead-letter metadata

Queue persistence is mandatory.

---

### Correlation State

Must persist:

* active ask requests
* receive subscriptions
* timeout metadata
* matching filters
* response state

This allows correlation continuity after restart where practical.

---

### Event Cache

Architecturally supported but optional.

Potential uses:

* replay
* debugging
* deduplication
* observability
* historical inspection

The event cache should not become the canonical source of truth for Matrix room history.

---

## Schema Migration Strategy

The persistence layer must support:

* forward migrations
* schema version tracking
* startup migration execution

Destructive automatic migrations should be avoided where possible.

---

## Database Failure Handling

Database failures may require:

* degraded operation
* write suspension
* process termination

depending on severity.

Critical persistence corruption should terminate the relay.

---

# 8. Multi-Account Architecture

The relay supports multiple configured Matrix accounts.

Accounts are:

* externally created
* statically configured
* persistent

Dynamic account creation through APIs is not supported.

---

## Account Isolation

Each Matrix account maintains independent:

* sync state
* crypto state
* device store
* retry state
* queue ownership
* account metadata

Account failures should not corrupt unrelated accounts.

---

## Account Configuration

Accounts are configured statically.

Example conceptual structure:

```yaml id="jlwmgd"
accounts:
  - name: alerts
    homeserver: https://matrix.example.org
    user_id: "@alerts:example.org"

  - name: support
    homeserver: https://matrix.example.org
    user_id: "@support:example.org"
```

Actual configuration structure is implementation-defined.

---

## Account Lifecycle

Each account undergoes:

1. initialization
2. authentication
3. sync startup
4. crypto initialization
5. availability registration

Unavailable accounts must be tracked internally.

---

## Account Failure Semantics

### Total Failure

If all configured accounts are unusable or inaccessible:

* the relay must terminate
* the relay must return an appropriate exit code

Examples:

* all accounts unauthorized
* all homeservers unreachable during startup
* persistent crypto initialization failure across all accounts

---

### Partial Failure

If at least one account remains operational:

* the relay continues running
* failed accounts are logged
* failed accounts are marked unavailable internally

The relay should continue serving requests that can be fulfilled by operational accounts.

---

## Account Routing

Outbound requests may:

* explicitly specify sender accounts
* use default sender resolution
* later support policy-based routing

---

## Account Selection Policies

Potential future routing policies:

* round-robin
* room affinity
* fallback accounts
* capability-based routing
* encryption-capable routing

These are architecturally supported but not required initially.

---

## Per-Account Isolation Boundaries

Per-account boundaries should isolate:

* sync loops
* encryption sessions
* retry workers
* outbound queues where practical

This simplifies debugging and recovery.

---

# 9. Message Model

The relay internally preserves Matrix-native semantics wherever possible.

The relay should avoid inventing incompatible abstractions over Matrix events.

---

## Internal Representation

Internally, the relay should preserve:

* raw Matrix event metadata
* event IDs
* sender metadata
* timestamps
* relations
* encryption metadata

Internal systems should operate on normalized Matrix event structures instead of heavily simplified abstractions.

---

## External Representation

External APIs may expose simplified representations for convenience.

Examples:

* simplified message body
* simplified sender information
* simplified room references

Advanced APIs should still allow access to raw Matrix metadata.

---

## Supported Event Types

Architecturally supported event types include:

* text messages
* HTML-formatted messages
* replies
* reactions
* edits
* redactions
* attachments
* media events

Additional Matrix event types should remain extensible.

---

## Message Metadata

Messages may contain:

* sender information
* room information
* timestamps
* transaction IDs
* relation metadata
* encryption metadata
* delivery metadata

---

## Relation Support

The relay should support Matrix relation semantics including:

* replies
* reactions
* edits
* threads

Relation metadata is especially important for:

* ask-response correlation
* event reconstruction
* conversational workflows

---

## Message Normalization

The relay may normalize:

* sender identifiers
* timestamps
* room references
* message formatting

Normalization must not discard important Matrix metadata.

---

# 10. Sending API

The Sending API provides outbound Matrix message submission.

Outbound submission is asynchronous internally, even if synchronous acknowledgements are exposed externally.

---

## Supported Destination Types

### Room ID

Preferred destination type.

Example:

```text id="imx2py"
!abcdef:matrix.org
```

Room IDs are:

* globally unique
* stable
* canonical

---

### Room Alias

Supported.

Example:

```text id="r1zq4u"
#alerts:matrix.org
```

Room aliases may require resolution before sending.

---

### User ID

Supported through DM room resolution.

Example:

```text id="v1v91t"
@alice:matrix.org
```

The relay may:

* locate existing DM rooms
* create new DM rooms
* invite users where necessary

---

### Display Name Resolution

Optional and non-authoritative.

Display names are:

* mutable
* non-unique
* unreliable as canonical identifiers

Display-name-based routing should never be considered authoritative.

---

## Supported Message Types

Architecturally supported outbound event types include:

* plain text
* HTML-formatted text
* replies
* reactions
* edits
* redactions
* attachments
* media events

---

## HTML Formatting

The relay supports Matrix-formatted HTML.

The relay must:

* sanitize incoming HTML
* reject unsupported tags
* prevent unsupported formatting constructs

Matrix HTML formatting uses:

* `format: org.matrix.custom.html`
* `formatted_body`

The relay should preserve Matrix-compatible formatting semantics.

---

## Outbound Lifecycle

Outbound messages transition through lifecycle states:

```text id="7krxx4"
accepted
queued
sending
sent
acknowledged
failed
dead_letter
```

---

## Idempotency

The relay should support idempotency keys.

Idempotency keys help prevent duplicate outbound delivery caused by:

* retries
* API retries
* client reconnection
* transient failures

---

## Delivery Semantics

The relay should expose delivery metadata where practical.

Potential metadata includes:

* event IDs
* delivery timestamps
* retry counts
* failure reasons
* queue state

---

## Room Resolution

Room resolution may involve:

* alias resolution
* DM lookup
* membership validation
* encryption capability checks

Room resolution failures should produce structured errors.

---

## Encryption-Aware Sending

Before sending encrypted events, the relay may need to:

* fetch device lists
* establish encryption sessions
* verify encryption capabilities
* synchronize room encryption state

Encryption preparation may affect delivery latency.
