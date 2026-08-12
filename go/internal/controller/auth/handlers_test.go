package auth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/db"
)

func newAuthServer(t *testing.T) (*sql.DB, *Middleware) {
	t.Helper()
	d, err := db.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d, NewMiddleware(d, false)
}

func TestFirstRunFlow(t *testing.T) {
	d, _ := newAuthServer(t)

	get := httptest.NewRequest(http.MethodGet, "/api/v1/firstrun", nil)
	getW := httptest.NewRecorder()
	HandleFirstRunStatus(context.Background(), d).ServeHTTP(getW, get)
	if getW.Code != http.StatusOK {
		t.Fatalf("firstrun status: %d", getW.Code)
	}
	var fr FirstRunStatus
	if err := json.NewDecoder(getW.Body).Decode(&fr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !fr.NeedsSetup {
		t.Fatal("expected NeedsSetup=true on fresh DB")
	}

	body, _ := json.Marshal(FirstRunRequest{
		Username: "admin",
		Password: "correct-horse-battery-staple",
		Email:    "admin@example.com",
	})
	post := httptest.NewRequest(http.MethodPost, "/api/v1/firstrun", bytes.NewReader(body))
	post.Header.Set("Content-Type", "application/json")
	postW := httptest.NewRecorder()
	HandleFirstRunCreate(context.Background(), d, false).ServeHTTP(postW, post)
	if postW.Code != http.StatusCreated {
		t.Fatalf("firstrun create: %d %s", postW.Code, postW.Body.String())
	}

	// Second call must be rejected.
	post2 := httptest.NewRequest(http.MethodPost, "/api/v1/firstrun", bytes.NewReader(body))
	post2.Header.Set("Content-Type", "application/json")
	post2W := httptest.NewRecorder()
	HandleFirstRunCreate(context.Background(), d, false).ServeHTTP(post2W, post2)
	if post2W.Code != http.StatusConflict {
		t.Fatalf("second firstrun: expected 409, got %d", post2W.Code)
	}

	// After init, NeedsSetup should be false.
	getW2 := httptest.NewRecorder()
	HandleFirstRunStatus(context.Background(), d).ServeHTTP(getW2, get)
	if err := json.NewDecoder(getW2.Body).Decode(&fr); err != nil {
		t.Fatalf("decode 2: %v", err)
	}
	if fr.NeedsSetup {
		t.Fatal("expected NeedsSetup=false after init")
	}
}

func TestFirstRunValidates(t *testing.T) {
	d, _ := newAuthServer(t)
	cases := []FirstRunRequest{
		{Username: "ab", Password: "longenoughpassword"},                 // short username
		{Username: "valid", Password: "short"},                          // short password
		{Username: "valid name with spaces", Password: "longenoughpassword"}, // bad chars
	}
	for _, c := range cases {
		body, _ := json.Marshal(c)
		r := httptest.NewRequest(http.MethodPost, "/api/v1/firstrun", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		HandleFirstRunCreate(context.Background(), d, false).ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for %+v, got %d (%s)", c, w.Code, w.Body.String())
		}
	}
}

func TestLoginAndMe(t *testing.T) {
	d, mw := newAuthServer(t)

	// Bootstrap an admin.
	hash, _ := HashPassword("correct-horse-battery-staple")
	if _, err := CreateUser(context.Background(), d, "admin", "", "admin", hash); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// Login with wrong password.
	body, _ := json.Marshal(LoginRequest{Username: "admin", Password: "wrong"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/login", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	HandleLogin(context.Background(), d, false).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", w.Code)
	}

	// Login with correct password.
	body, _ = json.Marshal(LoginRequest{Username: "admin", Password: "correct-horse-battery-staple"})
	r = httptest.NewRequest(http.MethodPost, "/api/v1/login", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	HandleLogin(context.Background(), d, false).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	var cookies []*http.Cookie
	for _, c := range w.Result().Cookies() {
		cookies = append(cookies, c)
	}
	var sessionCookie, csrfCookie *http.Cookie
	for _, c := range cookies {
		switch c.Name {
		case CookieSessionID:
			sessionCookie = c
		case CookieCSRFToken:
			csrfCookie = c
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("expected session cookie")
	}
	if csrfCookie == nil || csrfCookie.Value == "" {
		t.Fatal("expected csrf cookie")
	}
	if sessionCookie.HttpOnly != true {
		t.Error("session cookie must be HttpOnly")
	}
	if csrfCookie.HttpOnly != false {
		t.Error("csrf cookie must NOT be HttpOnly so the SPA can read it")
	}

	// /me without cookie -> 401.
	meR := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	meW := httptest.NewRecorder()
	mw.Wrap(HandleMe(context.Background(), d)).ServeHTTP(meW, meR)
	if meW.Code != http.StatusUnauthorized {
		t.Fatalf("me without cookie: %d", meW.Code)
	}

	// /me with cookie -> 200.
	meR = httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	meR.AddCookie(sessionCookie)
	meR.AddCookie(csrfCookie)
	meW = httptest.NewRecorder()
	mw.Wrap(HandleMe(context.Background(), d)).ServeHTTP(meW, meR)
	if meW.Code != http.StatusOK {
		t.Fatalf("me with cookie: %d", meW.Code)
	}
	bodyBytes, _ := io.ReadAll(meW.Body)
	if !strings.Contains(string(bodyBytes), `"username":"admin"`) {
		t.Fatalf("expected admin in /me body, got %s", bodyBytes)
	}
	if !strings.Contains(string(bodyBytes), `"csrf_token":"`+csrfCookie.Value+`"`) {
		t.Fatalf("expected CSRF token in /me body, got %s", bodyBytes)
	}
}

func TestCSRFRequiredForMutating(t *testing.T) {
	d, mw := newAuthServer(t)
	hash, _ := HashPassword("correct-horse-battery-staple")
	if _, err := CreateUser(context.Background(), d, "admin", "", "admin", hash); err != nil {
		t.Fatalf("create user: %v", err)
	}
	body, _ := json.Marshal(LoginRequest{Username: "admin", Password: "correct-horse-battery-staple"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/login", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	HandleLogin(context.Background(), d, false).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d", w.Code)
	}
	var sessionCookie, csrfCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		switch c.Name {
		case CookieSessionID:
			sessionCookie = c
		case CookieCSRFToken:
			csrfCookie = c
		}
	}

	// POST /api/v1/logout without CSRF header -> 403
	logout := httptest.NewRequest(http.MethodPost, "/api/v1/logout", nil)
	logout.AddCookie(sessionCookie)
	logout.AddCookie(csrfCookie)
	w = httptest.NewRecorder()
	mw.Wrap(HandleLogout(context.Background(), d, false)).ServeHTTP(w, logout)
	if w.Code != http.StatusForbidden {
		t.Fatalf("logout without CSRF: %d", w.Code)
	}

	// POST /api/v1/logout with CSRF header -> 200
	logout = httptest.NewRequest(http.MethodPost, "/api/v1/logout", nil)
	logout.AddCookie(sessionCookie)
	logout.AddCookie(csrfCookie)
	logout.Header.Set(HeaderCSRFToken, csrfCookie.Value)
	w = httptest.NewRecorder()
	mw.Wrap(HandleLogout(context.Background(), d, false)).ServeHTTP(w, logout)
	if w.Code != http.StatusOK {
		t.Fatalf("logout with CSRF: %d (%s)", w.Code, w.Body.String())
	}

	// After logout, the session is gone.
	if _, err := GetSession(context.Background(), d, sessionCookie.Value); err == nil {
		t.Fatal("expected session to be deleted")
	}
}

func TestSessionExpiry(t *testing.T) {
	d, mw := newAuthServer(t)
	hash, _ := HashPassword("correct-horse-battery-staple")
	u, _ := CreateUser(context.Background(), d, "admin", "", "admin", hash)
	sess, err := CreateSession(context.Background(), d, u.ID, "ua", "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Backdate the session to be expired.
	expiredAt := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if _, err := d.ExecContext(context.Background(), `UPDATE sessions SET expires_at = ? WHERE id = ?`, expiredAt, sess.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	r.AddCookie(&http.Cookie{Name: CookieSessionID, Value: sess.ID})
	r.AddCookie(&http.Cookie{Name: CookieCSRFToken, Value: sess.CSRFToken})
	w := httptest.NewRecorder()
	mw.Wrap(HandleMe(context.Background(), d)).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired session: %d", w.Code)
	}
}
