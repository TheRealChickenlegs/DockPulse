package agentapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/auth"
)

// CreateEnrollmentTokenRequest is the JSON body of
// POST /api/v1/admin/agents/enroll-token.
type CreateEnrollmentTokenRequest struct {
	ServerName string `json:"server_name"`
	TTLHours   int    `json:"ttl_hours"`
}

// EnrollmentTokenResponse is the JSON body returned to admin
// callers. The token is shown ONCE here; the database only stores
// its hash.
type EnrollmentTokenResponse struct {
	Token       string `json:"token"`
	ServerName  string `json:"server_name"`
	ExpiresAt   string `json:"expires_at"`
	CAFingerprint string `json:"ca_fingerprint"`
}

// HandleCreateEnrollmentToken creates a one-time enrollment
// token. Only admin users may call this.
//
// The token is a 24-byte random hex string. The database stores
// only the SHA-256 hash so a database leak doesn't grant
// enrollment.
func HandleCreateEnrollmentToken(ctx context.Context, db *sql.DB, caFingerprint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		u, ok := auth.UserFrom(r.Context())
		if !ok || u.Role != "admin" {
			http.Error(w, "admin only", http.StatusForbidden)
			return
		}
		var req CreateEnrollmentTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.ServerName) == "" {
			http.Error(w, "server_name required", http.StatusBadRequest)
			return
		}
		if len(req.ServerName) > 64 {
			http.Error(w, "server_name too long", http.StatusBadRequest)
			return
		}
		ttl := time.Duration(req.TTLHours) * time.Hour
		if ttl <= 0 {
			ttl = 24 * time.Hour
		}
		if ttl > 7*24*time.Hour {
			http.Error(w, "ttl_hours must be <= 168", http.StatusBadRequest)
			return
		}

		token := auth.RandomToken(24)
		tokenHash := sha256Hex(token)
		now := time.Now().UTC()
		id := auth.RandomToken(16)
		expires := now.Add(ttl)

		_, err := db.ExecContext(r.Context(), `
			INSERT INTO enrollment_tokens(id, token_hash, server_name, created_by, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, id, tokenHash, req.ServerName, u.ID,
			now.Format(time.RFC3339Nano),
			expires.Format(time.RFC3339Nano))
		if err != nil {
			http.Error(w, "could not create token", http.StatusInternalServerError)
			return
		}
		_, _ = db.ExecContext(r.Context(), `
			INSERT INTO audit_log(actor_kind, actor_id, action, target, details_json)
			VALUES ('user', ?, 'agent.token.create', ?, ?)
		`, u.ID, req.ServerName, fmt.Sprintf(`{"ttl_hours":%d}`, req.TTLHours))

		writeJSON(w, http.StatusCreated, EnrollmentTokenResponse{
			Token:         token,
			ServerName:    req.ServerName,
			ExpiresAt:     expires.Format(time.RFC3339Nano),
			CAFingerprint: caFingerprint,
		})
	}
}

// ServerListItem is the JSON shape of GET /api/v1/servers.
type ServerListItem struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Hostname      string  `json:"hostname"`
	OS            string  `json:"os"`
	DockerVersion string  `json:"docker_version"`
	Status        string  `json:"status"`
	LastSeenAt    *string `json:"last_seen_at"`
	ContainerCount int    `json:"container_count"`
	RunningCount  int     `json:"running_count"`
}

// HandleListServers returns all known servers with summary
// counts from the cached container list.
func HandleListServers(ctx context.Context, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rows, err := db.QueryContext(r.Context(), `
			SELECT s.id, s.name, s.hostname, COALESCE(s.os, ''), COALESCE(s.docker_version, ''),
			       s.status, s.last_seen_at,
			       COUNT(c.id) AS container_count,
			       COALESCE(SUM(CASE WHEN c.state = 'running' THEN 1 ELSE 0 END), 0) AS running_count
			FROM servers s
			LEFT JOIN containers c ON c.server_id = s.id
			GROUP BY s.id
			ORDER BY s.name
		`)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var out []ServerListItem
		for rows.Next() {
			var s ServerListItem
			var lastSeen sql.NullString
			if err := rows.Scan(&s.ID, &s.Name, &s.Hostname, &s.OS, &s.DockerVersion, &s.Status, &lastSeen, &s.ContainerCount, &s.RunningCount); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if lastSeen.Valid {
				s.LastSeenAt = &lastSeen.String
			}
			out = append(out, s)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if out == nil {
			out = []ServerListItem{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"servers": out})
	}
}

// ContainerListItem is the JSON shape of GET /api/v1/servers/{id}/containers.
type ContainerListItem struct {
	ID         string  `json:"id"`
	DockerID   string  `json:"docker_id"`
	Name       string  `json:"name"`
	ImageRef   string  `json:"image_ref"`
	ImageDigest string `json:"image_digest_local"`
	State      string  `json:"state"`
	Stack      string  `json:"stack"`
	StartedAt  *string `json:"started_at"`
	ServerID   string  `json:"server_id"`
	UpdatedAt  string  `json:"updated_at"`
}

// composeProjectLabel is the label Docker Compose sets on every
// container it manages; it identifies the stack the container belongs
// to. Containers without it are not part of a stack.
const composeProjectLabel = "com.docker.compose.project"

// containerStack extracts the Docker Compose stack name from a
// container's stored labels JSON. Empty means the container is not
// managed by a stack.
func containerStack(labelsJSON string) string {
	var labels map[string]string
	if err := json.Unmarshal([]byte(labelsJSON), &labels); err != nil {
		return ""
	}
	return labels[composeProjectLabel]
}

// HandleListContainers returns the cached containers for a server.
func HandleListContainers(ctx context.Context, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		serverID := r.PathValue("id")
		if serverID == "" {
			http.Error(w, "server id required", http.StatusBadRequest)
			return
		}
		rows, err := db.QueryContext(r.Context(), `
			SELECT id, docker_id, name, image_ref, COALESCE(image_digest_local, ''),
			       state, started_at, server_id, updated_at, COALESCE(labels_json, '{}')
			FROM containers WHERE server_id = ?
			ORDER BY name
		`, serverID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var out []ContainerListItem
		for rows.Next() {
			var c ContainerListItem
			var started sql.NullString
			var labelsJSON string
			if err := rows.Scan(&c.ID, &c.DockerID, &c.Name, &c.ImageRef, &c.ImageDigest, &c.State, &started, &c.ServerID, &c.UpdatedAt, &labelsJSON); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			c.Stack = containerStack(labelsJSON)
			if started.Valid {
				c.StartedAt = &started.String
			}
			out = append(out, c)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if out == nil {
			out = []ContainerListItem{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"containers": out})
	}
}
