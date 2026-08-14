# DockPulse — Public API surface

> This document tracks the **public, documented** HTTP surface of the DockPulse controller. Internal handlers, in-process channels, and Go-package APIs are not listed here. Anything in this file is a compatibility commitment: changes must be additive unless the version is bumped.

This file is hand-maintained until an OpenAPI generator is introduced.

## Controller

Base URL: `https://<your-host>`

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| GET | `/healthz` | none | Liveness probe. Returns `{"status":"ok"}`. Safe to expose to load balancers; does not reveal host state. |
| GET | `/version` | none | Returns `{"version","commit","go"}`. Safe to expose. |
| GET | `/` | none | SPA shell. Returns the embedded SvelteKit `index.html`. |
| GET | `/*` | none | SPA route fallback. |
| GET | `/static/*`, `/_app/*`, `/favicon.svg`, `/robots.txt` | none | Static assets from the embedded bundle. |
| POST | `/api/v1/login` | none | Session login with username + password. Sets `dockpulse_session` and `dockpulse_csrf` cookies. |
| POST | `/api/v1/logout` | session | Invalidates the session. |
| GET | `/api/v1/me` | session | Returns the current user and session state. |
| GET | `/api/v1/servers` | session | Lists enrolled servers with heartbeat/agent info. |
| GET | `/api/v1/servers/:id/containers` | session | Container inventory for one server. |
| GET | `/api/v1/containers/:id` | session | Single container detail. |
| GET | `/api/v1/containers/:id/changelog` | session | Changelog entries for the container's image. Returns `{"image_ref","entries"}`. |
| GET | `/api/v1/updates` | session | Detected updates with per-image changelog and affected-container counts. |
| POST | `/api/v1/admin/agents/enroll-token` | session (admin) | Creates a short-lived enrollment token. |
| POST | `/agent/v1/enroll` | token + mTLS | One-time enrollment; exchanges the token for a client cert. |
| POST | `/agent/v1/heartbeat` | mTLS + signed | Agent liveness + Docker info. |
| POST | `/agent/v1/containers/snapshot` | mTLS + signed | Container state batch. |
| POST | `/agent/v1/updates/report` | mTLS + signed | Detected `(image, local digest, remote digest)` deltas. |
| POST | `/agent/v1/changelog/upload` | mTLS + signed | Changelog entries for an image (release notes and/or registry tag history). |

The following endpoints are planned but **not yet implemented**:

| Method | Path | Planned for |
| --- | --- | --- |
| POST | `/api/v1/updates/:id/{ignore,apply,unignore}` | Phase 6 (opt-in apply) |

## Headers

Every response carries a defensive baseline set by the controller:

- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: no-referrer`
- `Permissions-Policy: …`

The operator's reverse proxy is responsible for the remaining hardening headers (TLS termination, HSTS, CSP, and rate limits). The bundled optional `Caddyfile` shows the full set Caddy applies. See `deploy/README.md` for the required header list if you use a different proxy.

Mutating endpoints require a `X-CSRF-Token` header whose token is the value of the `dockpulse_csrf` cookie.

## Rate limits

- `/api/v1/login`: 10 requests / minute / IP.
- `/api/*`: 120 requests / minute / IP.

These limits are documented as a baseline. In production they are enforced by the operator's reverse proxy.

## CORS

No CORS allowlist is configured in Phase 0. Phase 1 introduces an explicit allowlist with the admin UI as the only consumer.