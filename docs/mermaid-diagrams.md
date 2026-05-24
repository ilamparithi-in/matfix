# Matrix Relay Daemon - State Machines and Flowcharts

# 1. High-Level System Architecture Flow

```mermaid
flowchart TD
    A[Client Application] --> B[API Layer]
    B --> C[Authentication & Authorization]
    C --> D[Submission Manager]
    D --> E[Queue Manager]

    E --> F[Worker Pool]
    F --> G[Matrix Engine]
    G --> H[mautrix-go]
    H --> I[Matrix Homeserver]

    I --> J[Sync Manager]
    J --> K[Event Bus]

    K --> L[Correlation Manager]
    K --> M[Crypto Manager]
    K --> N[Observability]
    K --> O[Receive Subscriptions]

    L --> P[Ask/Response Resolution]

    Q[Persistence Layer] --> E
    Q --> J
    Q --> L
    Q --> M
    Q --> G
```

---

# 2. Outbound Message Lifecycle State Machine

```mermaid
stateDiagram-v2
    [*] --> accepted

    accepted --> queued

    queued --> sending

    sending --> sent
    sending --> failed

    failed --> queued : retry scheduled
    failed --> dead_letter : retries exhausted

    sent --> acknowledged

    acknowledged --> [*]
    dead_letter --> [*]
```

---

# 3. Queue Retry Flowchart

```mermaid
flowchart TD
    A[Worker Pulls Queue Job] --> B[Attempt Delivery]

    B --> C{Delivery Successful?}

    C -->|Yes| D[Mark Sent]
    D --> E[Wait for Acknowledgement]
    E --> F[Mark Acknowledged]

    C -->|No| G{Retryable Failure?}

    G -->|No| H[Mark Failed]
    H --> I[Move to Dead Letter Queue]

    G -->|Yes| J[Increment Retry Count]
    J --> K[Calculate Backoff]
    K --> L[Schedule Retry]
    L --> M[Return to Queue]
```

---

# 4. Sync Manager Event Flow

```mermaid
flowchart TD
    A[Start Sync Loop] --> B[Call Matrix /sync]

    B --> C{Sync Successful?}

    C -->|No| D[Log Failure]
    D --> E[Retry Sync]
    E --> B

    C -->|Yes| F[Receive Sync Response]

    F --> G[Validate Sync State]
    G --> H[Advance Sync Token]

    H --> I[Normalize Events]

    I --> J[Publish to Event Bus]

    J --> K[Correlation Manager]
    J --> L[Receive Subscribers]
    J --> M[Crypto Manager]
    J --> N[Observability]

    N --> B
```

---

# 5. Ask/Response Lifecycle State Machine

```mermaid
stateDiagram-v2
    [*] --> ask_created

    ask_created --> queued

    queued --> sent

    sent --> waiting_for_response

    waiting_for_response --> response_received
    waiting_for_response --> timeout_expired
    waiting_for_response --> cancelled

    response_received --> resolved

    resolved --> cleaned_up
    timeout_expired --> cleaned_up
    cancelled --> cleaned_up

    cleaned_up --> [*]
```

---

# 6. Ask/Response Correlation Flowchart

```mermaid
flowchart TD
    A[Client Sends Ask Request] --> B[Create Correlation Context]

    B --> C[Send Matrix Event]

    C --> D[Store Event ID]

    D --> E[Register Subscription]

    E --> F[Wait for Incoming Events]

    F --> G{Event Matches Correlation Rules?}

    G -->|No| F

    G -->|Yes| H{Sender Allowed?}

    H -->|No| F

    H -->|Yes| I{Predicate Match?}

    I -->|No| F

    I -->|Yes| J[Resolve Ask Request]

    J --> K[Cleanup Correlation State]

    K --> L[Return Response]
```

---

# 7. Receive Subscription Lifecycle

```mermaid
stateDiagram-v2
    [*] --> subscription_created

    subscription_created --> active

    active --> collecting_events

    collecting_events --> limit_reached
    collecting_events --> timeout_expired
    collecting_events --> cancelled

    limit_reached --> finalized
    timeout_expired --> finalized
    cancelled --> finalized

    finalized --> cleaned_up

    cleaned_up --> [*]
```

---

# 8. Multi-Account Startup Flow

```mermaid
flowchart TD
    A[Relay Startup] --> B[Load Configuration]

    B --> C[Initialize Database]

    C --> D[Initialize Accounts]

    D --> E{Account Valid?}

    E -->|No| F[Mark Account Failed]

    E -->|Yes| G[Initialize Crypto]

    G --> H{Crypto Successful?}

    H -->|No| F

    H -->|Yes| I[Start Sync Loop]

    I --> J[Mark Account Available]

    J --> K{More Accounts?}

    F --> K

    K -->|Yes| D

    K -->|No| L{At Least One Account Available?}

    L -->|No| M[Terminate Process]

    L -->|Yes| N[Start API Server]
```

