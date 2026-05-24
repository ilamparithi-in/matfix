Almost — there is still one final section left from the original structure.

# Matrix Relay Daemon Specification — Part 5

Contains sections:
21. Architectural Principles
22. Attachment Support

---

# 21. Architectural Principles

This section defines the high-level architectural philosophy of the relay.

These principles guide:

* implementation decisions
* future extensibility
* operational behavior
* internal ownership boundaries

Architectural principles are important because they prevent future features from gradually violating the original system design.

---

## Matrix as Source of Truth

The relay should preserve Matrix-native semantics internally wherever possible.

The relay must avoid inventing incompatible messaging abstractions that conflict with Matrix behavior.

Examples:

* preserve Matrix event IDs
* preserve relation semantics
* preserve room semantics
* preserve encryption semantics

The relay may expose simplified APIs externally, but internal systems should remain Matrix-aware.

---

## Stateful by Design

The relay is intentionally stateful.

Persistent state is required for:

* synchronization continuity
* queue durability
* encryption persistence
* correlation tracking
* retry scheduling

Stateless operation is not a design goal.

---

## Reliability Over Minimalism

Reliability and recoverability are prioritized over implementation minimalism.

Examples:

* persistent queues preferred over in-memory queues
* durable sync state preferred over transient sync state
* structured retries preferred over fire-and-forget delivery

Operational correctness is more important than minimizing implementation complexity.

---

## Explicit Ownership Boundaries

Internal components should have clearly defined ownership responsibilities.

Examples:

* only Sync Manager consumes `/sync`
* only Queue Manager mutates outbound queue state
* only Crypto Manager owns encryption lifecycle behavior

Shared mutable state should be minimized.

Ownership clarity improves:

* maintainability
* debugging
* fault isolation
* concurrency safety

---

## Internal Decoupling

Internal modules should communicate through stable internal interfaces and the internal Event Bus where practical.

This reduces:

* tight coupling
* cascading failures
* implementation rigidity

Decoupling improves:

* extensibility
* testing
* future feature development

---

## Persistence-First Architecture

Critical operational state should be durable wherever practical.

Critical state includes:

* queue state
* retry state
* sync state
* crypto state
* correlation state

The relay should minimize irreplaceable volatile state.

---

## Failure Isolation

Failures should remain isolated where practical.

Examples:

* one failed Matrix account should not stop unrelated accounts
* one failed event consumer should not terminate sync processing
* one malformed queue job should not stop queue processing

Failure isolation improves operational resilience.

---

## Best-Effort Delivery Semantics

Exactly-once delivery is not guaranteed.

The relay instead aims for:

* best-effort deduplication
* deterministic retries
* idempotent workflow support where practical

This aligns with Matrix federation realities.

---

## API Simplicity, Internal Sophistication

External APIs should remain simple and stable.

Internal systems may remain significantly more sophisticated.

The relay exists specifically to abstract:

* Matrix synchronization complexity
* encryption complexity
* retry complexity
* correlation complexity

Applications should not need to understand Matrix internals to use the relay effectively.

---

## Future-Proof Internal Models

Internal abstractions should preserve future extensibility.

The architecture should avoid assumptions that would later prevent:

* streaming APIs
* distributed workers
* plugin systems
* advanced routing
* clustering
* richer correlation models

Even initially unimplemented features should remain architecturally possible.

---

## Avoid Reinventing Matrix

The relay should reuse existing Matrix semantics whenever possible instead of inventing parallel systems.

Examples:

* use Matrix reply relations for correlation
* use Matrix room semantics directly
* use Matrix encryption semantics directly

This reduces:

* protocol drift
* incompatibility
* maintenance burden

---

## Operational Transparency

Operational state should be observable and inspectable.

Operators should be able to inspect:

* queues
* retries
* correlation state
* account state
* encryption failures
* delivery failures

Opaque infrastructure behavior should be avoided.

---

## Security-Conscious Design

The relay handles:

* access tokens
* encrypted message content
* device trust state
* API authentication credentials

Security-sensitive data must be handled carefully.

The relay should:

* avoid leaking secrets
* support redaction
* isolate account state
* support least-privilege authorization

---

## Deterministic Behavior

The relay should behave predictably under:

* retries
* reconnects
* federation instability
* concurrent requests
* partial failures

Deterministic behavior simplifies:

* debugging
* operational recovery
* automation reliability

---

## Extensibility Without Architectural Violation

Future features should integrate into the architecture without violating:

* ownership boundaries
* persistence guarantees
* Matrix semantic compatibility
* failure isolation principles

Features should extend the architecture, not bypass it.

---

# 22. Attachment Support

