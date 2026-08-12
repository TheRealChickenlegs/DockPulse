package agentapi

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/agentca"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/auth"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/db"
)

func newTestServer(t *testing.T) (*Server, *sql.DB, *agentca.CA) {
	t.Helper()
	d, err := db.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	caDir := t.TempDir()
	ca, err := agentca.LoadOrCreate(caDir)
	if err != nil {
		t.Fatalf("ca: %v", err)
	}
	// Use the same CA instance for both the server and any
	// test-side fingerprint checks; otherwise the in-memory
	// Server will hold a different CA than the test reference.
	return New(d, ca, slog.New(slog.NewTextHandler(io.Discard, nil))), d, ca
}

func makeUser(t *testing.T, d *sql.DB, username, role string) *auth.User {
	t.Helper()
	hash, _ := auth.HashPassword("correct-horse-battery-staple")
	u, err := auth.CreateUser(context.Background(), d, username, "", role, hash)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func makeToken(t *testing.T, d *sql.DB, userID, name string) string {
	t.Helper()
	now := time.Now().UTC()
	tok := auth.RandomToken(24)
	sum := sha256.Sum256([]byte(tok))
	_, err := d.ExecContext(context.Background(), `
		INSERT INTO enrollment_tokens(id, token_hash, server_name, created_by, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, auth.RandomToken(16), hex.EncodeToString(sum[:]), name, userID, now.Format(time.RFC3339Nano), now.Add(24*time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	return tok
}

func makeCSR(t *testing.T, name string) []byte {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "agent:" + name},
	}, key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

func enroll(t *testing.T, s *Server, tok, name, caPin string) (agentID, serverID string) {
	t.Helper()
	body, _ := json.Marshal(EnrollRequest{Token: tok, ServerName: name, CSR: string(makeCSR(t, name)), CAFingerprintPin: caPin})
	r := httptest.NewRequest(http.MethodPost, "/agent/v1/enroll", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.HandleEnroll(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("enroll %s: %d %s", name, w.Code, w.Body.String())
	}
	var resp EnrollResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	return resp.AgentID, resp.ServerID
}

func TestEnrollRoundTrip(t *testing.T) {
	s, d, ca := newTestServer(t)
	admin := makeUser(t, d, "admin", "admin")
	tok := makeToken(t, d, admin.ID, "server-a")

	// Make the server log to stderr so we can see the underlying error.
	s.Log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	agentID, serverID := enroll(t, s, tok, "server-a", ca.Fingerprint())
	if agentID == "" || serverID == "" {
		t.Fatal("expected agent and server id")
	}

	// Replay must fail (token consumed).
	body, _ := json.Marshal(EnrollRequest{Token: tok, ServerName: "server-a", CSR: string(makeCSR(t, "server-a"))})
	r := httptest.NewRequest(http.MethodPost, "/agent/v1/enroll", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.HandleEnroll(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("replay: %d", w.Code)
	}

	// Server row exists and references the agent.
	var status string
	if err := d.QueryRowContext(context.Background(), `SELECT status FROM servers WHERE id = ?`, serverID).Scan(&status); err != nil {
		t.Fatalf("server: %v", err)
	}
	if status != "online" {
		t.Fatalf("expected online, got %s", status)
	}
}

func TestEnrollRejectsWrongCAPin(t *testing.T) {
	s, d, _ := newTestServer(t)
	admin := makeUser(t, d, "admin", "admin")
	tok := makeToken(t, d, admin.ID, "server-b")

	body, _ := json.Marshal(EnrollRequest{Token: tok, ServerName: "server-b", CSR: string(makeCSR(t, "server-b")), CAFingerprintPin: "deadbeef"})
	r := httptest.NewRequest(http.MethodPost, "/agent/v1/enroll", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.HandleEnroll(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad CA pin, got %d", w.Code)
	}
}

func TestEnrollRejectsExpired(t *testing.T) {
	s, d, _ := newTestServer(t)
	admin := makeUser(t, d, "admin", "admin")
	tok := makeToken(t, d, admin.ID, "server-z")
	// Backdate expiry to one minute ago.
	if _, err := d.ExecContext(context.Background(), `UPDATE enrollment_tokens SET expires_at = ? WHERE server_name = ?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), "server-z"); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	body, _ := json.Marshal(EnrollRequest{Token: tok, ServerName: "server-z", CSR: string(makeCSR(t, "server-z"))})
	r := httptest.NewRequest(http.MethodPost, "/agent/v1/enroll", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.HandleEnroll(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired token, got %d", w.Code)
	}
}

func TestHeartbeatRequiresAgentID(t *testing.T) {
	s, _, _ := newTestServer(t)
	body, _ := json.Marshal(HeartbeatRequest{DockerVersion: "1"})
	r := httptest.NewRequest(http.MethodPost, "/agent/v1/heartbeat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.HandleHeartbeat(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (no server context), got %d", w.Code)
	}
}

