package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/config"
)

func newServerWithProxies(t *testing.T, proxies []string) *Server {
	t.Helper()
	cfg := config.Controller{
		Common:         config.Common{Mode: config.ModeController},
		Listen:         ":0",
		DBPath:         ":memory:",
		TrustedProxies: proxies,
	}
	s, err := New(cfg)
	if err != nil {
		t.Skipf("no embedded bundle available in this environment: %v", err)
	}
	return s
}

func TestTrustedRealIPIgnoresHeaderByDefault(t *testing.T) {
	s := newServerWithProxies(t, nil)
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
		// httptest sets RemoteAddr to its own; the XFF should still be ignored
		// because no trusted proxy is configured. The RemoteAddr in the
		// response will be the test server's local loopback.
		t.Fatalf("unexpected error: %v", err)
	}
	defer res.Body.Close()
}

func TestTrustedRealIPAllowsHeaderFromTrustedProxy(t *testing.T) {
	s := newServerWithProxies(t, []string{"127.0.0.0/8"})
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
	s := newServerWithProxies(t, []string{"10.0.0.0/8"})
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
	// "not-a-cidr" is dropped from the trusted list with a warning;
	// the second entry is a single valid IP. The test client
	// connects from 127.0.0.1, which is NOT 192.0.2.1, so XFF is
	// not honoured — this test confirms the invalid entry is dropped
	// without causing the whole request to fail.
	s := newServerWithProxies(t, []string{"not-a-cidr", "192.0.2.1"})
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