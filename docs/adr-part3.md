# Matrix Relay Daemon Specification — Part 3

Contains sections:
11. Receiving API
12. Ask/Response API
13. Encryption Model
14. Queue and Retry Semantics
15. Internal Event Bus

---

# 11. Receiving API

The relay supports bounded future event collection.

The Receiving API allows applications to wait for future Matrix events without directly interacting with Matrix synchronization semantics.

The Receiving API abstracts:

* `/sync`
* event buffering
* event filtering
* timeout handling
* event correlation
* encryption handling

---

## Receive Semantics

Receive operations:

* wait for future events
* do not return historical backlog by default
* support timeout-based completion
* support limit-based completion
* support filter-based matching

The receive operation is conceptually a temporary event subscription.

---

## Receive Lifecycle

A receive request performs:

1. subscription registration
2. filter registration
3. timeout scheduling
4. event collection
5. response serialization
6. cleanup

---

## Future Events Only

By default, receive operations observe:

* events occurring after subscription registration

The relay should not implicitly return historical room history.

This avoids:

* replay ambiguity
* duplicate consumption confusion
* accidental historical processing

Historical retrieval may later be supported separately.

---

## Timeout Semantics

Receive operations support configurable timeout behavior.

Examples:

* complete after timeout
* complete after limit reached
* complete after first matching event

Timeout expiration is not considered an error condition.

---

## Event Limits

Receive operations may specify:

* maximum event count
* maximum collection duration

The relay may return partial results when:

* timeout expires
* internal limits are reached

---

## Supported Filters

Supported filtering concepts may include:

* room
* sender
* event type
* relation metadata
* regex matching
* transaction ID
* message body predicates
* encryption state
* account source

---

## Sender Filters

Sender filters restrict accepted events to specific Matrix users.

Example conceptual behavior:

* only accept replies from `@alice:example.org`

Sender filters are especially useful for:

* bot workflows
* command-response interactions
* moderated rooms

---

## Regex Matching

Regex filters allow matching against:

* message bodies
* formatted bodies
* event metadata

Regex matching should operate on normalized message content where practical.

---

## Relation Filters

Relation filters allow matching:

* replies to specific events
* reactions to specific events
* thread membership
* edit relationships

Relation filtering is especially important for ask-response workflows.

---

## Delivery Model

The primary receive model is:

* long polling over HTTP

The internal architecture should permit future support for:

* WebSockets
* Server-Sent Events (SSE)
* gRPC streaming

without redesigning the internal event model.

---

## Concurrent Receive Operations

The relay must support:

* multiple simultaneous receive requests
* overlapping filters
* multiple subscriptions in the same room

Concurrent receive operations must remain isolated from each other.

---

## Event Ordering

Where practical:

* per-room ordering should be preserved

Global event ordering is not guaranteed.

---

## Subscription Cleanup

Receive subscriptions must be cleaned up after:

* completion
* timeout
* cancellation
* client disconnect

Improper cleanup may result in:

* memory leaks
* orphaned subscriptions
* unnecessary event processing

---

## Receive Failure Semantics

Potential receive failures include:

* account unavailability
* invalid filters
* authorization failures
* internal event bus failures

Timeout expiration is not considered a failure.

---

# 12. Ask/Response API

The Ask API provides synchronous request-response workflows over asynchronous Matrix messaging.

The Ask API combines:

* outbound message submission
* response subscription
* correlation handling
* timeout handling

This allows applications to perform conversational workflows without manually implementing Matrix event correlation logic.

---

## Purpose

The Ask API is intended for:

* automation workflows
* bot interactions
* approval systems
* conversational infrastructure
* command-response semantics

The Ask API abstracts:

* event subscriptions
* reply tracking
* correlation state
* response matching

---

## Ask Lifecycle

An ask request performs:

1. outbound message creation
2. queue submission
3. correlation registration
4. subscription activation
5. response matching
6. timeout handling
7. cleanup

---

## Correlation Models

The relay supports multiple correlation strategies.

---

## Relation-Based Correlation

Preferred correlation strategy.

Uses Matrix reply relations:

* `m.in_reply_to`

Workflow:

1. relay sends outbound event
2. relay records outbound event ID
3. relay waits for replies referencing the event ID
4. matching reply resolves the ask request

This is the most Matrix-native correlation approach.

---

## Advantages of Relation-Based Correlation

Advantages:

* robust against unrelated room traffic
* supports concurrent asks
* naturally maps to Matrix semantics
* minimizes ambiguity

