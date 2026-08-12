# DockPulse — Public API surface (Phase 0 placeholder)

> This document tracks the **public, documented** HTTP surface of the DockPulse controller. Internal handlers, in-process channels, and Go-package APIs are not listed here. Anything in this file is a compatibility commitment: changes must be additive unless the version is bumped.

This file is hand-maintained until Phase 1, at which point it will be generated from the Go source via an OpenAPI generator.

## Controller

Base URL: `https://<your-host>`

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| GET | `/healthz` | none | Liveness probe. Returns `{"status":"ok"}`. Safe to expose to load balancers; does not reveal host state. |
| GET | `/version` | none | Returns `{"version","commit","go"}`. Safe to expose. |
| GET | `/` | none | SPA shell. Returns the embedded SvelteKit `index.html`. |
| GET | `/*` | none | SPA route fallback. |
| GET | `/static/*`, `/_app/*`, `/favicon.svg`, `/robots.txt` | none | Static assets from the embedded bundle. |

The following endpoints are planned but **not yet implemented**:

| Method | Path | Planned for |
| --- | --- | --- |
| POST | `/api/v1/login` | Phase 1 |
| POST | `/api/v1/logout` | Phase 1 |
| GET | `/api/v1/me` | Phase 1 |
| GET | `/api/v1/servers` | Phase 1 |
| GET | `/api/v1/servers/:id/containers` | Phase 1 |
| GET | `/api/v1/containers/:id` | Phase 1 |
| GET | `/api/v1/containers/:id/changelog` | Phase 2 |
| GET | `/api/v1/updates` | Phase 2 |
| POST | `/api/v1/updates/:id/{ignore,apply,unignore}` | Phase 6 (opt-in apply) |
| POST | `/agent/v1/enroll` | Phase 1 |
| POST | `/agent/v1/heartbeat` | Phase 1 |
| POST | `/agent/v1/containers/snapshot` | Phase 1 |
| POST | `/agent/v1/updates/report` | Phase 2 |
| POST | `/agent/v1/changelog/upload` | Phase 2 |

## Headers

Every response carries (set by Caddy; mirrored in the controller):

- `Strict-Transport-Security: max-age=63072000; includeSubDomains; preload`
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: no-referrer`
- `Content-Security-Policy: default-src 'self'; …`
- `Permissions-Policy: …`

Mutating endpoints require a `X-CSRF-Token` header whose token is the value of the `dockpulse_csrf` cookie.

## Rate limits

- `/api/v1/login`: 10 requests / minute / IP.
- `/api/*`: 120 requests / minute / IP.

## CORS

No CORS allowlist is configured in Phase 0. Phase 1 introduces an explicit allowlist with the admin UI as the only consumer.