package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// LoginRequest is the JSON body accepted by POST /api/v1/login.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is the JSON body returned by POST /api/v1/login on
// success. The session is also set in cookies by the handler.
type LoginResponse struct {
	User User `json:"user"`
}

// MeResponse is returned by GET /api/v1/me.
type MeResponse struct {
	User         User   `json:"user"`
	CSRFToken    string `json:"csrf_token"`
	HasUsers     bool   `json:"-"` // populated by handler via the users table
	FirstRunHint bool   `json:"first_run_hint"`
}

// FirstRunStatus is returned by GET /api/v1/firstrun.
type FirstRunStatus struct {
	NeedsSetup bool `json:"needs_setup"`
}

// FirstRunRequest is the JSON body accepted by POST /api/v1/firstrun.
// It is only honoured when the users table is empty.
type FirstRunRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

// HandleFirstRunStatus reports whether the users table is empty.
// The endpoint is always unauthenticated.
func HandleFirstRunStatus(ctx context.Context, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n, err := CountUsers(ctx, db)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, FirstRunStatus{NeedsSetup: n == 0})
	}
}

// HandleFirstRunCreate creates the first admin user. Returns 409
// if any user already exists. The new user is created with role
// 'admin' and is automatically logged in.
func HandleFirstRunCreate(ctx context.Context, db *sql.DB, cookieSecure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		n, err := CountUsers(ctx, db)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if n > 0 {
			writeError(w, http.StatusConflict, "already initialised")
			return
		}

		var req FirstRunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		req.Username = strings.TrimSpace(req.Username)
		if err := validateNewUser(req.Username, req.Password); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		hash, err := HashPassword(req.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "hash failure")
			return
		}
		u, err := CreateUser(ctx, db, req.Username, req.Email, "admin", hash)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not create user")
			return
		}

		sess, err := CreateSession(ctx, db, u.ID, r.UserAgent(), clientIP(r), DefaultSessionTTL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not start session")
			return
		}
		SetSessionCookies(w, sess, cookieSecure)
		writeJSON(w, http.StatusCreated, LoginResponse{User: *u})
	}
}

// HandleLogin accepts username + password, verifies the hash, and
// sets a session cookie pair on success. Rate limiting is applied
// by the reverse proxy.
func HandleLogin(ctx context.Context, db *sql.DB, cookieSecure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if req.Username == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "username and password required")
			return
		}

		u, err := GetUserByUsername(ctx, db, req.Username)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// Constant-time-ish: still hash a dummy to keep timing
				// similar to the user-exists path. argon2 of a 16-byte
				// random salt is enough.
				_, _ = HashPassword(req.Password)
				writeError(w, http.StatusUnauthorized, "invalid credentials")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		if u.Disabled {
			writeError(w, http.StatusForbidden, "account disabled")
			return
		}

		ok, err := VerifyPassword(u.passwordHash, req.Password)
		if err != nil || !ok {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}

		sess, err := CreateSession(ctx, db, u.ID, r.UserAgent(), clientIP(r), DefaultSessionTTL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not start session")
			return
		}
		SetSessionCookies(w, sess, cookieSecure)

		// Best-effort last_login_at update.
		_, _ = db.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`,
			time.Now().UTC().Format(time.RFC3339Nano), u.ID)

		// Refresh the public projection so the SPA has the new last_login_at.
		fresh, err := GetUserByID(ctx, db, u.ID)
		if err == nil {
			u = fresh
		}

		writeJSON(w, http.StatusOK, LoginResponse{User: *u})
	}
}

// HandleLogout deletes the current session and clears the cookies.
func HandleLogout(ctx context.Context, db *sql.DB, cookieSecure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		sess, ok := SessionFrom(r.Context())
		if ok {
			_ = DeleteSession(ctx, db, sess.ID)
		}
		clearCookie(w, CookieSessionID, cookieSecure)
		clearCookie(w, CookieCSRFToken, cookieSecure)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// HandleMe returns the current user, the CSRF token to echo on
// mutating requests, and a hint that the first-run wizard still
// needs to run (only true when no users exist).
func HandleMe(ctx context.Context, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := UserFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		sess, _ := SessionFrom(r.Context())
		csrf := ""
		if sess != nil {
			csrf = sess.CSRFToken
		}
		n, err := CountUsers(ctx, db)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, MeResponse{
			User:         *u,
			CSRFToken:    csrf,
			FirstRunHint: n == 0,
		})
	}
}

// validateNewUser enforces a minimum bar for new local accounts.
// We deliberately do not require a full email because homelab
// installations often use local accounts; we just check the field
// is not abused.
func validateNewUser(username, password string) error {
	if len(username) < 3 || len(username) > 64 {
		return errors.New("username must be 3-64 characters")
	}
	for _, r := range username {
		if !(r == '-' || r == '_' || r == '.' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return errors.New("username must be alphanumeric, '.', '_', or '-'")
		}
	}
	if len(password) < 12 {
		return errors.New("password must be at least 12 characters")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// clientIP returns the best-effort client IP for a request. The
// controller's trustedRealIP middleware has already validated the
// X-Forwarded-For header against --trusted-proxies, so r.RemoteAddr
// here is the resolved client IP.
func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

// argon2Hash is a private accessor for tests. Production code should
// use VerifyPassword.
func (u *User) passwordHashIsNeverSerialized() string { return u.passwordHash }

// Hidden — used to silence unused warnings if needed.
var _ = fmt.Sprintf
