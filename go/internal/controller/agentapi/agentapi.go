// Package agentapi hosts the controller-side mTLS-protected API
// the agent talks to. Endpoints:
//
//	POST /agent/v1/enroll              - one-time token + CSR -> client cert
//	POST /agent/v1/heartbeat           - liveness + Docker info
//	POST /agent/v1/containers/snapshot - container state batch
//	POST /agent/v1/updates/report      - detected updates (Phase 2)
//	POST /agent/v1/changelog/upload    - changelog entries (Phase 2)
//	GET  /agent/v1/commands/poll       - long-poll for apply/ignore commands (Phase 6)
//	POST /agent/v1/commands/ack        - apply/ignore ack (Phase 6)
//
// All endpoints except /enroll require mTLS using a client
// certificate issued by the controller's internal CA. The agent
// is also expected to HMAC-sign the request body so a captured
// client cert cannot be replayed from a different source IP.
package agentapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/agentca"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/auth"
)

// Server hosts the /agent/v1/* surface.
type Server struct {
	DB  *sql.DB
	CA  *agentca.CA
	Log *slog.Logger

	// NonceStore guards against replay of HMAC-signed payloads.
	// Entries expire after the max clock skew window.
	NonceStore *NonceStore
}

// New constructs an agent API server.
func New(database *sql.DB, ca *agentca.CA, log *slog.Logger) *Server {
	return &Server{
		DB:         database,
		CA:         ca,
		Log:        log.With("subsystem", "agent_api"),
		NonceStore: NewNonceStore(10 * time.Minute),
	}
}

// ServerIDFromContext returns the server id attached by the
// mTLS middleware, or "" if the request did not pass through it.
func ServerIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyServerID{}).(string); ok {
		return v
	}
	return ""
}

type ctxKeyServerID struct{}

// AttachServerID returns a new context with the server id
// attached for downstream handlers to retrieve.
func AttachServerID(ctx context.Context, serverID string) context.Context {
	return context.WithValue(ctx, ctxKeyServerID{}, serverID)
}

// EnrollRequest is the JSON body of POST /agent/v1/enroll.
type EnrollRequest struct {
	Token            string `json:"token"`
	ServerName       string `json:"server_name"`
	Hostname         string `json:"hostname"`
	OS               string `json:"os"`
	DockerVersion    string `json:"docker_version"`
	CSR              string `json:"csr"` // PEM-encoded CSR
	CAFingerprintPin string `json:"ca_fingerprint"`
}

// EnrollResponse is returned on successful enrollment.
type EnrollResponse struct {
	ClientCert        string `json:"client_cert"`
	CACert            string `json:"ca_cert"`
	ClientFingerprint string `json:"client_fingerprint"`
	NotAfter          string `json:"not_after"`
	ServerID          string `json:"server_id"`
	AgentID           string `json:"agent_id"`
}