func TestContainerSnapshotReplace(t *testing.T) {
	s, d, ca := newTestServer(t)
	admin := makeUser(t, d, "admin", "admin")
	tok := makeToken(t, d, admin.ID, "server-d")
	agentID, serverID := enroll(t, s, tok, "server-d", ca.Fingerprint())

	post := func(containers []Container) int {
		t.Helper()
		body, _ := json.Marshal(ContainerSnapshotRequest{Containers: containers})
		r := httptest.NewRequest(http.MethodPost, "/agent/v1/containers/snapshot", bytes.NewReader(body))
		r.Header.Set("X-DockPulse-Agent-Id", agentID)
		r = r.WithContext(AttachServerID(r.Context(), serverID))
		w := httptest.NewRecorder()
		s.HandleContainerSnapshot(w, r)
		return w.Code
	}
	if code := post([]Container{
		{DockerID: "a", Name: "web", ImageRef: "nginx:1.25", State: "running"},
		{DockerID: "b", Name: "db", ImageRef: "postgres:16", State: "exited"},
	}); code != http.StatusOK {
		t.Fatalf("first snapshot: %d", code)
	}
	if code := post([]Container{
		{DockerID: "a", Name: "web", ImageRef: "nginx:1.25", State: "running"},
	}); code != http.StatusOK {
		t.Fatalf("second snapshot: %d", code)
	}

	var n int
	if err := d.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM containers WHERE server_id = ?`, serverID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row after replace, got %d", n)
	}
}

func TestListServersAndContainers(t *testing.T) {
	s, d, ca := newTestServer(t)
	admin := makeUser(t, d, "admin", "admin")
	tok := makeToken(t, d, admin.ID, "server-e")
	agentID, serverID := enroll(t, s, tok, "server-e", ca.Fingerprint())

	body, _ := json.Marshal(ContainerSnapshotRequest{Containers: []Container{
		{DockerID: "a", Name: "web", ImageRef: "nginx:1.25", State: "running"},
		{DockerID: "b", Name: "db", ImageRef: "postgres:16", State: "running"},
	}})
	r := httptest.NewRequest(http.MethodPost, "/agent/v1/containers/snapshot", bytes.NewReader(body))
	r.Header.Set("X-DockPulse-Agent-Id", agentID)
	r = r.WithContext(AttachServerID(r.Context(), serverID))
	s.HandleContainerSnapshot(httptest.NewRecorder(), r)

	// ListServers
	w := httptest.NewRecorder()
	HandleListServers(context.Background(), d).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list servers: %d", w.Code)
	}
	var lr struct {
		Servers []ServerListItem `json:"servers"`
	}
	if err := json.NewDecoder(w.Body).Decode(&lr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(lr.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(lr.Servers))
	}
	if lr.Servers[0].ContainerCount != 2 || lr.Servers[0].RunningCount != 2 {
		t.Fatalf("unexpected counts: %+v", lr.Servers[0])
	}

	// ListContainers
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/api/v1/servers/"+serverID+"/containers", nil)
	r2.SetPathValue("id", serverID)
	HandleListContainers(context.Background(), d).ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list containers: %d", w2.Code)
	}
	var lc struct {
		Containers []ContainerListItem `json:"containers"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&lc); err != nil {
		t.Fatalf("decode containers: %v", err)
	}
	if len(lc.Containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(lc.Containers))
	}
}

