// Package db owns the controller's SQLite database connection and
// migration runner. All other controller packages that need to
// persist state should accept a *sql.DB and use the query helpers
// in this package or write their own against the schema in
// db/migrations/.
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens (or creates) a SQLite database at path, applies the
// migration set, and returns a connection pool ready to use.
//
// If path is ":memory:" an in-memory database is returned. WAL is
// enabled for file-backed databases. Foreign keys are enforced.
//
// For file-backed paths the parent directory is created with mode
// 0o700 if it doesn't already exist. Permissions errors (which
// happen when the parent is a bind mount owned by a host UID
// the container process can't write as) are surfaced with a clear
// hint.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if path == "" {
		return nil, errors.New("db: empty path")
	}

	if path != ":memory:" {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
				return nil, fmt.Errorf("db: cannot create %q (permission denied). The container runs as UID 1000:1000; the host directory mounted at %q must be writable by that UID", dir, dir)
			}
			return nil, fmt.Errorf("db: create parent dir: %w", err)
		}
	}

	dsn := buildDSN(path)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}

	db.SetMaxOpenConns(1)
	if path != ":memory:" {
		// SQLite write contention. modernc honours busy_timeout.
		if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
			_ = db.Close()
			if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.EACCES) {
				return nil, fmt.Errorf("db: open %q: %w. The container runs as UID 1000:1000; the host directory mounted at %q must be writable by that UID", path, err, filepath.Dir(path))
			}
			return nil, fmt.Errorf("db: set busy_timeout: %w", err)
		}
	}

	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db: enable foreign keys: %w", err)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil && path != ":memory:" {
		_ = db.Close()
		return nil, fmt.Errorf("db: set WAL: %w", err)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA synchronous = NORMAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db: set synchronous: %w", err)
	}

	if err := Migrate(ctx, db, migrationsFS); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db: migrate: %w", err)
	}

	return db, nil
}

func buildDSN(path string) string {
	if path == ":memory:" {
		return "file::memory:?cache=shared"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	// _pragma= sets additional connection-level PRAGMAs that modernc
	// applies when the connection is first opened. We also call
	// PRAGMA foreign_keys = ON explicitly above.
	return fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", abs)
}

// Migrate runs every .sql file in migrationsFS whose name matches
// NNNN_name.sql in order, skipping any whose NNNN version is already
// recorded in schema_migrations. The migration table itself is
// created on the first call.
func Migrate(ctx context.Context, db *sql.DB, fsys fs.FS) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := loadAppliedVersions(ctx, db)
	if err != nil {
		return err
	}

	migrations, err := loadMigrationFiles(fsys)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if _, ok := applied[m.version]; ok {
			continue
		}
		slog.Info("applying migration", "version", m.version, "name", m.name)
		if err := applyMigration(ctx, db, m); err != nil {
			return fmt.Errorf("apply %04d_%s: %w", m.version, m.name, err)
		}
	}
	return nil
}

type migration struct {
	version int
	name    string
	body    string
}

func loadMigrationFiles(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		// Expect NNNN_name.sql.
		base := strings.TrimSuffix(e.Name(), ".sql")
		i := strings.IndexByte(base, '_')
		if i <= 0 {
			return nil, fmt.Errorf("migration %q does not match NNNN_name.sql", e.Name())
		}
		v, err := strconv.Atoi(base[:i])
		if err != nil {
			return nil, fmt.Errorf("migration %q: invalid version: %w", e.Name(), err)
		}
		body, err := fs.ReadFile(fsys, "migrations/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		out = append(out, migration{version: v, name: base[i+1:], body: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func loadAppliedVersions(ctx context.Context, db *sql.DB) (map[int]struct{}, error) {
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()
	out := map[int]struct{}{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = struct{}{}
	}
	return out, rows.Err()
}

func applyMigration(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, m.body); err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES (?)", m.version); err != nil {
		return fmt.Errorf("record: %w", err)
	}
	return tx.Commit()
}

// Now returns the current UTC time formatted as RFC3339. Used in
// default-value INSERTs and audit log entries.
func Now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
