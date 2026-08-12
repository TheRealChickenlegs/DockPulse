package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Session is the server-side record of a logged-in user.
type Session struct {
	ID         string
	UserID     string
	CSRFToken  string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
	UserAgent  string
	IP         string
}

// CreateSession inserts a new session row and returns it. The
// caller is responsible for setting the cookie on the response.
func CreateSession(ctx context.Context, db *sql.DB, userID, userAgent, ip string, ttl time.Duration) (*Session, error) {
	now := time.Now().UTC()
	s := &Session{
		ID:         RandomToken(32),
		UserID:     userID,
		CSRFToken:  RandomToken(32),
		CreatedAt:  now,
		ExpiresAt:  now.Add(ttl),
		LastSeenAt: now,
		UserAgent:  userAgent,
		IP:         ip,
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO sessions(id, user_id, csrf_token, created_at, expires_at, last_seen_at, user_agent, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		s.ID,
		s.UserID,
		s.CSRFToken,
		s.CreatedAt.Format(time.RFC3339Nano),
		s.ExpiresAt.Format(time.RFC3339Nano),
		s.LastSeenAt.Format(time.RFC3339Nano),
		s.UserAgent,
		s.IP,
	)
	if err != nil {
		return nil, fmt.Errorf("auth: create session: %w", err)
	}
	return s, nil
}

// GetSession looks up a session by id and returns it. The caller
// must check ExpiresAt and call DeleteSession if expired.
func GetSession(ctx context.Context, db *sql.DB, id string) (*Session, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, user_id, csrf_token, created_at, expires_at, last_seen_at,
		       COALESCE(user_agent, ''), COALESCE(ip, '')
		FROM sessions WHERE id = ?
	`, id)
	s := &Session{}
	var created, expires, lastSeen string
	if err := row.Scan(&s.ID, &s.UserID, &s.CSRFToken, &created, &expires, &lastSeen, &s.UserAgent, &s.IP); err != nil {
		return nil, err
	}
	var err error
	if s.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return nil, fmt.Errorf("auth: parse created_at: %w", err)
	}
	if s.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires); err != nil {
		return nil, fmt.Errorf("auth: parse expires_at: %w", err)
	}
	if s.LastSeenAt, err = time.Parse(time.RFC3339Nano, lastSeen); err != nil {
		return nil, fmt.Errorf("auth: parse last_seen_at: %w", err)
	}
	return s, nil
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

// TouchSession updates the last_seen_at and (if rolling) the
// expires_at columns for an active session.
func TouchSession(ctx context.Context, db *sql.DB, id string, rolling bool) error {
	now := time.Now().UTC()
	if rolling {
		_, err := db.ExecContext(ctx, `
			UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?
		`, now.Format(time.RFC3339Nano), now.Add(DefaultSessionTTL).Format(time.RFC3339Nano), id)
		return err
	}
	_, err := db.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE id = ?`, now.Format(time.RFC3339Nano), id)
	return err
}

// DeleteSession removes a session by id.
func DeleteSession(ctx context.Context, db *sql.DB, id string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// DeleteExpiredSessions is a janitor that should be called
// periodically (e.g. once an hour) to remove dead session rows.
func DeleteExpiredSessions(ctx context.Context, db *sql.DB) (int64, error) {
	res, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountUsers returns the number of users in the database. Used by
// the first-run wizard to know whether to create the bootstrap
// admin on the next login attempt.
func CountUsers(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ErrNoSession is returned by session lookup helpers when no row
// matches. The HTTP layer maps this to 401.
var ErrNoSession = errors.New("auth: session not found")
