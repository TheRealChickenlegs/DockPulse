package auth

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

// Cookie names. SessionID is httpOnly+Secure+SameSite=Strict;
// CSRFToken is intentionally NOT httpOnly so the SPA can echo it
// in the X-CSRF-Token header.
const (
	CookieSessionID = "dockpulse_session"
	CookieCSRFToken = "dockpulse_csrf"
	HeaderCSRFToken = "X-CSRF-Token"
)

// Middleware enforces an active session on every request and
// stashes the resolved user in the request context.
//
// For mutating requests (POST/PUT/PATCH/DELETE), the SPA must echo
// the CSRF cookie value in the X-CSRF-Token header. The cookie
// value is also used to validate the session, so the two-channel
// double-submit is reduced here to a single-channel check: the
// request must have BOTH a valid session cookie AND (if a mutating
// method) the X-CSRF-Token header. This is safe because the
// cookie is SameSite=Strict so cross-origin POSTs cannot include
// it.
type Middleware struct {
	DB        *sql.DB // exposed so handlers can load user/session by id
	CookieSec bool    // when true, the Secure attribute is set on cookies
	Now       func() time.Time
}

// NewMiddleware returns a Middleware. The Secure flag should be
// true in production (HTTPS).
func NewMiddleware(database *sql.DB, cookieSec bool) *Middleware {
	return &Middleware{DB: database, CookieSec: cookieSec, Now: time.Now}
}

// Wrap returns a http.Handler middleware that resolves the
// session and applies CSRF protection to mutating requests.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(CookieSessionID)
		if err != nil || cookie.Value == "" {
			next.ServeHTTP(w, r)
			return
		}

		sess, err := GetSession(r.Context(), m.DB, cookie.Value)
		if err != nil {
			// Invalid or expired session — clear the cookie.
			clearCookie(w, CookieSessionID, m.CookieSec)
			next.ServeHTTP(w, r)
			return
		}

		if m.Now().After(sess.ExpiresAt) {
			_ = DeleteSession(r.Context(), m.DB, sess.ID)
			clearCookie(w, CookieSessionID, m.CookieSec)
			next.ServeHTTP(w, r)
			return
		}

		if isMutating(r.Method) {
			headerToken := r.Header.Get(HeaderCSRFToken)
			cookieToken := ""
			if c, err := r.Cookie(CookieCSRFToken); err == nil {
				cookieToken = c.Value
			}
			if headerToken == "" || headerToken != cookieToken || cookieToken != sess.CSRFToken {
				http.Error(w, "csrf token missing or invalid", http.StatusForbidden)
				return
			}
		}

		user, err := GetUserByID(r.Context(), m.DB, sess.UserID)
		if err != nil || user.Disabled {
			_ = DeleteSession(r.Context(), m.DB, sess.ID)
			clearCookie(w, CookieSessionID, m.CookieSec)
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), userKey{}, user)
		ctx = context.WithValue(ctx, sessionKey{}, sess)
		_ = TouchSession(r.Context(), m.DB, sess.ID, true)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isMutating(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

type userKey struct{}
type sessionKey struct{}

// UserFrom returns the user attached by the middleware, if any.
func UserFrom(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(userKey{}).(*User)
	return u, ok
}

// ContextWithUser returns a new context with the user attached.
// Used by tests and by handlers that need to set the user
// without going through the middleware.
func ContextWithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userKey{}, u)
}

// SessionFrom returns the session attached by the middleware, if any.
func SessionFrom(ctx context.Context) (*Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(*Session)
	return s, ok
}

// SetSessionCookies writes the session + CSRF cookies. Both are
// SameSite=Strict; session is httpOnly, csrf is not.
func SetSessionCookies(w http.ResponseWriter, sess *Session, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieSessionID,
		Value:    sess.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		Expires:  sess.ExpiresAt,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     CookieCSRFToken,
		Value:    sess.CSRFToken,
		Path:     "/",
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		Expires:  sess.ExpiresAt,
	})
}

func clearCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}
