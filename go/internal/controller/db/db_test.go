package db

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestOpenCreatesParentDirectory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	// The parent directory does NOT exist yet.
	nested := filepath.Join(dir, "nested", "data", "dockpulse.db")
	conn, err := Open(ctx, nested)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if _, err := os.Stat(filepath.Dir(nested)); err != nil {
		t.Fatalf("expected parent dir to be created: %v", err)
	}
}

func TestOpenPermissionDeniedGivesHint(t *testing.T) {
	ctx := context.Background()
	// Create a read-only parent directory.
	dir := t.TempDir()
	roDir := filepath.Join(dir, "ro")
	if err := os.Mkdir(roDir, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o700) })

	// Skip if running as root (the chmod above won't restrict root).
	if os.Geteuid() == 0 {
		t.Skip("test requires a non-root user")
	}

	_, err := Open(ctx, filepath.Join(roDir, "data", "dockpulse.db"))
	if err == nil {
		t.Fatal("expected error from MkdirAll under read-only parent")
	}
	msg := err.Error()
	for _, expect := range []string{"permission denied", "1000"} {
		if !strings.Contains(msg, expect) {
			t.Errorf("error message %q is missing hint %q", msg, expect)
		}
	}
}