This should be the default correlation strategy.

---

## Sender-Constrained Correlation

Optional additional restriction.

The relay may require:

* replies originate from specific Matrix users

Example use cases:

* approval workflows
* admin commands
* restricted bot interactions

Sender constraints reduce accidental correlation matches.

---

## Predicate-Based Correlation

Advanced correlation strategy.

Predicates may include:

* regex matching
* event type matching
* structured body matching
* custom metadata matching
* relation matching
* sender matching

Predicate-based correlation provides flexible conversational workflows.

---

## Correlation State

The Correlation Manager tracks:

* outbound event IDs
* active ask requests
* timeout state
* matching rules
* completion state

Correlation state should be persisted where practical.

---

## Concurrency Guarantees

The relay must support:

* concurrent ask requests
* multiple asks in the same room
* overlapping filters
* simultaneous responses

Concurrent asks must remain isolated.

---

## Ambiguous Matches

Ambiguous response matches must be resolved deterministically.

Potential strategies:

* first match wins
* strict relation matching
* sender precedence
* explicit rejection of ambiguity

Behavior should remain predictable.

---

## Ask Timeouts

Ask requests support configurable timeouts.

Timeout expiration:

* completes the request
* cleans up subscriptions
* removes correlation state

Timeout expiration is not considered an internal relay failure.

---

## Multi-Response Support

Future support may include:

* collecting multiple responses
* quorum semantics
* voting workflows
* approval aggregation

The architecture should permit this without redesign.

---

## Ask Cancellation

Ask requests may support:

* explicit cancellation
* client disconnect cleanup
* timeout cancellation

Cancellation should release:

* subscriptions
* correlation state
* timers

---

## Failure Semantics

Potential ask failures include:

* outbound delivery failure
* account unavailability
* invalid correlation filters
* authorization failures
* internal correlation failures

Timeout expiration is not considered an internal failure.

---

# 13. Encryption Model

The relay supports Matrix End-to-End Encryption (E2EE).

The relay abstracts encryption handling from client applications.

Applications should not need to manually manage:

* Olm sessions
* Megolm sessions
* device lists
* encryption synchronization
* key persistence

---

## Supported Encryption

The relay supports:

* Olm
* Megolm

through mautrix-go crypto functionality.

The relay must not implement custom cryptographic behavior outside supported SDK behavior.

---

## Encryption Persistence

Encryption state must persist across restarts.

Required persistent crypto state includes:

* device keys
* Olm sessions
* Megolm sessions
* trust metadata
* verification metadata

Without persistent crypto state:

* encrypted rooms become unusable
* session renegotiation becomes excessive
* decryption continuity breaks

---

## Device Store

Each Matrix account maintains an independent device store.

Device stores contain:

* known devices
* trust state
* verification state
* encryption capabilities

Device stores must persist.

---

## Verification Strategy

Verification strategy determines how the relay decides whether Matrix devices are trusted.

Potential verification models include:

* TOFU (Trust On First Use)
* explicit allowlists
* manual verification
* automatic trust policies

The verification strategy affects:

* whether encrypted messages are accepted
* whether encrypted messages are sent
* device trust decisions
* encryption security guarantees

---

## TOFU (Trust On First Use)

Under TOFU:

* unknown devices are trusted initially
* future device changes may trigger warnings or restrictions

TOFU is operationally convenient but less secure against impersonation attacks.

---

## Manual Verification

Manual verification requires:

* explicit device approval
* explicit trust decisions

This is more secure but operationally more complex.

---

## Device Trust Changes

The relay must handle:

* new devices
* removed devices
* changed device keys
* device list synchronization

Unexpected device changes may:

* trigger warnings
* trigger trust invalidation
* block encrypted sending depending on policy

---

## Encryption Failure Modes

Potential encryption failures include:

* undecryptable events
* missing Megolm sessions
* missing device keys
* unverified devices
* expired sessions
* device list desynchronization

The relay must:

* log encryption failures
* expose encryption failure metadata
* continue operation where possible

---

## Undecryptable Events

The relay may receive events that cannot be decrypted.

Potential causes:

* missing session keys
* delayed key delivery
* corrupted crypto state
* untrusted devices

The relay should:

* expose undecryptable event metadata
* optionally retry decryption later
* avoid crashing

---

## Encryption-Aware Routing

Before sending encrypted events, the relay may need to:

* synchronize device lists
* establish encryption sessions
* fetch missing keys
* verify encryption capabilities

Encryption preparation may increase outbound latency.

---

