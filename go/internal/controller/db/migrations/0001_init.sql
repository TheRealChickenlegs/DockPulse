-- 0001_init.sql -- initial DockPulse schema (Phase 1)
--
-- Designed for SQLite (WAL). All IDs are TEXT primary keys holding
-- hex-encoded random bytes (16 bytes from crypto/rand). Timestamps are
-- stored as TEXT in RFC3339 UTC.
--
-- The schema_migrations table is created by the migration runner;
-- this file must not touch it.

CREATE TABLE users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE COLLATE NOCASE,
    email TEXT,
    argon2_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_login_at TEXT,
    disabled INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_users_username ON users(username);

-- Sessions are server-side. The cookie carries an opaque, high-entropy
-- token; the server looks up the session row and revokes on logout.
-- csrf_token is a per-session secret returned to the SPA via a
-- non-httpOnly cookie so the SPA can echo it in the X-CSRF-Token
-- header on mutating requests (double-submit pattern).
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    user_agent TEXT,
    ip TEXT
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

-- OIDC provider config (Phase 5). Defined here so future migrations
-- don't have to alter the users table to add a foreign key.
CREATE TABLE oidc_providers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    issuer TEXT NOT NULL,
    client_id TEXT NOT NULL,
    client_secret_enc TEXT NOT NULL,
    scopes TEXT NOT NULL DEFAULT 'openid email profile',
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Servers are the agents we know about. The agent_id column points
-- to the certificate the agent is currently using.
CREATE TABLE servers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    agent_id TEXT,
    hostname TEXT,
    os TEXT,
    docker_version TEXT,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'online', 'offline', 'revoked')),
    last_seen_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    tags_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_servers_status ON servers(status);

-- Agents are the mTLS client certificates issued to a host. A server
-- can be re-enrolled, which creates a new agent row. Only the latest
-- agent row for a server is "active".
CREATE TABLE agents (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    cert_fingerprint TEXT NOT NULL UNIQUE,
    cert_pem TEXT NOT NULL,
    cert_expires_at TEXT NOT NULL,
    enrolled_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_ip TEXT,
    revoked_at TEXT
);
CREATE INDEX idx_agents_server ON agents(server_id);

-- One-time enrollment tokens. The token itself is not stored; we store
-- only a SHA-256 hash so a database leak doesn't grant enrollment.
CREATE TABLE enrollment_tokens (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    server_name TEXT NOT NULL,
    created_by TEXT REFERENCES users(id),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    consumed_by_agent_id TEXT
);
CREATE INDEX idx_enrollment_tokens_expires ON enrollment_tokens(expires_at);

-- Container cache. Populated by agent snapshots. We store the image
-- ref and the local digest; the remote digest + changelog metadata is
-- added in Phase 2.
CREATE TABLE containers (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    docker_id TEXT NOT NULL,
    name TEXT NOT NULL,
    image_ref TEXT NOT NULL,
    image_digest_local TEXT,
    state TEXT NOT NULL DEFAULT 'unknown' CHECK (state IN ('running', 'exited', 'paused', 'restarting', 'removing', 'dead', 'created', 'unknown')),
    started_at TEXT,
    labels_json TEXT NOT NULL DEFAULT '{}',
    ports_json TEXT NOT NULL DEFAULT '[]',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (server_id, docker_id)
);
CREATE INDEX idx_containers_server ON containers(server_id);
CREATE INDEX idx_containers_state ON containers(state);

CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    actor_kind TEXT NOT NULL CHECK (actor_kind IN ('user', 'agent', 'system')),
    actor_id TEXT,
    action TEXT NOT NULL,
    target TEXT,
    ip TEXT,
    user_agent TEXT,
    details_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_audit_log_ts ON audit_log(ts);
CREATE INDEX idx_audit_log_action ON audit_log(action);
CREATE INDEX idx_audit_log_actor ON audit_log(actor_kind, actor_id);