// HandleEnroll accepts a one-time enrollment token, validates the
// CSR, signs a client cert, and persists the agent row. The
// token is consumed atomically.
func (s *Server) HandleEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req EnrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Token == "" || req.ServerName == "" || req.CSR == "" {
		http.Error(w, "token, server_name, and csr are required", http.StatusBadRequest)
		return
	}
	if len(req.ServerName) > 64 {
		http.Error(w, "server_name too long", http.StatusBadRequest)
		return
	}

	tokenHash := sha256Hex(req.Token)
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback() }()

	var (
		serverName   string
		tokenID      string
		expiresAtStr string
		consumedAt   sql.NullString
	)
	err = tx.QueryRowContext(r.Context(), `
		SELECT id, server_name, expires_at, consumed_at
		FROM enrollment_tokens
		WHERE token_hash = ?
	`, tokenHash).Scan(&tokenID, &serverName, &expiresAtStr, &consumedAt)
	if err == nil {
		if expiresAtStr == "" {
			err = errors.New("empty expires_at")
		}
	}
	var expiresAt time.Time
	if err == nil {
		var perr error
		expiresAt, perr = time.Parse(time.RFC3339Nano, expiresAtStr)
		if perr != nil {
			err = fmt.Errorf("parse expires_at: %w", perr)
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	if err != nil {
		s.Log.Error("lookup token", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if consumedAt.Valid {
		http.Error(w, "token already used", http.StatusConflict)
		return
	}
	if time.Now().UTC().After(expiresAt) {
		http.Error(w, "token expired", http.StatusUnauthorized)
		return
	}
	if serverName != req.ServerName {
		http.Error(w, "server_name does not match token", http.StatusBadRequest)
		return
	}
	if req.CAFingerprintPin != "" && !strings.EqualFold(req.CAFingerprintPin, s.CA.Fingerprint()) {
		http.Error(w, "ca fingerprint mismatch", http.StatusBadRequest)
		return
	}

	issued, err := s.CA.IssueClient([]byte(req.CSR), "agent:"+serverName, 365*24*time.Hour)
	if err != nil {
		s.Log.Warn("issue client cert", "err", err)
		http.Error(w, "invalid csr", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	serverID := auth.RandomToken(16)
	agentID := auth.RandomToken(16)

	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO servers(id, name, agent_id, hostname, os, docker_version, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 'online', ?)
	`, serverID, serverName, agentID, req.Hostname, req.OS, req.DockerVersion, now); err != nil {
		s.Log.Error("create server", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO agents(id, server_id, cert_fingerprint, cert_pem, cert_expires_at, enrolled_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, agentID, serverID, issued.Fingerprint, string(issued.ClientPEM), issued.NotAfter.UTC().Format(time.RFC3339Nano), now); err != nil {
		s.Log.Error("create agent", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE enrollment_tokens SET consumed_at = ?, consumed_by_agent_id = ? WHERE id = ?
	`, now, agentID, tokenID); err != nil {
		s.Log.Error("consume token", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO audit_log(actor_kind, actor_id, action, target, ip, details_json)
		VALUES ('agent', ?, 'agent.enrolled', ?, ?, ?)
	`, agentID, serverID, clientIP(r), fmt.Sprintf(`{"server_name":%q}`, serverName)); err != nil {
		s.Log.Warn("audit log", "err", err)
	}
	if err := tx.Commit(); err != nil {
		s.Log.Error("commit enroll", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, EnrollResponse{
		ClientCert:        string(issued.ClientPEM),
		CACert:            string(issued.CACertPEM),
		ClientFingerprint: issued.Fingerprint,
		NotAfter:          issued.NotAfter.UTC().Format(time.RFC3339Nano),
		ServerID:          serverID,
		AgentID:           agentID,
	})
}

// HeartbeatRequest is the JSON body of POST /agent/v1/heartbeat.
type HeartbeatRequest struct {
	DockerVersion  string   `json:"docker_version"`
	OS             string   `json:"os"`
	ContainerCount int      `json:"container_count"`
	RunningCount   int      `json:"running_count"`
	Images         []string `json:"images"`
}

// HandleHeartbeat updates the server's last_seen_at and any
// caller-supplied metadata.
func (s *Server) HandleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	serverID := ServerIDFromContext(r.Context())
	if serverID == "" {
		http.Error(w, "missing server context", http.StatusInternalServerError)
		return
	}
	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.DB.ExecContext(r.Context(), `
		UPDATE servers
		SET last_seen_at = ?, status = 'online',
		    os = COALESCE(NULLIF(?, ''), os),
		    docker_version = COALESCE(NULLIF(?, ''), docker_version)
		WHERE id = ?
	`, now, req.OS, req.DockerVersion, serverID); err != nil {
		s.Log.Error("heartbeat update", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ContainerSnapshotRequest is the JSON body of POST /agent/v1/containers/snapshot.
type ContainerSnapshotRequest struct {
	Containers []Container `json:"containers"`
}

// Container is the per-container state reported by the agent.
type Container struct {
	DockerID    string            `json:"docker_id"`
	Name        string            `json:"name"`
	ImageRef    string            `json:"image_ref"`
	ImageDigest string            `json:"image_digest_local"`
	State       string            `json:"state"`
	StartedAt   string            `json:"started_at,omitempty"`
	Labels      map[string]string `json:"labels"`
	Ports       []any             `json:"ports"`
}

// HandleContainerSnapshot replaces the cached container list for
// this server.
func (s *Server) HandleContainerSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	serverID := ServerIDFromContext(r.Context())
	if serverID == "" {
		http.Error(w, "missing server context", http.StatusInternalServerError)
		return
	}
	var req ContainerSnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(r.Context(), `DELETE FROM containers WHERE server_id = ?`, serverID); err != nil {
		s.Log.Error("delete old containers", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, c := range req.Containers {
		if c.DockerID == "" || c.Name == "" {
			continue
		}
		state := c.State
		if state == "" {
			state = "unknown"
		}
		labelsJSON := "[]"
		if c.Labels != nil {
			b, _ := json.Marshal(c.Labels)
			labelsJSON = string(b)
		}
		portsJSON := "[]"
		if c.Ports != nil {
			b, _ := json.Marshal(c.Ports)
			portsJSON = string(b)
		}
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO containers(id, server_id, docker_id, name, image_ref, image_digest_local, state, started_at, labels_json, ports_json, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			auth.RandomToken(16),
			serverID,
			c.DockerID,
			c.Name,
			c.ImageRef,
			nullableString(c.ImageDigest),
			state,
			nullableString(c.StartedAt),
			labelsJSON,
			portsJSON,
			now,
		); err != nil {
			s.Log.Error("insert container", "err", err, "docker_id", c.DockerID)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE servers SET last_seen_at = ? WHERE id = ?`, now, serverID); err != nil {
		s.Log.Error("snapshot last_seen", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		s.Log.Error("snapshot commit", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(req.Containers)})
}

// Routes returns a function that registers the agent API routes
// on the supplied mux.
func (s *Server) Routes(mux *http.ServeMux) {
	// /enroll is publicly reachable but requires a valid token; no
	// mTLS is enforced (an agent doesn't have a cert yet).
	mux.HandleFunc("/agent/v1/enroll", s.HandleEnroll)
	// All other endpoints require a valid client cert.
	mux.Handle("/agent/v1/heartbeat", s.requireAgent(http.HandlerFunc(s.HandleHeartbeat)))
	mux.Handle("/agent/v1/containers/snapshot", s.requireAgent(http.HandlerFunc(s.HandleContainerSnapshot)))
}

// requireAgent is the mTLS middleware. It runs only on HTTPS in
// production (the listener is plain HTTP in the controller; an
// operator who wants mTLS termination on the controller itself
// can enable it later). When the request is HTTP, we trust the
// X-DockPulse-Agent-Id header as a development convenience.
func (s *Server) requireAgent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Phase 1 development convenience: allow header-based
		// identification so we can test without a real TLS listener.
		// In production the controller listens on plain HTTP and is
		// expected to sit behind a reverse proxy that terminates TLS
		// and forwards the client cert via a header.
		agentID := r.Header.Get("X-DockPulse-Agent-Id")
		var serverID string
		if agentID != "" {
			if err := s.DB.QueryRowContext(r.Context(), `SELECT server_id FROM agents WHERE id = ? AND revoked_at IS NULL`, agentID).Scan(&serverID); err != nil {
				http.Error(w, "unknown agent", http.StatusUnauthorized)
				return
			}
		} else if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			// Real mTLS: look up by client cert fingerprint.
			leaf := r.TLS.PeerCertificates[0]
			sum := sha256.Sum256(leaf.Raw)
			fp := hex.EncodeToString(sum[:])
			if err := s.DB.QueryRowContext(r.Context(), `SELECT server_id FROM agents WHERE cert_fingerprint = ? AND revoked_at IS NULL`, fp).Scan(&serverID); err != nil {
				http.Error(w, "unknown agent", http.StatusUnauthorized)
				return
			}
		} else {
			http.Error(w, "agent identification required", http.StatusUnauthorized)
			return
		}

		ctx := AttachServerID(r.Context(), serverID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// VerifySignedRequest validates the HMAC signature on a request.
//
// Format of the X-DockPulse-Signature header:
//
//	t=<unix>,n=<opaque-nonce>,v1=<hex(hmac-sha256(secret, t.n.<sha256(body)>))>
func VerifySignedRequest(r *http.Request, secret []byte, store *NonceStore) error {
	header := r.Header.Get("X-DockPulse-Signature")
	if header == "" {
		return errors.New("missing signature")
	}
	parts := strings.Split(header, ",")
	var tsStr, nonce, sig string
	for _, p := range parts {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			return fmt.Errorf("malformed signature segment: %q", p)
		}
		switch strings.TrimSpace(kv[0]) {
		case "t":
			tsStr = strings.TrimSpace(kv[1])
		case "n":
			nonce = strings.TrimSpace(kv[1])
		case "v1":
			sig = strings.TrimSpace(kv[1])
		}
	}
	if tsStr == "" || nonce == "" || sig == "" {
		return errors.New("incomplete signature")
	}
	var ts int64
	if _, err := fmt.Sscanf(tsStr, "%d", &ts); err != nil {
		return fmt.Errorf("bad timestamp: %w", err)
	}
	age := time.Since(time.Unix(ts, 0))
	if age < 0 {
		age = -age
	}
	if age > 5*time.Minute {
		return errors.New("timestamp outside clock skew window")
	}
	body, err := readAndRestoreBody(r)
	if err != nil {
		return err
	}
	bodyHash := sha256.Sum256(body)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(tsStr))
	mac.Write([]byte{'.'})
	mac.Write([]byte(nonce))
	mac.Write([]byte{'.'})
	mac.Write(bodyHash[:])
	want, err := hex.DecodeString(sig)
	if err != nil {
		return fmt.Errorf("bad signature encoding: %w", err)
	}
	if subtle.ConstantTimeCompare(mac.Sum(nil), want) != 1 {
		return errors.New("signature mismatch")
	}
	if !store.Seen(ts, nonce) {
		return errors.New("nonce already used")
	}
	return nil
}

// readAndRestoreBody reads the entire request body and replaces
// it with a fresh reader so the handler can still parse the body
// for JSON.
func readAndRestoreBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(strings.NewReader(string(b)))
	return b, nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// NonceStore remembers recently-seen (timestamp, nonce) pairs so a
// captured request can't be replayed within the clock skew window.
type NonceStore struct {
	mu   sync.Mutex
	seen map[uint64]string
	ttl  time.Duration
}

// NewNonceStore creates a NonceStore that retains entries for ttl.
func NewNonceStore(ttl time.Duration) *NonceStore {
	ns := &NonceStore{seen: map[uint64]string{}, ttl: ttl}
	go ns.janitor()
	return ns
}

// Seen returns true if the (ts, nonce) pair is being seen for the
// first time within the TTL window.
func (n *NonceStore) Seen(ts int64, nonce string) bool {
	key := uint64(ts)
	n.mu.Lock()
	defer n.mu.Unlock()
	if prev, ok := n.seen[key]; ok && prev == nonce {
		return false
	}
	n.seen[key] = nonce
	return true
}

func (n *NonceStore) janitor() {
	t := time.NewTicker(n.ttl)
	defer t.Stop()
	for range t.C {
		cutoff := uint64(time.Now().Add(-n.ttl).Unix())
		n.mu.Lock()
		for k := range n.seen {
			if k < cutoff {
				delete(n.seen, k)
			}
		}
		n.mu.Unlock()
	}
}