---

# 9. Multi-Account Isolation Diagram

```mermaid
flowchart LR
    A[API Layer]

    A --> B1[Account A]
    A --> B2[Account B]
    A --> B3[Account C]

    B1 --> C1[Sync Loop A]
    B1 --> D1[Crypto Store A]
    B1 --> E1[Queue A]

    B2 --> C2[Sync Loop B]
    B2 --> D2[Crypto Store B]
    B2 --> E2[Queue B]

    B3 --> C3[Sync Loop C]
    B3 --> D3[Crypto Store C]
    B3 --> E3[Queue C]
```

---

# 10. Encryption Lifecycle Flowchart

```mermaid
flowchart TD
    A[Need to Send Encrypted Event] --> B[Check Room Encryption State]

    B --> C{Encryption Enabled?}

    C -->|No| D[Send Plaintext Event]

    C -->|Yes| E[Fetch Device List]

    E --> F[Verify Device Trust]

    F --> G{Trusted Devices Available?}

    G -->|No| H[Block or Warn Depending on Policy]

    G -->|Yes| I[Establish Olm Sessions]

    I --> J[Generate Megolm Session]

    J --> K[Encrypt Event]

    K --> L[Send Encrypted Event]
```

---

# 11. Device Verification State Machine

```mermaid
stateDiagram-v2
    [*] --> unknown

    unknown --> trusted : TOFU/manual verification
    unknown --> blocked

    trusted --> revoked : key change
    trusted --> blocked

    revoked --> trusted : re-verification
    revoked --> blocked

    blocked --> trusted : manual approval

    trusted --> [*]
    blocked --> [*]
```

---

# 12. Event Bus Internal Flow

```mermaid
flowchart TD
    A[Sync Manager Publishes Event] --> B[Event Bus]

    B --> C1[Correlation Manager]
    B --> C2[Receive Subscribers]
    B --> C3[Crypto Manager]
    B --> C4[Observability]
    B --> C5[Queue Manager]

    C1 --> D1[Ask Resolution]
    C2 --> D2[Receive Responses]
    C3 --> D3[Decryption / Trust Updates]
    C4 --> D4[Metrics & Logging]
    C5 --> D5[Retry Scheduling]
```

---

# 13. API Request Flow

```mermaid
flowchart TD
    A[Incoming HTTP Request] --> B[Authenticate API Key]

    B --> C{Valid Key?}

    C -->|No| D[Reject Request]

    C -->|Yes| E[Authorize Route]

    E --> F{Route Allowed?}

    F -->|No| G[Reject Request]

    F -->|Yes| H[Check Rate Limits]

    H --> I{Rate Limit Exceeded?}

    I -->|Yes| J[Reject Request]

    I -->|No| K[Forward to Internal Manager]

    K --> L[Generate Response]
```

---

# 14. Relay Failure Handling Flow

```mermaid
flowchart TD
    A[Failure Detected] --> B{Failure Type}

    B -->|Account Failure| C[Mark Account Unavailable]

    C --> D{Any Accounts Remaining?}

    D -->|Yes| E[Continue Running]
    D -->|No| F[Terminate Relay]

    B -->|Database Failure| G{Recoverable?}

    G -->|Yes| H[Retry / Degraded Mode]
    G -->|No| F

    B -->|Sync Failure| I[Reconnect Sync Loop]

    B -->|Crypto Failure| J[Log Failure]

    J --> E

    I --> E

    H --> E
```

---

# 15. Complete Outbound Send Sequence Diagram

```mermaid
sequenceDiagram
    participant Client
    participant API
    participant Submission
    participant Queue
    participant Worker
    participant Matrix
    participant Homeserver

    Client->>API: POST /send

    API->>API: Authenticate & Authorize

    API->>Submission: Submit Request

    Submission->>Queue: Create Queue Job

    Queue-->>API: Job Accepted

    API-->>Client: Accepted Response

    Worker->>Queue: Pull Job

    Worker->>Matrix: Send Event

    Matrix->>Homeserver: Matrix API Call

    Homeserver-->>Matrix: Event Response

    Matrix-->>Worker: Delivery Result

    Worker->>Queue: Update Job State
```

---

# 16. Complete Ask/Response Sequence Diagram

```mermaid
sequenceDiagram
    participant Client
    participant API
    participant Correlation
    participant Queue
    participant Matrix
    participant Room

    Client->>API: POST /ask

    API->>Correlation: Register Ask Context

    API->>Queue: Queue Outbound Message

    Queue->>Matrix: Send Matrix Event

    Matrix->>Room: Deliver Event

    Correlation->>Correlation: Wait for Matching Events

    Room-->>Matrix: Reply Event

    Matrix-->>Correlation: Incoming Event

    Correlation->>Correlation: Match Correlation Rules

    Correlation-->>API: Resolve Ask Request

    API-->>Client: Return Response
```
