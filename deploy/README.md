# DockPulse

This directory contains the production deployment manifests for DockPulse.

## Layout

| File | Purpose |
| --- | --- |
| `docker-compose.yml` | Full controller stack (Caddy + DockPulse). |
| `docker-compose.agent.yml` | Drop-in agent stack for each Docker host. |
| `Caddyfile` | Reverse proxy configuration with security headers and rate limiting. |
| `docker/Dockerfile` | Multi-stage build for the DockPulse binary. |

## Quick start (controller)

```bash
cp .env.example .env
# Edit .env to set DOCKPULSE_DOMAIN and a strong DOCKPULSE_CA_PASS

docker compose -f deploy/docker-compose.yml up -d
docker compose -f deploy/docker-compose.yml logs -f
```

Visit `https://$DOCKPULSE_DOMAIN` — you should see the placeholder DockPulse UI.

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

Both compose files are runnable but functional behavior is limited to:

- Controller: `/healthz`, `/version`, and the static placeholder UI.
- Agent: configuration validation, data directory creation, and a placeholder heartbeat loop.

Subsequent phases activate enrollment, the database, registry polling, and changelog aggregation. See the top-level [`README.md`](../README.md) and [`docs/ARCHITECTURE.md`](../docs/ARCHITECTURE.md) for the full roadmap.