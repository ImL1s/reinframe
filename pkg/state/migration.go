package state

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migration represents a single SQL migration file.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// RunMigrations executes all embedded SQL migrations that have not yet been applied.
func RunMigrations(db *sql.DB) error {
	return RunMigrationsContext(context.Background(), db)
}

// RunMigrationsContext executes all embedded SQL migrations with context.
func RunMigrationsContext(ctx context.Context, db *sql.DB) error {
	// Ensure schema_migrations table exists
	err := runTxWithRetry(ctx, db, func(tx *sql.Tx) error {
		bootstrapSQL := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY NOT NULL,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL
		);`
		if _, err := tx.ExecContext(ctx, bootstrapSQL); err != nil {
			return fmt.Errorf("failed to bootstrap schema_migrations table: %w", err)
		}
		return tx.Commit()
	})
	if err != nil {
		return err
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read embedded migrations directory: %w", err)
	}

	var migrations []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}

		version, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid migration version in file %s: %w", entry.Name(), err)
		}

		content, err := migrationFS.ReadFile(path.Join("migrations", entry.Name()))
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", entry.Name(), err)
		}

		migrations = append(migrations, Migration{
			Version: version,
			Name:    entry.Name(),
			SQL:     string(content),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	for _, m := range migrations {
		err := runTxWithRetry(ctx, db, func(tx *sql.Tx) error {
			var exists bool
			err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", m.Version).Scan(&exists)
			if err != nil {
				return fmt.Errorf("failed to check migration status for version %d: %w", m.Version, err)
			}

			if exists {
				return tx.Commit()
			}

			if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
				return fmt.Errorf("failed to execute migration %s: %w", m.Name, err)
			}

			appliedAt := time.Now().UTC().Format(time.RFC3339Nano)
			recordSQL := "INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)"
			if _, err := tx.ExecContext(ctx, recordSQL, m.Version, m.Name, appliedAt); err != nil {
				return fmt.Errorf("failed to record migration %s: %w", m.Name, err)
			}

			return tx.Commit()
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func runTxWithRetry(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			if isBusyError(err) {
				lastErr = err
				time.Sleep(10 * time.Millisecond)
				continue
			}
			return err
		}

		err = fn(tx)
		if err == nil {
			return nil
		}

		_ = tx.Rollback()
		if isBusyError(err) {
			lastErr = err
			time.Sleep(10 * time.Millisecond)
			continue
		}
		return err
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("migration transaction timed out due to busy lock")
}

func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	str := err.Error()
	return strings.Contains(str, "database is locked") || strings.Contains(str, "SQLITE_BUSY") || strings.Contains(str, "busy")
}
