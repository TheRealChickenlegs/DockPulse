# Registries

> Pluggable registry providers. All providers expose a minimal interface; new providers can be added without controller-side changes.

## Supported

| Kind | Phase | Status | Auth | Notes |
| --- | --- | --- | --- | --- |
| `hub` (Docker Hub) | 1 | **implemented** | Optional | Uses `/v2/<repo>/manifests/<tag>` with anonymous bearer-token auth. |
| `ghcr` | 2 | planned | Optional | Uses the same `/v2/` flow; tokens from `ghcr.io/token`. |
| `quay` | 2 | planned | Optional | Uses the Quay v2 REST API for digest resolution. |
| `gcr` | 2 | planned | Optional | Uses Google Cloud's `/v2/` flow with anonymous or service-account auth. |
| `ecr` | 2 | planned | Required | Uses GetAuthorizationToken + AWS Sigv4. |
| `gitlab` | 2 | planned | Optional | Uses GitLab Container Registry's `/v2/` flow. |

## Changelog sources

Each image may have multiple candidate changelog sources, tried in order:

1. **OCI image labels** (`org.opencontainers.image.source`, `org.opencontainers.image.url`). **implemented** for `github.com/...` repos.
2. **GitHub Releases API** for `github.com/...` repos. **implemented** — fetch host is hardcoded to `api.github.com` (SSRF-bounded, see THREAT_MODEL T15).
3. **GitLab Releases API** for `gitlab.com/...` repos. planned.
4. **Generic scrape** (Atom/RSS at common paths, configurable regex) — opt-in. planned.
5. **Manual override** — admins can pin a changelog URL per image. planned.

Deduplication is by `(image_id, version, hash)` so the same release note is never stored twice even if multiple sources report it.