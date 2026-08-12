# Architecture

> Status: Phase 0 skeleton. The diagrams and contracts below are the target design; only `/healthz`, mode dispatch, logging, config, and the embedded SvelteKit shell are implemented in this phase.

## Topology

```
                ┌────────────────────────────────┐
                │  Reverse proxy (operator)      │
                │  nginx proxy manager / Traefik │
                │  / Caddy / HAProxy             │
                │  + TLS termination             │
                │  + HSTS, CSP, rate limits      │
                └─────────────────┬──────────────┘
                                  │  plain HTTP on private net
                                  ▼
┌──────────────────────────────────────────────────────────────┐
│                  DockPulse Controller (1×)                    │
│  ┌─────────────────────────────┐  ┌────────────────────────┐ │
│  │ SvelteKit static bundle     │  │ Agent API (mTLS)       │ │
│  │ (served by Go via go:embed) │  │ /agent/v1/*            │ │
│  └─────────────────────────────┘  └────────────────────────┘ │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │ Frontend API (session auth, CSRF) /api/v1/*             │ │
│  └─────────────────────────────────────────────────────────┘ │
│  ┌────────────┐  ┌─────────────┐  ┌──────────────────────┐  │
│  │ SQLite WAL │  │ Notifier    │  │ Audit log            │  │
│  │            │  │ registry    │  │                      │  │
│  └────────────┘  └─────────────┘  └──────────────────────┘  │
└──────────────────────────────────────────────────────────────┘
                            ▲
                            │  mTLS + HMAC-signed payloads
                            │  (agents are outbound only)
              ┌─────────────┼─────────────┬──────────────┐
              ▼             ▼             ▼              ▼
        ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐
        │ Agent A  │  │ Agent B  │  │ Agent C  │  │ Agent N  │
        │ Docker   │  │ Docker   │  │ Docker   │  │ Docker   │
        │ +socket  │  │ +socket  │  │ +socket  │  │ +socket  │
        │  proxy   │  │  proxy   │  │  proxy   │  │  proxy   │
        └──────────┘  └──────────┘  └──────────┘  └──────────┘
```

DockPulse does not include a reverse proxy in the default deployment. The controller binds to plain HTTP on a private Docker network and is reached via the operator's existing reverse proxy (nginx proxy manager, Traefik, Caddy, HAProxy, etc.). The controller sets a defensive baseline of security headers itself, but the reverse proxy is the **primary** enforcement point for TLS, HSTS, CSP, and rate limits.

An optional `docker-compose.with-caddy.yml` is provided for users who don't already run a reverse proxy. See `deploy/README.md`.

## Why agents, not direct Docker access

- Avoids opening the Docker API port on every host to the controller.
- Avoids exposing the Docker socket (root-equivalent) inside the controller container.
- Agents only need **outbound** HTTPS to the controller — works behind NAT, CGNAT, and corporate firewalls.
- Per-host credentials (registry logins) stay on the host where they're used.

## Component responsibilities

### Controller (`--mode=controller`, default)

- Listens on plain HTTP on a private Docker network and is reached via the operator's reverse proxy. The default compose stack does not publish 8080 to the host.
- Owns the SQLite database and runs schema migrations on boot.
- Issues mTLS client certificates for newly enrolled agents.
- Dispatches notifications via the configured notifiers.
- Hosts the `frontend API` consumed by the SvelteKit UI.
- Sets a defensive baseline of security headers (X-Content-Type-Options, X-Frame-Options, Referrer-Policy, Permissions-Policy) on every response. The reverse proxy is the primary enforcement point for HSTS, CSP, and rate limits.
- Resolves the client IP only from `X-Forwarded-For` if the connection's remote address is in `--trusted-proxies`; otherwise the connection's real remote address is used. This prevents a client from spoofing its source IP.

### Agent (`--mode=agent`)

- Discovers running containers via the local Docker daemon (through the socket-proxy).
- Periodically resolves the remote digest for each unique `(repo, tag)` using the pluggable registry provider.
- Fetches changelog source metadata for any image with a digest delta.
- Signs and uploads batches to the controller over mTLS.

### Shared library

- HMAC-signed payload envelope (timestamp + nonce + body hash).
- mTLS helpers (cert verification, controller fingerprint pinning).
- Common types: `Container`, `Image`, `Update`, `ChangelogEntry`, `Agent`.

## Process and goroutine model

- One Go process per binary; runtime mode is selected by `--mode`.
- The controller uses `net/http` with one goroutine per request. Background work (notification fan-out, agent cert expiry sweep, digest reconciliation) runs in dedicated goroutines started at boot.
- The agent uses a single supervisor goroutine that owns sub-tasks (Docker poll, registry poll, changelog fetch, reporter). Sub-tasks communicate via channels and can be cancelled via context.

## Data model

See `go/internal/controller/db/migrations/` for the canonical schema. High-level tables:

```
users, sessions, oidc_providers
servers, agents, enrollment_tokens
registries, registry_creds
containers, images, updates
changelog_entries, changelog_links
notifiers, notify_prefs, notify_log
audit_log
```

## APIs

### Frontend API (`/api/v1/...`, session cookie + CSRF)

Implemented in `go/internal/controller/frontend_api`. Endpoints are documented per package; an OpenAPI doc is generated in Phase 1.

### Agent API (`/agent/v1/...`, mTLS + signed payload)

Implemented in `go/internal/controller/agent_api`. Every request carries:

```
X-DockPulse-Signature: t=<unix>,v1=<hex(hmac-sha256(body || t || nonce))>
X-DockPulse-Timestamp: <unix>
X-DockPulse-Nonce: <opaque>
```

with a 5-minute clock skew window and a server-side nonce store to reject replays.

## Build pipeline

The Go module lives under `go/` so `go build` never traverses the npm `node_modules/` tree (some npm packages ship Go source for unrelated projects). The build pipeline:

1. `web/` — `npm ci && npm run build` produces `web/build/` (SvelteKit adapter-static).
2. `go/internal/web/embed.go` — `//go:embed all:build` references the directory at `go/internal/web/build`, which `make build` populates from `web/build` before invoking `go build`.
3. `go/cmd/dockpulse/main.go` — single entry point dispatches to `internal/controller` or `internal/agent`.

A multi-stage `Dockerfile` produces a distroless image containing only the static Go binary.

## Future: mobile companion

SvelteKit's adapter-static output is already PWA-friendly. The controller exposes a JSON-first API; no code change is needed to consume it from a Capacitor or React Native shell. The frontend routes can later be reused or replaced by a thin native shell without backend changes.