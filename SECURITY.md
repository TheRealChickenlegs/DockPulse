# Security Policy

## Supported versions

DockPulse is in active development. Security patches will be applied to the most recent release and the `main` branch. Earlier releases will not receive backported fixes.

| Version | Supported |
| --- | --- |
| `main` | yes |
| latest tagged release | yes |
| older releases | no |

## Reporting a vulnerability

**Please do not file a public issue for security bugs.**

Send a private report to one of the maintainers listed at https://github.com/TheRealChickenlegs. Use GitHub's [private vulnerability disclosure](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability) workflow against this repository if you prefer a tracked channel.

We will acknowledge receipt within 72 hours and aim to triage within 7 days. Coordinated disclosure timelines will be agreed with the reporter before any public advisory.

## Security baseline

DockPulse is designed with the assumption that the controller may be exposed to the public internet. The following are baseline properties; deviations are bugs.

- **Authentication is required by default.** No endpoint — including health checks — reveals host or container information to an unauthenticated caller.
- **Passwords** are stored using Argon2id (memory ≥ 64 MiB, time ≥ 3, threads ≥ 2).
- **Sessions** are server-side, opaque tokens, `httpOnly`, `Secure`, `SameSite=Strict`.
- **CSRF** protection on every mutating endpoint via double-submit tokens.
- **CSP** is `default-src 'self'`; no inline scripts. The SvelteKit adapter-static output produces no inline scripts by default.
- **HSTS**, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, and a strict `Permissions-Policy` are the **operator's reverse proxy's** responsibility. The controller sets a defensive baseline of the non-CSP/non-HSTS headers itself so a misconfigured proxy cannot degrade them, but TLS, HSTS, CSP, and rate limits must be set by the proxy. An optional `deploy/docker-compose.with-caddy.yml` bundles Caddy with the full set for users who don't have their own reverse proxy.
- **`X-Forwarded-For` trust** is opt-in: the controller ignores the header by default. Operators who front the controller with a reverse proxy must set `--trusted-proxies` (or `DOCKPULSE_TRUSTED_PROXIES`) to the proxy's IP/CIDR so audit logs and rate limits see the real client IP.
- **Agents** authenticate to the controller with mTLS using certificates issued by the controller's internal CA on first enrollment. The controller's certificate fingerprint is pinned at enrollment.
- **All agent payloads** are HMAC-signed with per-agent secrets; replays within a 5-minute window are rejected via nonce store.
- **Registry credentials** are stored encrypted on the agent host and never transmitted to the controller.
- **The Docker socket** is never mounted directly into any container. Agents access Docker through `tecnativa/docker-socket-proxy` with `CONTAINERS=1, IMAGES=1, POST=0`, bound to `127.0.0.1`.
- **Supply chain:** each build records SLSA provenance via GitHub's `attest-build-provenance`, and an SBOM is attached to each release. CI runs CodeQL, Trivy, and `govulncheck` on every PR.

## Hardening checklist for operators

- [ ] Put DockPulse behind your own reverse proxy (nginx proxy manager, Traefik, Caddy, HAProxy) and ensure the proxy terminates TLS, sets HSTS, CSP, and rate limits. See `deploy/README.md` for the header set.
- [ ] Set `DOCKPULSE_TRUSTED_PROXIES` in `deploy/.env` to the IP or CIDR of your reverse proxy so client IPs are recorded accurately.
- [ ] Do not publish the controller's port 9787 to `0.0.0.0`; only join the Docker network the reverse proxy is on.
- [ ] If you use the optional bundled Caddy stack, restrict the Caddy admin API to `localhost` (the bundled `Caddyfile` already does this).
- [ ] Generate a strong enrollment token for each new agent and rotate the controller CA passphrase periodically.
- [ ] Enable OIDC if exposing DockPulse to the public internet; disable local account creation after the first admin exists.
- [ ] Back up the SQLite file daily; test restores.
- [ ] Subscribe to GitHub security advisories for this repository.