This section defines how the relay handles file and media attachments in both
outbound (relay → Matrix) and inbound (Matrix → relay consumer) directions.

---

## Scope

The relay supports the following Matrix attachment message types:

* m.file — generic file
* m.image — image
* m.audio — audio
* m.video — video

Thumbnail generation and sticker events (m.sticker) are out of scope for this
version.

---

## Media Manager

A dedicated Media Manager component owns all attachment upload and
encryption-for-upload logic.

The Media Manager is a stateless, privileged internal component permitted to
import the Matrix SDK directly, at the same tier as the Sync Manager and Crypto
Manager.

No other internal component may perform Matrix media uploads directly.

The Engine delegates attachment preparation entirely to the Media Manager and
does not inspect file bytes directly.

---

## Outbound Attachment Flow

Outbound attachment delivery follows this lifecycle:

1. The API caller submits a send request with type "file", base64-encoded file
   bytes, a MIME type, a filename, and optional metadata hints (width, height,
   duration in milliseconds).
2. The API handler decodes the base64 bytes and enforces a maximum upload size
   (default 50 MB, 52,428,800 bytes). Oversized payloads are rejected at the
   API boundary before queue insertion.
3. The Submission Manager enqueues a FileMessage job. The raw bytes are
   serialised as base64-encoded JSON in the queue. This is acceptable for v1.
4. A Worker pulls the job and calls Engine.Send with the FileMessage.
5. The Engine delegates to the Media Manager to prepare the attachment:
   a. The Media Manager checks whether the target room is encrypted via the
      Matrix state store.
   b. If encrypted: the bytes are encrypted using AES-CTR with a randomly
      generated key and IV (via mautrix-go attachment.EncryptedFile). The
      ciphertext is uploaded. The event content File field carries the MXC URI
      and full key material.
   c. If not encrypted: the bytes are uploaded directly and the event content
      URL field carries the MXC URI.
6. The Engine sends the m.room.message event with the prepared content.
7. The MsgType is inferred from the MIME type: image/* → m.image, audio/* →
   m.audio, video/* → m.video, all else → m.file.

---

## Encryption Transparency

Callers always submit plaintext bytes regardless of room encryption state.

The Media Manager determines whether encryption is required and handles it
transparently, consistent with how the Engine handles E2EE for text messages.

Callers are never exposed to MXC URIs or encryption key material on the
outbound path.

---

## Inbound Attachment Flow

When the Sync Manager receives an m.room.message event with an attachment
message type, normalization proceeds as follows:

1. The dispatcher identifies the message type as m.file, m.image, m.audio, or
   m.video.
2. The normalizer extracts attachment metadata:
   a. For unencrypted attachments: the MXC URI from the URL field.
   b. For encrypted attachments: the MXC URI and full key material (key, IV,
      SHA-256 hash, version) from the File field.
   c. File metadata from the Info block: MIME type, size, dimensions, duration.
   d. Filename from the FileName field.
3. The InboundMessageEvent published on the bus carries a non-nil Attachment
   field containing all extracted metadata.

The relay does not eagerly download or decrypt inbound attachments. Consumers
receive the URL and key material and are responsible for downloading and
decrypting the bytes themselves.

This preserves the relay's role as a relay, avoids unnecessary bandwidth
consumption, and is consistent with the principle of API simplicity with
internal sophistication.

---

## API Surface

### Outbound request

The send and ask request bodies accept a new message type "file" with a nested
FileAttachment object containing:

* data — base64-encoded file bytes (required)
* mime_type — MIME content type (required)
* filename — display filename (optional)
* width, height — pixel dimensions for image and video (optional)
* duration — duration in milliseconds for audio and video (optional)

### Inbound response

The receive and ask response EventPayload carries a non-nil attachment field
when the matched event is an attachment. The attachment object contains:

* url — MXC URI for unencrypted attachments
* encrypted_file — key material object for encrypted attachments, with fields
  url, key, iv, sha256, and version; consumers download from encrypted_file.url
  and decrypt using the provided key material
* mime_type, filename, size, width, height, duration — metadata

Exactly one of url or encrypted_file is populated.

---

## Size Limit

The relay enforces a maximum attachment size at the API boundary before queue
insertion.

The default limit is 50 MB (52,428,800 bytes) of decoded plaintext.

Homeservers independently enforce their own upload size limits. If a homeserver
rejects an upload, the job follows the standard retry and dead-letter lifecycle.

---

## Ownership Rules

* Only the Media Manager may call Matrix media upload APIs.
* Only the Media Manager may encrypt attachment bytes for upload.
* Only the Sync Manager normalizer may extract attachment metadata from
  inbound events.
* The Engine delegates attachment preparation entirely to the Media Manager.
