# Matrix Relay Daemon Specification — Part 1

Contains sections:

1. Overview
2. Goals
3. Terminology
4. Technology Stack
5. High-Level Architecture

---

# 1. Overview

## Purpose

The Matrix Relay Daemon is a persistent middleware service that provides simplified, reliable, and programmable APIs over the Matrix protocol.

The relay abstracts:

* Matrix synchronization
* End-to-End Encryption (E2EE)
* retry handling
* room resolution
* event correlation
* delivery persistence
* message queueing
* multi-account management

The relay is conceptually similar to a Message Transfer Agent (MTA), but for Matrix messaging.

The relay is not intended to replace native Matrix clients or homeservers.

---

# 2. Goals

## Primary Goals

* Simplify Matrix integrations for applications and infrastructure
* Provide durable and persistent message delivery
* Support synchronous workflows over asynchronous Matrix events
* Transparently support encrypted Matrix rooms
* Expose stable and implementation-independent APIs
* Support multiple configured Matrix accounts
* Provide reliable event correlation semantics
* Maintain Matrix-native semantics internally

---

## Non-Goals

* Acting as a Matrix homeserver
* Replacing native Matrix clients
* Replacing Matrix federation
* Providing generic distributed message broker semantics unrelated to Matrix
* Dynamic Matrix account registration through APIs
* Implementing custom Matrix protocol logic outside existing Matrix SDK support

---

# 3. Terminology

The project uses Matrix-native terminology wherever possible.

---

## Room

A Matrix room.

---

## Room ID

A globally unique Matrix room identifier.

Example:

```text
!abcdef:matrix.org
```

---

## Room Alias

A human-readable Matrix room alias.

Example:

```text
#alerts:matrix.org
```

---

## Event

A Matrix event.

---

## Relation

A Matrix event relationship such as replies or reactions.

---

## Sync Token

A Matrix sync continuation token.

---

## Sender

A Matrix user account used by the relay.

---

## Correlation

The process of associating incoming events with outbound requests.

---

## Relay

The Matrix Relay Daemon.

---

# 4. Technology Stack

## Language

* Go

Reasons:

* strong concurrency model
* static binary support
* efficient networking support
* suitable for infrastructure software
* mature HTTP ecosystem

---

## Matrix SDK

* mautrix-go

The relay uses mautrix-go for:

* Matrix connectivity
* synchronization
* event processing
* encryption
* device management
* room membership handling

The relay must not reimplement Matrix protocol behavior already supported by the SDK.

---

# 5. High-Level Architecture

The relay consists of multiple isolated internal components.

The architecture is intentionally modular to support future extensibility and avoid tight coupling between Matrix synchronization, queueing, correlation, and API logic.

---

## API Layer

Responsible for:

* HTTP REST APIs
* request parsing
* response serialization
* authentication
* authorization
* rate limiting
* input validation

The API layer must not directly interact with Matrix SDK objects.

All Matrix interactions must go through internal managers.

---

## Submission Manager

Responsible for:

* accepting outbound requests
* validating outbound requests
* preparing queue jobs
* resolving submission policies
* assigning delivery metadata

The Submission Manager inserts outbound work into the persistent queue system.

It must not directly send Matrix events.

---

## Queue Manager

Responsible for:

* persistent outbound queues
* retry scheduling
* delivery tracking
* dead-letter handling
* queue recovery after restart
* retry backoff policies

The Queue Manager owns outbound job lifecycle state.

Suggested delivery states:

```text
accepted
queued
sending
sent
acknowledged
failed
dead_letter
```

---

## Matrix Engine

Responsible for:

* Matrix connectivity
* account lifecycle management
* room resolution
* message sending
* SDK interaction
* encryption operations
* device synchronization

The Matrix Engine acts as the internal abstraction layer over mautrix-go.

Other internal modules should avoid depending directly on mautrix-go internals where possible.

---

## Sync Manager

Responsible for:

* ownership of Matrix sync streams
* sync token advancement
* inbound event ingestion
* internal event dispatching

Only the Sync Manager may directly consume Matrix sync streams.

This avoids:

* duplicate sync consumers
* sync token corruption
* race conditions
* inconsistent event ordering

All other components receive events through the internal event bus.

---

## Event Bus

Responsible for:

* internal publish/subscribe behavior
* event distribution
* internal decoupling
* asynchronous event propagation

The Event Bus distributes:

* message events
* membership events
* encrypted events
* delivery events
* retry events
* correlation events

The Event Bus is strictly internal and not externally exposed.

---

## Correlation Manager

Responsible for:

* ask-response tracking
* receive subscriptions
* timeout management
* response matching
* correlation cleanup
* concurrent request isolation

The Correlation Manager allows synchronous workflows to be implemented over asynchronous Matrix messaging semantics.

---

## Crypto Manager

Responsible for:

* encryption state management
* decryption operations
* device trust state
* Olm session handling
* Megolm session handling
* crypto persistence

The Crypto Manager owns E2EE lifecycle behavior.

---

## Persistence Layer

Responsible for:

* database abstraction
* schema migrations
* transaction management
* durable state persistence

All persistent state must go through this layer.

---

## Worker Pool

Responsible for:

* asynchronous processing
* queue workers
* retry workers
* event processing workers
* background cleanup jobs

Worker concurrency limits should be configurable.