func TestCreateEnrollmentTokenAdminOnly(t *testing.T) {
	d, _ := db.Open(context.Background(), ":memory:")
	t.Cleanup(func() { _ = d.Close() })
	makeUser(t, d, "user", "user")
	admin := makeUser(t, d, "admin", "admin")

	req := CreateEnrollmentTokenRequest{ServerName: "x", TTLHours: 1}

	body, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/agents/enroll-token", bytes.NewReader(body))
	r = r.WithContext(auth.ContextWithUser(r.Context(), mustUser(t, d, "user")))
	w := httptest.NewRecorder()
	HandleCreateEnrollmentToken(context.Background(), d, "fp").ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin: expected 403, got %d", w.Code)
	}

	body, _ = json.Marshal(req)
	r = httptest.NewRequest(http.MethodPost, "/api/v1/admin/agents/enroll-token", bytes.NewReader(body))
	r = r.WithContext(auth.ContextWithUser(r.Context(), admin))
	w = httptest.NewRecorder()
	HandleCreateEnrollmentToken(context.Background(), d, "fp").ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("admin: expected 201, got %d %s", w.Code, w.Body.String())
	}
}

func mustUser(t *testing.T, d *sql.DB, name string) *auth.User {
	t.Helper()
	u, err := auth.GetUserByUsername(context.Background(), d, name)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return u
}

func TestSignedRequest(t *testing.T) {
	store := NewNonceStore(time.Minute)
	secret := []byte("super-secret")
	body := []byte(`{"hello":"world"}`)
	ts := time.Now().Unix()
	nonce := "abc123"

	bodyHash := sha256.Sum256(body)
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(intToString(ts)))
	h.Write([]byte{'.'})
	h.Write([]byte(nonce))
	h.Write([]byte{'.'})
	h.Write(bodyHash[:])
	sig := hex.EncodeToString(h.Sum(nil))

	r := httptest.NewRequest(http.MethodPost, "/agent/v1/heartbeat", bytes.NewReader(body))
	r.Header.Set("X-DockPulse-Signature", "t="+intToString(ts)+",n="+nonce+",v1="+sig)
	if err := VerifySignedRequest(r, secret, store); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := VerifySignedRequest(r, secret, store); err == nil {
		t.Fatal("replay should fail")
	}
}

func TestSignedRequestRejectsTampering(t *testing.T) {
	store := NewNonceStore(time.Minute)
	secret := []byte("k")
	body := []byte("hello")
	ts := time.Now().Unix()
	nonce := "n"

	bodyHash := sha256.Sum256(body)
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(intToString(ts)))
	h.Write([]byte{'.'})
	h.Write([]byte(nonce))
	h.Write([]byte{'.'})
	h.Write(bodyHash[:])

	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("hacked")))
	r.Header.Set("X-DockPulse-Signature", "t="+intToString(ts)+",n="+nonce+",v1="+hex.EncodeToString(h.Sum(nil)))
	if err := VerifySignedRequest(r, secret, store); err == nil {
		t.Fatal("tampered body should fail")
	}
}

func TestSignedRequestStaleTimestamp(t *testing.T) {
	store := NewNonceStore(time.Minute)
	secret := []byte("k")
	body := []byte("x")
	ts := time.Now().Add(-10 * time.Minute).Unix()
	nonce := "n"
	bodyHash := sha256.Sum256(body)
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(intToString(ts)))
	h.Write([]byte{'.'})
	h.Write([]byte(nonce))
	h.Write([]byte{'.'})
	h.Write(bodyHash[:])
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	r.Header.Set("X-DockPulse-Signature", "t="+intToString(ts)+",n="+nonce+",v1="+hex.EncodeToString(h.Sum(nil)))
	if err := VerifySignedRequest(r, secret, store); err == nil {
		t.Fatal("stale timestamp should fail")
	}
}

func intToString(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
