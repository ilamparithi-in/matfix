package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/ilamparithi-in/matfix/internal/account"
	"github.com/ilamparithi-in/matfix/internal/admin"
	"github.com/ilamparithi-in/matfix/internal/api"
	"github.com/ilamparithi-in/matfix/internal/persistence"
	"github.com/ilamparithi-in/matfix/internal/worker"
)

// shutdownTimeout is the maximum wall-clock time allowed for the entire
// graceful shutdown sequence. If any step exceeds its share the context
// deadline will propagate to OS-level socket/connection closes.
const shutdownTimeout = 30 * time.Second

// shutdown performs an ordered graceful shutdown of all daemon components:
//
//  1. Stop the HTTP API server — no new requests accepted after this point.
//  2. Stop the admin UNIX socket server.
//  3. Drain the worker pool — wait for in-flight send jobs to finish.
//  4. Stop all account sync loops and per-account components.
//  5. Close the database connection.
func shutdown(
	apiSrv *api.Server,
	adminSrv *admin.Server,
	workers *worker.WorkerPool,
	accounts *account.AccountManager,
	db *persistence.DB,
) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	slog.Info("matfix: stopping HTTP API server")
	apiSrv.Stop()

	slog.Info("matfix: stopping admin socket server")
	if err := adminSrv.Shutdown(ctx); err != nil {
		slog.Error("matfix: admin server shutdown error", "error", err)
	}

	slog.Info("matfix: draining worker pool")
	workers.Stop()

	slog.Info("matfix: stopping account sync loops")
	accounts.StopAll()

	slog.Info("matfix: closing database")
	if err := db.Close(); err != nil {
		slog.Error("matfix: database close error", "error", err)
	}

	slog.Info("matfix: shutdown complete")
}
