package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/db"
)

func newTestDB(t *testing.T) (*sql.DB, *User) {
	t.Helper()
	d, err := db.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	hash, err := HashPassword("hunter2")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	u, err := CreateUser(context.Background(), d, "alice", "alice@example.com", "admin", hash)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return d, u
}

func TestCreateAndGetSession(t *testing.T) {
	d, u := newTestDB(t)
	sess, err := CreateSession(context.Background(), d, u.ID, "ua", "127.0.0.1", DefaultSessionTTL)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(sess.ID) != 64 {
		t.Fatalf("expected 64-char session id, got %d", len(sess.ID))
	}
	if len(sess.CSRFToken) != 64 {
		t.Fatalf("expected 64-char csrf token, got %d", len(sess.CSRFToken))
	}

	got, err := GetSession(context.Background(), d, sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.UserID != u.ID || got.CSRFToken != sess.CSRFToken {
		t.Fatalf("round-trip mismatch: %+v vs %+v", got, sess)
	}
}

func TestDeleteSession(t *testing.T) {
	d, u := newTestDB(t)
	sess, err := CreateSession(context.Background(), d, u.ID, "ua", "127.0.0.1", DefaultSessionTTL)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := DeleteSession(context.Background(), d, sess.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := GetSession(context.Background(), d, sess.ID); err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	d, u := newTestDB(t)
	old := time.Now().UTC().Add(-2 * time.Hour)
	sess, err := CreateSession(context.Background(), d, u.ID, "ua", "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := d.ExecContext(context.Background(), `UPDATE sessions SET expires_at = ? WHERE id = ?`, old.Format(time.RFC3339Nano), sess.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if _, err := CreateSession(context.Background(), d, u.ID, "ua", "127.0.0.1", DefaultSessionTTL); err != nil {
		t.Fatalf("create fresh: %v", err)
	}
	n, err := DeleteExpiredSessions(context.Background(), d)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected to remove 1 expired session, removed %d", n)
	}
}

func TestCountUsers(t *testing.T) {
	d, _ := newTestDB(t)
	n, err := CountUsers(context.Background(), d)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 user, got %d", n)
	}
}
