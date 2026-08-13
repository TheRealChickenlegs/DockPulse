# Registries

> Pluggable registry providers. All providers expose a minimal interface; new providers can be added without controller-side changes.

## Supported

| Kind | Phase | Status | Auth | Notes |
| --- | --- | --- | --- | --- |
| `hub` (Docker Hub) | 1 | **implemented** | Optional PAT | Uses `/v2/<repo>/manifests/<tag>` with bearer-token auth. Anonymous by default; a personal access token (PAT) lifts Docker Hub's unauthenticated pull rate limit. |
| `ghcr` | 2 | planned | Optional | Uses the same `/v2/` flow; tokens from `ghcr.io/token`. |
| `quay` | 2 | planned | Optional | Uses the Quay v2 REST API for digest resolution. |
| `gcr` | 2 | planned | Optional | Uses Google Cloud's `/v2/` flow with anonymous or service-account auth. |
| `ecr` | 2 | planned | Required | Uses GetAuthorizationToken + AWS Sigv4. |
| `gitlab` | 2 | planned | Optional | Uses GitLab Container Registry's `/v2/` flow. |

## Authenticated pulls (Docker Hub)

Docker Hub rate-limits anonymous pulls by source IP. To raise the
limit, give the agent credentials for that registry:

1. Create a PAT at <https://hub.docker.com/settings/security>
   (read-only scope is enough for digest checks).
2. On the agent host, create a credentials directory with one file
   per registry host, each holding one line `username:token`:
   `mkdir -p ./agent-data/registry-creds`
   `printf 'chickenlegs:<PAT>' > ./agent-data/registry-creds/docker.io`
   then `chmod 700` the directory and `chmod 600` the file.
3. Start the agent with `--registry-credentials-dir=/data/registry-creds`
   (the bundled compose wires `REGISTRY_CREDS_DIR` for this).

Other registries follow the same shape as provider support lands:
add a file named after the registry host (e.g. `ghcr.io`) and the
agent looks it up per image. Docker Hub's hostnames (`docker.io`,
`index.docker.io`, `registry-1.docker.io`, and the implicit default)
all map to the single `docker.io` key. Hostnames for which no
credential exists fall back to anonymous pulls.

Credentials only authenticate the token exchange for their own
registry host; they are never sent to another registry, logged, or
transmitted to the controller. The directory is read once at agent
startup; changing it requires an agent restart.

## Polling cadence

`--registry-poll` controls how often the agent re-checks registries
(default `24h`; `0` disables periodic polling). The controller's
"Scan now" button triggers an immediate poll on the target agent, so
24h is a safe default even when a container is updated mid-day.

## Changelog sources

Each image may have multiple candidate changelog sources, tried in order:

1. **OCI image labels** (`org.opencontainers.image.source`, `org.opencontainers.image.url`). **implemented** for `github.com/...` repos.
2. **GitHub Releases API** for `github.com/...` repos. **implemented** — fetch host is hardcoded to `api.github.com` (SSRF-bounded, see THREAT_MODEL T15).
3. **GitLab Releases API** for `gitlab.com/...` repos. planned.
4. **Generic scrape** (Atom/RSS at common paths, configurable regex) — opt-in. planned.
5. **Manual override** — admins can pin a changelog URL per image. planned.

Deduplication is by `(image_id, version, hash)` so the same release note is never stored twice even if multiple sources report it.