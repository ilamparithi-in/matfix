package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ilamparithi-in/matfix/internal/account"
	"github.com/ilamparithi-in/matfix/internal/correlation"
	"github.com/ilamparithi-in/matfix/internal/persistence"
	"github.com/ilamparithi-in/matfix/internal/submission"
)

// # Context keys

type contextKey int

const (
	contextKeyPermissions contextKey = iota
	contextKeyKeyID
)

// # Permissions

// Permissions represents the access-control settings for an API key.
// A nil/empty slice means unrestricted for that dimension.
type Permissions struct {
	// Accounts lists the account IDs this key may act on. Nil = any account.
	Accounts []string `json:"accounts,omitempty"`

	// Routes lists the API routes this key may access. Nil = any route.
	// Valid values: "send", "receive", "ask", "admin".
	Routes []string `json:"routes,omitempty"`

	// Rooms lists the room IDs this key may target. Nil = any room.
	Rooms []string `json:"rooms,omitempty"`

	// EventTypes lists the inbound event types the key may subscribe to.
	// Nil = any event type.
	EventTypes []string `json:"event_types,omitempty"`

	// RateLimitRPS is the per-key request rate limit in requests per second.
	// 0 means use the server default (100 RPS).
	RateLimitRPS int `json:"rate_limit_rps,omitempty"`
}

// permissionsFromCtx extracts the Permissions stored by APIKeyMiddleware.
func permissionsFromCtx(ctx context.Context) *Permissions {
	p, _ := ctx.Value(contextKeyPermissions).(*Permissions)
	return p
}

// keyIDFromCtx extracts the API key database ID stored by APIKeyMiddleware.
func keyIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(contextKeyKeyID).(string)
	return id
}

// # APIKeyMiddleware

// APIKeyMiddleware extracts the bearer token from the Authorization header,
// SHA-256 hashes it, looks it up in the DB, and attaches the key's Permissions
// to the request context.
//
// Requests that fail authentication receive 401 Unauthorized.
func APIKeyMiddleware(store persistence.APIKeyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" {
				writeError(w, http.StatusUnauthorized, "missing authorization header", "unauthorized")
				return
			}
			if !strings.HasPrefix(auth, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "authorization header must use Bearer scheme", "unauthorized")
				return
			}
			token := strings.TrimPrefix(auth, "Bearer ")
			if token == "" {
				writeError(w, http.StatusUnauthorized, "empty bearer token", "unauthorized")
				return
			}

			hash := hashAPIKey(token)
			row, err := store.GetByHash(r.Context(), hash)
			if err != nil {
				slog.Error("api: key lookup failed", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
				return
			}
			if row == nil || row.RevokedAt != nil {
				writeError(w, http.StatusUnauthorized, "invalid or revoked API key", "unauthorized")
				return
			}

			var perms Permissions
			if row.PermissionsJSON != "" {
				if err := json.Unmarshal([]byte(row.PermissionsJSON), &perms); err != nil {
					slog.Error("api: invalid permissions JSON", "key_id", row.ID, "error", err)
					writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
					return
				}
			}

			ctx := context.WithValue(r.Context(), contextKeyPermissions, &perms)
			ctx = context.WithValue(ctx, contextKeyKeyID, row.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// hashAPIKey returns the lowercase hex-encoded SHA-256 hash of a plaintext key.
// The plaintext key is never stored or logged.
func hashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

// # HTTP helpers

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
		Code  string `json:"code,omitempty"`
	}{Error: message, Code: code})
}

// writeJSON writes v as a JSON response with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// # Server

// Config holds the construction parameters for the API Server.
type Config struct {
	// BindAddr is the TCP address to listen on (e.g. ":8080").
	BindAddr string

	// Submission is the outbound message submission manager.
	Submission *submission.SubmissionManager

	// Correlation is the ask/receive correlation manager.
	Correlation *correlation.CorrelationManager

	// Accounts is the account lifecycle manager used for health and admin routes.
	Accounts *account.AccountManager

	// APIKeyStore is used by the auth middleware to look up API key records.
	APIKeyStore persistence.APIKeyStore

	// QueueStore is used by the admin queue inspection route.
	QueueStore persistence.QueueStore

	// CorrStore is used by the admin subscriptions inspection route.
	CorrStore persistence.CorrelationStore
}

// Server is the HTTP API server for matfix. It exposes /v1/* REST endpoints
// and /health/* probes. TLS termination is handled externally.
type Server struct {
	httpServer *http.Server
}

// New constructs a Server and registers all routes on a chi router.
func New(cfg Config) *Server {
	rl := newRateLimiter()

	mux := buildRouter(cfg, rl)

	return &Server{
		httpServer: &http.Server{
			Addr:        cfg.BindAddr,
			Handler:     mux,
			ReadTimeout: 30 * time.Second,
			// WriteTimeout is left at zero to support long-polling routes whose
			// response time is bounded by the caller-supplied timeout parameter.
			IdleTimeout: 120 * time.Second,
		},
	}
}

// Start begins accepting HTTP requests. It blocks until the server is stopped
// or encounters a fatal error (not http.ErrServerClosed).
func (s *Server) Start() error {
	slog.Info("api: server starting", "addr", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Stop performs a graceful shutdown with a 30-second deadline.
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.httpServer.Shutdown(ctx); err != nil {
		slog.Error("api: shutdown error", "error", err)
	}
}
