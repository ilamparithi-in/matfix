package persistence

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ilamparithi-in/matfix/internal/config"
	_ "modernc.org/sqlite"
)

// DB wraps a SQLite database connection.
type DB struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database at the path specified in cfg.
// The caller must call Close when done.
func Open(cfg config.DatabaseConfig) (*DB, error) {
	db, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("persistence: open sqlite: %w", err)
	}

	// SQLite works best with a single writer connection; avoid SQLITE_BUSY contention.
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("persistence: set pragma %q: %w", pragma, err)
		}
	}

	return &DB{db: db}, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// SQL returns the underlying *sql.DB.
// Use this only when a third-party library (e.g. mautrix-go crypto store) needs raw access.
func (d *DB) SQL() *sql.DB {
	return d.db
}

// Stores returns all store implementations backed by this DB.
func (d *DB) Stores() Stores {
	return Stores{
		Sync:        NewSyncStore(d.db),
		Queue:       NewQueueStore(d.db),
		Correlation: NewCorrelationStore(d.db),
		APIKey:      NewAPIKeyStore(d.db),
		EventCache:  NewEventCacheStore(d.db),
	}
}

// Stores groups all repository implementations for the application.
type Stores struct {
	Sync        SyncStore
	Queue       QueueStore
	Correlation CorrelationStore
	APIKey      APIKeyStore
	EventCache  EventCacheStore
}
