package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenInMemoryRunsMigrations(t *testing.T) {
	ctx := context.Background()
	conn, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	rows, err := conn.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()

	want := map[string]bool{
		"audit_log":         false,
		"schema_migrations": false,
		"servers":           false,
		"agents":            false,
		"users":             false,
		"sessions":          false,
		"containers":        false,
		"enrollment_tokens": false,
		"oidc_providers":    false,
	}

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected table %q to be created", name)
		}
	}
}

func TestOpenFileDB(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "dockpulse.db")
	conn, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if _, err := conn.ExecContext(ctx, "INSERT INTO users(username, argon2_hash) VALUES (?, ?)", "test", "x"); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	conn2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = conn2.Close() })

	var username string
	if err := conn2.QueryRowContext(ctx, "SELECT username FROM users WHERE username = ?", "test").Scan(&username); err != nil {
		t.Fatalf("read user: %v", err)
	}
	if username != "test" {
		t.Fatalf("expected 'test', got %q", username)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "dockpulse.db")
	conn, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Re-run migrations on the same DB.
	if err := Migrate(ctx, conn, migrationsFS); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	if err := Migrate(ctx, conn, migrationsFS); err != nil {
		t.Fatalf("re-migrate 2: %v", err)
	}

	var count int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 migration row, got %d", count)
	}
}
