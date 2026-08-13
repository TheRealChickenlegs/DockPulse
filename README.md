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

**Phase 1 — Core.** Local accounts, Argon2id + CSRF + session cookies, SQLite database, agent enrollment + mTLS, container inventory, and the SPA shell. See `docs/ARCHITECTURE.md` for the full design and `docs/SECURITY.md` for the threat model.

## Quick start (development)

```bash
# 1. Install Go 1.25+ and Node 20+
# 2. Install dependencies (npm workspaces + Go module)
npm ci
(cd go && go mod download)

# 3. Build the web bundle and the Go binary
make build

# 4. Run the controller (defaults: --listen=:9787, --db=./data/dockpulse.db)
#    Open http://localhost:9787 to set up the first admin.
./bin/dockpulse

# 5. Run an agent pointed at the local controller (LAN http is OK)
./bin/dockpulse --mode=agent --controller=http://192.168.10.10:9787 \
    --name=server-a --docker=tcp://socket-proxy:2375
```

The controller listens on all interfaces by default. From another machine on the same LAN, just browse to `http://<host-lan-ip>:9787`. For internet-facing deployment, put a reverse proxy in front and set `--secure-cookies` and `--trusted-proxies`. The agent requires `https://` for any non-local host unless you pass `--allow-insecure-controller`.

For end-to-end testing with TLS and the reverse proxy, see `deploy/README.md` and `docs/AGENT_SETUP.md`.

## Pulling the published image

The Docker image is built and pushed to GHCR on every push to `main` and signed with [cosign](https://github.com/sigstore/cosign) (keyless OIDC). Tags:

| Tag | Meaning |
| --- | --- |
| `latest` | The most recent main build. |
| `edge` | Alias for `latest` (familiar to users of edge-release channels). |
| `edge-<sha>` | Pinned to a specific main commit (short SHA). |
| `main-<sha>` | Same as `edge-<sha>`; both names point to the same digest. |
| `vX.Y.Z` | Released by `git tag vX.Y.Z && git push --tags`. Built by the `release.yml` workflow. |

```bash
docker pull ghcr.io/therealchickenlegs/dockpulse:latest
docker pull ghcr.io/therealchickenlegs/dockpulse:edge-79cb1c6
docker pull ghcr.io/therealchickenlegs/dockpulse:v0.3.0

# Verify the signature
cosign verify \
  --certificate-identity-regexp 'https://github.com/TheRealChickenlegs/DockPulse' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  ghcr.io/therealchickenlegs/dockpulse:latest
```

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