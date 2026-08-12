// Package auth provides password hashing, session management, CSRF
// tokens, and the first-run admin creation flow for the controller.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// Password params are tuned for a controller running on a homelab
// host — a small, single-user workload. The OWASP "Cheat Sheet"
// recommended baseline as of 2025-12 is 19 MiB / 2 iters / 1 thread;
// we go larger because we hash rarely (login only) and stronger is
// strictly better.
type passwordParams struct {
	memory      uint32 // KiB
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

var defaultParams = passwordParams{
	memory:      64 * 1024, // 64 MiB
	iterations:  3,
	parallelism: 2,
	saltLength:  16,
	keyLength:   32,
}

// HashPassword returns a self-describing Argon2id encoded hash of
// the password. The result is a single string safe to store in
// SQLite and safe to log in the rare case of an error message.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("auth: empty password")
	}
	salt := make([]byte, defaultParams.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, defaultParams.iterations, defaultParams.memory, defaultParams.parallelism, defaultParams.keyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		defaultParams.memory,
		defaultParams.iterations,
		defaultParams.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether raw matches the encoded Argon2id
// hash. It uses constant-time comparison for the key.
func VerifyPassword(encoded, raw string) (bool, error) {
	if encoded == "" || raw == "" {
		return false, nil
	}
	p, salt, key, err := parseEncodedHash(encoded)
	if err != nil {
		return false, err
	}
	candidate := argon2.IDKey([]byte(raw), salt, p.iterations, p.memory, p.parallelism, p.keyLength)
	return subtle.ConstantTimeCompare(candidate, key) == 1, nil
}

func parseEncodedHash(encoded string) (passwordParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return passwordParams{}, nil, nil, errors.New("auth: invalid hash format")
	}
	if parts[1] != "argon2id" {
		return passwordParams{}, nil, nil, errors.New("auth: unsupported algorithm")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return passwordParams{}, nil, nil, fmt.Errorf("auth: parse version: %w", err)
	}
	if version != argon2.Version {
		return passwordParams{}, nil, nil, errors.New("auth: unsupported version")
	}
	var p passwordParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallelism); err != nil {
		return passwordParams{}, nil, nil, fmt.Errorf("auth: parse params: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return passwordParams{}, nil, nil, fmt.Errorf("auth: decode salt: %w", err)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return passwordParams{}, nil, nil, fmt.Errorf("auth: decode key: %w", err)
	}
	p.saltLength = uint32(len(salt))
	p.keyLength = uint32(len(key))
	return p, salt, key, nil
}

// RandomToken returns a hex-encoded n-byte random token, suitable
// for session IDs, CSRF tokens, and enrollment tokens. It never
// returns an error from crypto/rand in practice; if entropy fails
// we panic because no secure fallback exists.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("auth: entropy source failed: %w", err))
	}
	return hex.EncodeToString(b)
}

// Session lifetime. Rolling — every authenticated request extends
// the expiry up to MaxSessionAge from now.
const (
	DefaultSessionTTL = 24 * time.Hour
	MaxSessionAge     = 30 * 24 * time.Hour
)

// User is the public projection of a row from the users table.
// The argon2 hash is included so the password can be verified in
// the login handler without a second DB round-trip; it is omitted
// from JSON serialisations via the `json:"-"` tag.
type User struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	Email       string     `json:"email"`
	Role        string     `json:"role"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	Disabled    bool       `json:"disabled"`
	passwordHash string    `json:"-"`
}

// GetUserByUsername returns the user with the given (case-insensitive)
// username, or sql.ErrNoRows if no such user exists.
func GetUserByUsername(ctx context.Context, db *sql.DB, username string) (*User, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, username, COALESCE(email, ''), role, argon2_hash, created_at, last_login_at, disabled
		FROM users
		WHERE username = ? COLLATE NOCASE
	`, username)
	return scanUser(row)
}

// GetUserByID returns the user with the given id.
func GetUserByID(ctx context.Context, db *sql.DB, id string) (*User, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, username, COALESCE(email, ''), role, argon2_hash, created_at, last_login_at, disabled
		FROM users WHERE id = ?
	`, id)
	return scanUser(row)
}

// scanUser reads a user row. The created_at and last_login_at
// columns are stored as RFC3339Nano text (modernc.org/sqlite does
// not auto-convert), so we scan into strings and parse.
func scanUser(row *sql.Row) (*User, error) {
	u := &User{}
	var created, lastLogin sql.NullString
	var disabled int
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.passwordHash, &created, &lastLogin, &disabled); err != nil {
		return nil, err
	}
	if created.Valid {
		t, err := time.Parse(time.RFC3339Nano, created.String)
		if err != nil {
			return nil, fmt.Errorf("auth: parse created_at: %w", err)
		}
		u.CreatedAt = t
	}
	if lastLogin.Valid {
		t, err := time.Parse(time.RFC3339Nano, lastLogin.String)
		if err != nil {
			return nil, fmt.Errorf("auth: parse last_login_at: %w", err)
		}
		u.LastLoginAt = &t
	}
	u.Disabled = disabled != 0
	return u, nil
}

// CreateUser inserts a new user with the given (already-hashed)
// password. The caller is responsible for hashing.
func CreateUser(ctx context.Context, db *sql.DB, username, email, role, hash string) (*User, error) {
	id := RandomToken(16)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.ExecContext(ctx, `
		INSERT INTO users(id, username, email, argon2_hash, role, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, username, nullableString(email), hash, role, now)
	if err != nil {
		return nil, err
	}
	return GetUserByID(ctx, db, id)
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
