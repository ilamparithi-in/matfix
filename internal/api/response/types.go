package response

// # Send

// SendResponse is the JSON body of a successful POST /v1/send response.
type SendResponse struct {
	JobID string `json:"job_id"`
}

// # Ask

// AskResponse is the JSON body of a successful POST /v1/ask response.
type AskResponse struct {
	JobID        string        `json:"job_id"`
	MatchedEvent *EventPayload `json:"matched_event,omitempty"`
	TimedOut     bool          `json:"timed_out,omitempty"`
}

// # Receive

// ReceiveResponse is the JSON body of a successful POST /v1/receive response.
type ReceiveResponse struct {
	Events   []EventPayload `json:"events"`
	TimedOut bool           `json:"timed_out,omitempty"`
}

// # Event payload

// EventPayload represents a matched inbound event returned in receive or ask
// responses.
type EventPayload struct {
	EventID   string `json:"event_id"`
	AccountID string `json:"account_id"`
	RoomID    string `json:"room_id"`
	SenderID  string `json:"sender_id,omitempty"`
	Type      string `json:"type"`
	Body      string `json:"body,omitempty"`
	Timestamp int64  `json:"timestamp"` // Unix milliseconds
}

// # Error

// ErrorResponse is returned for all non-2xx responses.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// # Health

// LivenessResponse is returned by GET /health/live.
type LivenessResponse struct {
	Status string `json:"status"`
}

// ReadinessResponse is returned by GET /health/ready.
type ReadinessResponse struct {
	Status   string            `json:"status"`
	Accounts map[string]string `json:"accounts,omitempty"`
}

// # Admin

// AdminQueueResponse is returned by GET /v1/admin/queue.
type AdminQueueResponse struct {
	Queued     int `json:"queued"`
	Sending    int `json:"sending"`
	Failed     int `json:"failed"`
	DeadLetter int `json:"dead_letter"`
}

// AdminAccountsResponse is returned by GET /v1/admin/accounts.
type AdminAccountsResponse struct {
	Accounts []AccountStatus `json:"accounts"`
}

// AccountStatus is one entry in AdminAccountsResponse.
type AccountStatus struct {
	ID        string `json:"id"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

// AdminSubscriptionsResponse is returned by GET /v1/admin/subscriptions.
type AdminSubscriptionsResponse struct {
	Asks     int `json:"asks"`
	Receives int `json:"receives"`
}
