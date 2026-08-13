# Threat Model

> Living document. Treated as code: changes require a PR review and a short rationale in the commit message.
## Scope

This document covers DockPulse v1 — the controller, the agent, and the
web UI bundle — in two deployment contexts:

1. **Homelab / LAN-only** — controller reachable only from the local network.
2. **Externally exposed** — controller reachable from the public internet, typically through an operator-managed reverse proxy (nginx proxy manager, Traefik, Caddy, HAProxy, etc.).

The reverse proxy is **not** a DockPulse component. It is the operator's responsibility to run a hardened proxy, terminate TLS, and apply the header set documented in `deploy/README.md` and `SECURITY.md`. The controller sets a defensive baseline of non-CSP/non-HSTS headers itself so a misconfigured proxy cannot fully degrade the baseline.

It does **not** cover:

- The host operating systems of the agents or controller (assumed reasonably hardened).
- The reverse proxy itself beyond specifying headers it must set.
- Container breakout from a malicious workload running on an agent host. This is bounded by not mounting the Docker socket directly and by using the socket-proxy with `POST=0`.

## Assets

| Asset | Sensitivity | Where it lives |
| --- | --- | --- |
| Admin credentials | Critical | Controller SQLite (Argon2id hash) |
| Agent client certificates | High | Controller CA + each agent's filesystem |
| Registry credentials | High | Agent host (AES-GCM encrypted at rest) |
| Container inventory & digests | Medium | Controller SQLite |
| Changelog cache | Low | Controller SQLite |
| Audit log | Medium | Controller SQLite |
| UI session tokens | High | Server-side session table + `httpOnly` cookie |

## Adversaries

| Adversary | Capability | Motivation |
| --- | --- | --- |
| Unauthenticated internet attacker | Network access to controller public endpoint | Recon, credential theft, defacement, ransomware |
| Authenticated low-privilege user | Valid login, no admin role | Privilege escalation, data exfiltration |
| Compromised container on an agent host | Root-equivalent inside that container | Pivot to the agent, then to the controller |
| Network attacker on the LAN | MITM, packet capture | Tamper with controller↔agent traffic |
| Insider / malicious maintainer | Source commit access | Supply-chain backdoor |
| Replay attacker | Captured signed payload | Re-trigger an action |

## Threats and mitigations

### T1 — Credential brute force on the login endpoint

**Mitigation:** Caddy rate limit (10 req/min/IP); Argon2id (≥ 64 MiB, ≥ 3 iters); account lockout after 10 failures within 10 minutes; audit-logged.

### T3 — CSRF on mutating endpoints

**Mitigation:** Double-submit CSRF token, `SameSite=Strict` cookies, `Origin` allowlist, custom request header required for non-form JSON requests.

### T5 — Session theft via XSS

**Mitigation:** Strict CSP (`default-src 'self'`, no inline), `httpOnly` cookies, `Trusted Types` once supported broadly, output sanitization on user-provided changelog URLs (markdown rendered with `bluemonday`).

### T6 — MITM on agent↔controller

**Mitigation:** mTLS with controller-issued client certs (1-year, renewable); controller cert fingerprint pinned at enrollment (TOFU); HMAC-signed payloads with 5-minute window and server-side nonce store.

### T7 — Replay of agent payloads

**Mitigation:** Timestamp window + nonce store; payloads older than 5 minutes are dropped; each nonce is single-use.

### T8 — Container breakout to the agent

**Mitigation:** Socket-proxy sidecar with `CONTAINERS=1, IMAGES=1, POST=0`, listening on `127.0.0.1` only; the agent container joins a restricted network; no other containers can reach the proxy.

### T9 — Supply-chain compromise

**Mitigation:** SLSA provenance via GitHub's `attest-build-provenance`, pinned base images (distroless), Dependabot, CodeQL, Trivy, `govulncheck`. Multi-party review required for changes to the agent API and the enrollment flow.

### T10 — Privilege escalation from agent to controller

**Mitigation:** Agents have only `POST` access to a small surface (`/heartbeat`, `/containers/snapshot`, `/updates/report`, `/changelog/upload`, `/cert/renew`, `/commands/ack`, `/commands/poll`); no path can mutate users, sessions, notifiers, or audit log; admin endpoints check session role.

### T11 — Replay of one-click update

**Mitigation:** Apply actions require an active admin session AND a fresh CSRF token AND a per-action nonce; agent confirms receipt via `/commands/ack` and the controller records the audit log entry only after ack.

### T12 — Information disclosure via `/healthz`

**Mitigation:** `/healthz` returns `{ "status": "ok", "version": "..." }` only — no host, container, or agent information. Detailed health is on a separate admin-only endpoint.

### T13 — Denial of service

**Mitigation:** Connection limits and rate limits on the operator's reverse proxy (the bundled `Caddyfile` shows the Caddy set; replicate the same in your proxy), controller `http.Server` read/write timeouts, SQLite WAL with `busy_timeout`, registry polling is rate-limited and distributed across agents (each host polls its own images, not a centralized thundering herd).

### T14 — Spoofed source IP via `X-Forwarded-For`

**Mitigation:** The controller ignores `X-Forwarded-For` by default. The middleware only honours the header when the connection's remote address is in the operator-supplied `--trusted-proxies` list (CIDR or single IP). An attacker cannot spoof their source IP unless they are the trusted proxy itself, in which case the attack is on the proxy, not DockPulse.

## Residual risk

- **T2 (credential stuffing using breached passwords)** — partially mitigated by Argon2id. Mitigation if externally exposed: require OIDC and disable local login.
- **T4 (UI supply-chain attack via compromised npm package)** — partially mitigated by CodeQL + Trivy + lockfile review; consider Subresource Integrity for any third-party script (none used in v1).
- **Compromise of the controller host OS** — out of scope but the SQLite file should be backed up off-host and the host should be patched regularly.

## Review cadence

This document is reviewed on every release and when a new feature ships a new external surface (e.g., mobile companion, webhook receiver).