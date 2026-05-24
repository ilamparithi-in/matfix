package admin

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ilamparithi-in/matfix/internal/account"
	adminroutes "github.com/ilamparithi-in/matfix/internal/admin/routes"
	"github.com/ilamparithi-in/matfix/internal/persistence"
)

// # Config

// Config holds the construction parameters for the admin Server.
type Config struct {
	// SocketPath is the UNIX domain socket path (e.g. /run/matfix/admin.sock).
	SocketPath string

	// Accounts is the account lifecycle manager.
	Accounts *account.AccountManager

	// APIKeyStore manages API key records.
	APIKeyStore persistence.APIKeyStore

	// QueueStore is used for queue depth inspection.
	QueueStore persistence.QueueStore

	// CorrStore is used for active subscription inspection.
	CorrStore persistence.CorrelationStore
}

// # Server

// Server is the admin HTTP server bound to a UNIX domain socket.
// Access control is delegated entirely to OS-level socket file permissions;
// there is no application-layer authentication on this server.
type Server struct {
	httpServer *http.Server
	socketPath string
}

// New constructs a Server and registers all admin routes on a chi router.
func New(cfg Config) *Server {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	// Key management — create, list, revoke, rotate.
	r.Post("/keys", adminroutes.CreateKeyHandler(cfg.APIKeyStore))
	r.Get("/keys", adminroutes.ListKeysHandler(cfg.APIKeyStore))
	r.Delete("/keys/{id}", adminroutes.RevokeKeyHandler(cfg.APIKeyStore))
	r.Post("/keys/{id}/rotate", adminroutes.RotateKeyHandler(cfg.APIKeyStore))

	// Read-only inspection.
	r.Get("/accounts", adminroutes.AccountsHandler(cfg.Accounts))
	r.Get("/queue", adminroutes.QueueHandler(cfg.Accounts, cfg.QueueStore))
	r.Get("/subscriptions", adminroutes.SubscriptionsHandler(cfg.CorrStore))

	return &Server{
		httpServer: &http.Server{
			Handler:     r,
			ReadTimeout: 30 * time.Second,
			IdleTimeout: 60 * time.Second,
		},
		socketPath: cfg.SocketPath,
	}
}

// Start removes any leftover socket file, binds the UNIX socket, and begins
// accepting connections. It blocks until the server is stopped or encounters a
// fatal error (not http.ErrServerClosed).
func (s *Server) Start() error {
	// Remove a stale socket from a previous run so net.Listen does not fail.
	_ = os.Remove(s.socketPath)

	l, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("admin: listen on %s: %w", s.socketPath, err)
	}

	slog.Info("admin: socket server starting", "path", s.socketPath)
	if err := s.httpServer.Serve(l); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown gracefully drains in-flight requests and closes the listener.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
