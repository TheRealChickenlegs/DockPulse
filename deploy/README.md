# DockPulse

This directory contains the production deployment manifests for DockPulse.

## Layout

| File | Purpose |
| --- | --- |
| `docker-compose.yml` | **Default** controller stack — no bundled reverse proxy. Use this with your existing reverse proxy. |
| `docker-compose.with-caddy.yml` | Optional controller + Caddy stack for users who don't already run a reverse proxy. |
| `docker-compose.agent.yml` | Drop-in agent stack for each Docker host. |
| `Caddyfile` | Used by the optional bundled Caddy stack. Mirrors the headers your own reverse proxy must set. |
| `docker/Dockerfile` | Multi-stage build for the DockPulse binary. |
| `.env.example` | Environment variables for the default (no-bundled-proxy) stack. |
| `.env.example.with-caddy` | Extra variables for the optional Caddy stack. |

## Recommended: use your own reverse proxy

DockPulse is designed to sit **behind a reverse proxy you already operate** (nginx proxy manager, Traefik, Caddy, HAProxy, etc.). The default `docker-compose.yml` does not include a reverse proxy — it attaches the controller only to a shared Docker network so your existing proxy can reach it.

```bash
# 1. Configure environment
cp deploy/.env.example deploy/.env
# Edit deploy/.env: set DOCKPULSE_PROXY_NETWORK to the network your
# reverse proxy already uses, and DOCKPULSE_TRUSTED_PROXIES to the
# address of your reverse proxy.

# 2. Bring up the controller
docker compose -f deploy/docker-compose.yml up -d

# 3. In nginx proxy manager (or equivalent):
#    - Add a new proxy host for your DockPulse domain
#    - Scheme: http
#    - Forward hostname: controller
#    - Forward port: 9787
#    - Enable Websockets
#    - Apply the security headers from the next section

# 4. Verify
docker compose -f deploy/docker-compose.yml logs -f controller
```

### Required headers on the reverse proxy

The controller sets a defensive baseline of security headers itself, but a properly configured reverse proxy is the **primary** enforcement point. The bundled Caddyfile shows the full set Caddy applies; if you use another proxy, replicate at least:

```
Strict-Transport-Security: max-age=63072000; includeSubDomains; preload
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Referrer-Policy: no-referrer
Permissions-Policy: accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()
Content-Security-Policy: default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'
```

Rate limits to consider on the proxy:

- `/api/v1/login`: 10 requests / minute / IP (Phase 1).
- `/api/*`: 120 requests / minute / IP (Phase 1).

### Sharing the Docker network

The default `docker-compose.yml` expects to attach the controller to a network your reverse proxy can already see. Two common setups:

**nginx proxy manager on the same Docker host** — let the controller join NPM's network:

```bash
# From the npm project directory, give the network a stable name
docker network create --driver bridge npm-net   # if you haven't already

# Then edit deploy/.env:
DOCKPULSE_PROXY_NETWORK=npm-net

# Bring up the controller
docker compose -f deploy/docker-compose.yml up -d

# In NPM, the proxy host's "Forward Hostname" is `controller` and
# "Forward Port" is `9787`.
```

**nginx proxy manager on a different host** — publish the controller port on the host loopback only (uncomment the `ports:` block in `docker-compose.yml`) and point NPM at `host-ip:9787`. Never publish 9787 on `0.0.0.0`.

## Optional: bundled Caddy

If you don't have a reverse proxy already, the alternative `docker-compose.with-caddy.yml` bundles Caddy with automatic HTTPS.

```bash
cp deploy/.env.example.with-caddy deploy/.env
# Edit deploy/.env: set DOCKPULSE_DOMAIN and DOCKPULSE_ACME_EMAIL

# Ensure DNS for $DOCKPULSE_DOMAIN points at this host and
# ports 80/443 are reachable from the internet.

docker compose -f deploy/docker-compose.with-caddy.yml up -d
```

This stack is the only place the `Caddyfile` is used; you can leave it alone or customize the security headers, rate limits, and ACME settings there.

## Quick start (agent)

```bash
# On the controller host: generate an enrollment token (Phase 1+).
# On the agent host:
mkdir -p ./agent-data
printf '<TOKEN>' > ./agent-data/enroll.token
chmod 600 ./agent-data/enroll.token
docker compose -f deploy/docker-compose.agent.yml up -d
```

## Phase 0 status

The compose files are runnable but functional behavior is limited to:

- Controller: `/healthz`, `/version`, and the static placeholder UI.
- Agent: configuration validation, data directory creation, and a placeholder heartbeat loop.

Subsequent phases activate enrollment, the database, registry polling, and changelog aggregation. See the top-level [`README.md`](../README.md) and [`docs/ARCHITECTURE.md`](../docs/ARCHITECTURE.md) for the full roadmap.