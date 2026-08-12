package controller

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/config"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/db"
)

func newServerWithProxies(t *testing.T, proxies []string) (*Server, *sql.DB) {
	t.Helper()
	sqlDB, err := db.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	s, err := New(config.Controller{
		Common:         config.Common{Mode: config.ModeController},
		Listen:         ":0",
		DBPath:         ":memory:",
		TrustedProxies: proxies,
	}, sqlDB)
	if err != nil {
		t.Skipf("no embedded bundle available in this environment: %v", err)
	}
	return s, sqlDB
}

func TestTrustedRealIPIgnoresHeaderByDefault(t *testing.T) {
	s, _ := newServerWithProxies(t, nil)
	handler := s.trustedRealIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.RemoteAddr))
	}))
	ts := httptest.NewServer(handler)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.42")
	req.RemoteAddr = "192.0.2.1:54321"
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer res.Body.Close()
}

func TestTrustedRealIPAllowsHeaderFromTrustedProxy(t *testing.T) {
	s, _ := newServerWithProxies(t, []string{"127.0.0.0/8"})
	handler := s.trustedRealIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.RemoteAddr))
	}))
	ts := httptest.NewServer(handler)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.42")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer res.Body.Close()
	buf := make([]byte, 64)
	n, _ := res.Body.Read(buf)
	got := string(buf[:n])
	if got != "203.0.113.42:0" {
		t.Fatalf("expected XFF to be honoured for trusted proxy, got RemoteAddr=%q", got)
	}
}

func TestTrustedRealIPRejectsHeaderFromUntrustedProxy(t *testing.T) {
	s, _ := newServerWithProxies(t, []string{"10.0.0.0/8"})
	handler := s.trustedRealIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.RemoteAddr))
	}))
	ts := httptest.NewServer(handler)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.42")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer res.Body.Close()
	buf := make([]byte, 64)
	n, _ := res.Body.Read(buf)
	got := string(buf[:n])
	if got == "203.0.113.42:0" {
		t.Fatalf("XFF must NOT be honoured for untrusted source, got %q", got)
	}
}

func TestTrustedRealIPIgnoresInvalidCIDR(t *testing.T) {
	s, _ := newServerWithProxies(t, []string{"not-a-cidr", "192.0.2.1"})
	handler := s.trustedRealIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.RemoteAddr))
	}))
	ts := httptest.NewServer(handler)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.42")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer res.Body.Close()
	buf := make([]byte, 64)
	n, _ := res.Body.Read(buf)
	if got := string(buf[:n]); got == "203.0.113.42:0" {
		t.Fatalf("XFF must not be honoured when source is not in the (reduced) trusted set: got %q", got)
	}
}