## Multi-Account Encryption Isolation

Each Matrix account maintains independent:

* crypto stores
* device stores
* trust state
* encryption sessions

Encryption failures in one account must not affect unrelated accounts.

---

# 14. Queue and Retry Semantics

The relay uses persistent queue-based outbound delivery.

Outbound messages are never considered transient in-memory operations.

---

## Queue Guarantees

Outbound jobs must:

* survive process restarts
* survive temporary outages
* survive transient federation failures
* support retry scheduling

Queue durability is mandatory.

---

## Queue Ownership

The Queue Manager exclusively owns:

* outbound job state
* retry scheduling
* delivery transitions
* dead-letter handling

Other components must not directly mutate queue state.

---

## Delivery Lifecycle

Outbound jobs transition through states:

```text id="zyvsgg"
accepted
queued
sending
sent
acknowledged
failed
dead_letter
```

---

## Retryable Failures

Retryable failures may include:

* federation timeouts
* temporary homeserver failures
* rate limits
* transient encryption failures
* network interruptions

Retryable failures should not permanently fail jobs immediately.

---

## Non-Retryable Failures

Non-retryable failures may include:

* malformed requests
* invalid room identifiers
* authorization failures
* unsupported event types
* invalid formatting

Non-retryable failures should terminate job processing immediately.

---

## Retry Strategy

The relay should support:

* exponential backoff
* retry ceilings
* cooldown periods
* retry metadata tracking

Retry scheduling should avoid:

* tight retry loops
* excessive federation load
* uncontrolled queue growth

---

## Retry Metadata

Retry metadata may include:

* retry count
* next retry timestamp
* last failure reason
* last failure timestamp

This metadata is useful for:

* observability
* debugging
* operational tooling

---

## Dead Letter Queue

The relay should support dead-letter handling for permanently failed jobs.

Dead-lettered jobs should preserve:

* original payloads
* failure reasons
* retry history
* timestamps

Dead-letter queues are useful for:

* debugging
* manual recovery
* operational inspection

---

## Queue Recovery

After restart, the relay must:

* restore queued jobs
* restore retry schedules
* resume processing safely

Recovery behavior must avoid:

* duplicate uncontrolled sends
* orphaned retry state

---

## Exactly-Once Delivery

Exactly-once delivery is not guaranteed.

Matrix federation itself does not guarantee exactly-once semantics.

The relay should instead aim for:

* best-effort deduplication
* idempotent operations where practical

---

## Queue Isolation

Queue failures should be isolated where practical.

Examples:

* one failed account should not block unrelated queues
* one malformed job should not stop the queue system

---

# 15. Internal Event Bus

The Event Bus is the internal communication backbone of the relay.

The Event Bus decouples:

* sync processing
* queue processing
* correlation logic
* crypto logic
* observability systems

---

## Purpose

The Event Bus allows internal modules to communicate without:

* direct dependency chains
* tight coupling
* shared ownership of Matrix state

This improves:

* modularity
* testability
* fault isolation
* extensibility

---

## Event Types

The Event Bus distributes:

* inbound Matrix events
* outbound delivery events
* retry events
* queue events
* encryption events
* correlation events
* lifecycle events

---

## Internal Publish/Subscribe Model

Internal consumers subscribe using:

* event type
* room
* sender
* account
* custom predicates

Subscriptions are internal-only.

The Event Bus is not externally exposed.

---

## Ordering Guarantees

Where practical:

* per-room ordering should be preserved

Global ordering is not guaranteed.

---

## Backpressure Handling

The Event Bus must consider:

* bounded buffering
* slow subscribers
* queue overflow
* memory growth

The relay should avoid unbounded in-memory event accumulation.

---

## Subscriber Isolation

Failure in one subscriber should not:

* stop sync processing
* terminate unrelated consumers
* corrupt event flow

Subscribers should fail independently where practical.

---

## Delivery Semantics

Internal delivery semantics may be:

* at-most-once
* at-least-once

depending on event type and implementation constraints.

Behavior should remain well-defined.

---

## Event Filtering

The Event Bus should support:

* lightweight filtering
* efficient fan-out
* selective subscriptions

Filtering should avoid unnecessary event copying where possible.

---

## Observability Integration

The Event Bus should expose:

* subscriber counts
* queue depths
* event throughput
* overflow statistics

This is important for operational visibility.

---

## Future Extensibility

The Event Bus architecture should permit future support for:

* distributed workers
* external streaming APIs
* clustered deployments
* plugin systems

without redesigning internal event semantics.
