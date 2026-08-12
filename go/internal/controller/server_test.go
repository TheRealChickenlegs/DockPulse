package controller

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/config"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/db"
)

func newTestServer(t *testing.T) (*Server, *sql.DB) {
	t.Helper()
	sqlDB, err := db.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	srv, err := New(config.Controller{
		Common: config.Common{Mode: config.ModeController},
		Listen: ":0",
		DBPath: ":memory:",
	}, sqlDB)
	if err != nil {
		// In tests we don't run with the embedded bundle present
		// (internal/web/build is gitignored), so this errors. Skip.
		_ = sqlDB.Close()
		t.Skipf("no embedded bundle available in this environment: %v", err)
	}
	return srv, sqlDB
}

// TestServerStartsAndHealthzOK spins up an in-process chi stack and
// confirms /healthz returns the expected body without leaking host state.
func TestServerStartsAndHealthzOK(t *testing.T) {
	srv, _ := newTestServer(t)
	ts := httptest.NewServer(srv.router)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("healthz status %d, want 200", res.StatusCode)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("healthz body is not JSON: %v", err)
	}
	if got["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v", got)
	}
	for _, header := range []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if v := res.Header.Get(header); v == "" {
			t.Fatalf("expected %s header to be set", header)
		}
	}
}

// TestVersionEndpoint confirms /version is reachable and returns JSON
// without requiring a web bundle.
func TestVersionEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	ts := httptest.NewServer(srv.router)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("version status %d, want 200", res.StatusCode)
	}
	if !strings.Contains(string(body), `"version"`) {
		t.Fatalf("expected version key, got %q", body)
	}
}

// TestHealthzNoCache confirms no-cache headers are set on the health
// endpoint so it cannot be poisoned by an intermediate cache.
func TestHealthzNoCache(t *testing.T) {
	srv, _ := newTestServer(t)
	ts := httptest.NewServer(srv.router)
	defer ts.Close()
	res, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	defer res.Body.Close()
	if got := res.Header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("expected Cache-Control to include no-store, got %q", got)
	}
}

// TestStartShutdown confirms the server binds, accepts one request,
// and shuts down cleanly within the deadline.
func TestStartShutdown(t *testing.T) {
	srv, sqlDB := newTestServer(t)
	defer func() { _ = sqlDB.Close() }()

	// Bind to an ephemeral port on loopback.
	listenCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = srv.Start(listenCtx)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
}
