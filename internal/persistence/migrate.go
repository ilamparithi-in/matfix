package persistence

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
)

//go:embed schema/*.sql
var schemaFS embed.FS

// Migrate applies any pending forward-only SQL migrations to db.
// It is idempotent: migrations already recorded in schema_migrations are skipped.
// Migrations are applied in lexicographic filename order inside a transaction each.
func Migrate(db *sql.DB) error {
	ctx := context.Background()

	// Ensure the migration-tracking table exists.
	const createTable = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT    NOT NULL PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)`
	if _, err := db.ExecContext(ctx, createTable); err != nil {
		return fmt.Errorf("persistence: create schema_migrations: %w", err)
	}

	// Read all .sql files from the embedded FS.
	entries, err := fs.ReadDir(schemaFS, "schema")
	if err != nil {
		return fmt.Errorf("persistence: read schema dir: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version := entry.Name()

		// Skip if already applied.
		var count int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version,
		).Scan(&count); err != nil {
			return fmt.Errorf("persistence: check migration %s: %w", version, err)
		}
		if count > 0 {
			continue
		}

		sqlBytes, err := schemaFS.ReadFile("schema/" + version)
		if err != nil {
			return fmt.Errorf("persistence: read migration %s: %w", version, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("persistence: begin migration %s: %w", version, err)
		}

		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("persistence: apply migration %s: %w", version, err)
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			version, time.Now().UnixMilli(),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("persistence: record migration %s: %w", version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("persistence: commit migration %s: %w", version, err)
		}
	}

	return nil
}
