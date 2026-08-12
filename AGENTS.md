# AGENTS.md — guidance for AI agents working on DockPulse

This file is read alongside `README.md` and `docs/ARCHITECTURE.md`.

## Project goals

DockPulse is a security-first, multi-server Docker dashboard. The controller is a single Go binary that embeds a SvelteKit SPA and serves both the web UI and the agent API. Agents run on each Docker host and report over mTLS.

Security is non-negotiable: threat model lives at `docs/THREAT_MODEL.md` and the security baseline at `docs/SECURITY.md`. Any change that introduces a new external surface or weakens an existing one must update those documents and reference the threat-model IDs.

## Stack

- **Backend:** Go 1.22+, `chi` router, `slog` for logging, distroless final image.
- **Frontend:** SvelteKit 2 + Svelte 5 (runes), `motion` for animations, `adapter-static` for the SPA bundle embedded via `go:embed`.
- **Database:** SQLite (WAL). Migrations live in `internal/controller/db/migrations/`.
- **Deployment:** Docker Compose, Caddy reverse proxy.

## Conventions

- **No comments unless necessary.** Code must be self-documenting; comments only when the *why* isn't obvious.
- **No secrets in logs.** Always redact credentials in `String()` methods and use the structured logger's field-level access controls.
- **All mutation endpoints require CSRF.** Implemented in Phase 1 via double-submit token + `Origin` allowlist.
- **Agents are outbound-only.** The controller must never require inbound ports on agent hosts.
- **TLS is mandatory for agent↔controller** (`https://` in `--controller`). Plain HTTP is rejected at config-parse time.
- **No inline scripts.** The SvelteKit SPA produces no inline scripts; preserve this property.
- **`prefers-reduced-motion`** must be honored by all animations.
- **CLI flags, not environment variables, for non-secret values.** Secrets come from files (`--enroll-token-file` pattern).

## Where things live

| Concern | Location |
| --- | --- |
| Mode dispatcher | `go/cmd/dockpulse/main.go` |
| Configuration | `go/internal/config/` |
| Logging | `go/internal/logging/` |
| Version + build info | `go/internal/version/` |
| Web bundle embed | `go/internal/web/embed.go` |
| Controller server | `go/internal/controller/` |
| Agent daemon | `go/internal/agent/` |
| Frontend | `web/src/` |
| Deployment manifests | `deploy/` |
| Threat model | `docs/THREAT_MODEL.md` |
| Public API contract | `docs/API.md` |

## Build pipeline

The Go module lives under `go/` (separate from `web/` and the npm `node_modules/`) so `go build` never walks third-party Go files that ship inside npm packages. The build embeds `web/build/` into the Go binary via `go:embed`:

1. `make web-build` — runs SvelteKit's `vite build` into `web/build/`.
2. `make build` — copies `web/build` into `go/internal/web/build` and runs `go build`.

The `Dockerfile` is multi-stage; it does the same steps inside the image so contributors do not need Node installed to produce a binary if they have a web bundle committed (we currently do not commit it; CI builds it).

## Verification

Before sending a PR:

- `go test ./...`
- `cd web && npm run check && npm run lint`
- `make build` succeeds
- `/healthz` returns `{"status":"ok"}` on the running controller
- No `// TODO` left in changed code without an associated GitHub issue reference

## Out of scope (today, by request)

- Auto-applying container updates without explicit user action (planned, opt-in, Phase 6).
- Mobile companion app (planned after web UI ships, see `docs/ARCHITECTURE.md`).
- Multi-controller HA (single controller only; SQLite file is easy to back up).