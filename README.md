# DockPulse

> Multi-server Docker dashboard with update detection, changelog aggregation, and outbound notifications.

DockPulse gives a homelabber (or anyone running a small fleet of Docker hosts) a single pane of glass for:

- **Container inventory** across all registered hosts.
- **Update detection** by polling the configured registry for each running image.
- **Changelog aggregation** for every detected update (GitHub/GitLab releases, OCI labels, manual overrides).
- **Notifications** via Discord, Slack, Ntfy, Email, and Telegram.
- **Optional one-click update apply** per image (off by default — read-only by design).

A single Go binary runs in one of two modes:

| Mode | Purpose |
| --- | --- |
| `controller` | Hosts the web UI, the SQLite database, the agent-facing API, and notification dispatch. |
| `agent` | Runs on each Docker host. Reads local Docker, polls registries, fetches changelogs, reports to the controller. |

The web UI is a SvelteKit application built to a static bundle and embedded in the controller binary via `go:embed`. There is one Docker image; the mode is selected at runtime.

## Status

**Phase 0 — Skeleton.** Repo scaffold, mode-aware binary, `/healthz` endpoint, embedded SvelteKit placeholder UI, multi-stage Dockerfile, and CI. See `docs/ARCHITECTURE.md` for the full design and `docs/SECURITY.md` for the threat model.

## Quick start (development)

```bash
# 1. Install Go 1.22+ and Node 20+
# 2. Install dependencies (npm workspaces + Go module)
npm ci
(cd go && go mod download)

# 3. Build the web bundle and the Go binary
make build

# 4. Run the controller (defaults to --mode=controller)
./bin/dockpulse --db=./data/dev.db

# 5. Run an agent pointed at it (separate terminal)
./bin/dockpulse --mode=agent --controller=https://localhost:8443 \
    --name=local-test --docker=unix:///var/run/docker.sock
```

For end-to-end testing with TLS and the reverse proxy, see `deploy/README.md` and `docs/AGENT_SETUP.md`.

## Architecture

See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## Security

See [`docs/SECURITY.md`](docs/SECURITY.md) and [`docs/THREAT_MODEL.md`](docs/THREAT_MODEL.md).

To report a vulnerability, see [`SECURITY.md`](SECURITY.md).

## License

[MIT](LICENSE).

## Acknowledgements

- [Tecnativa/docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy) — read-only Docker socket exposure pattern used by the agent.
- [Caddy](https://caddyserver.com/) — reverse proxy with automatic HTTPS used by the controller compose stack.
- [SvelteKit](https://kit.svelte.dev/) and [Motion One](https://motion.dev/) — frontend.