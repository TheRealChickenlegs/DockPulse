# syntax=docker/dockerfile:1.7
#
# DockPulse multi-stage build.
#
# Stage 1: build the SvelteKit static bundle with Node.
# Stage 2: copy the bundle into the Go module's internal/web/build and
#          produce a static binary via `go build`.
# Stage 3: copy the binary into a distroless final image.
#
# The resulting image contains a single static binary that runs in
# either controller or agent mode.

ARG GO_VERSION=1.25
ARG NODE_VERSION=20

# --- Stage 1: web bundle ---------------------------------------------------
#
# The repo uses npm workspaces: web/ is the only workspace and the
# lockfile lives at the repo root. We run `npm ci` from the root
# so the workspace-aware resolver picks up the right deps for web/.
FROM node:${NODE_VERSION}-alpine AS web
WORKDIR /src
COPY package.json package-lock.json ./
COPY web/package.json ./web/
RUN npm ci --no-audit --no-fund
WORKDIR /src/web
COPY web/ ./
RUN npm run build

# --- Stage 2: go build -----------------------------------------------------
FROM golang:${GO_VERSION}-alpine AS build
RUN apk add --no-cache ca-certificates git
WORKDIR /src

# Copy the Go module from ./go (separate from web/ and node_modules/ so
# `go` doesn't try to compile third-party packages hoisted into
# node_modules at the repo root).
COPY go/go.mod go/go.sum* ./go/
RUN cd go && go mod download

COPY go/cmd/        ./go/cmd/
COPY go/internal/   ./go/internal/

# The SvelteKit build output is copied to go/internal/web/build, which
# is referenced by //go:embed in go/internal/web/embed.go.
COPY --from=web /src/web/build ./go/internal/web/build

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
ENV CGO_ENABLED=0
RUN cd go && go build \
    -trimpath \
    -ldflags="-s -w -X github.com/TheRealChickenlegs/DockPulse/go/internal/version.Version=${VERSION} -X github.com/TheRealChickenlegs/DockPulse/go/internal/version.Commit=${COMMIT} -X github.com/TheRealChickenlegs/DockPulse/go/internal/version.BuildDate=${BUILD_DATE}" \
    -o /out/dockpulse \
    ./cmd/dockpulse

# --- Stage 3: distroless runtime --------------------------------------------
#
# Use the distroless :latest tag (which runs as root) so we can
# downgrade to UID 1000:1000 via a USER directive. Many homelab
# hosts have their user-data bind-mounted to /data, and the
# default UID on those hosts is 1000. Running as 1000:1000 means
# bind mounts work out of the box without `sudo chown` to a
# non-default UID.
#
# The distroless static image has no /etc/passwd entry for UID
# 1000. That's fine — Linux allows running as any UID even
# without a passwd entry; the only requirement is that the
# process can read/write the files it needs. The bind mount
# (and /data on named volumes) is created writable by UID 1000.
FROM gcr.io/distroless/static-debian12:latest AS runtime
LABEL org.opencontainers.image.source="https://github.com/TheRealChickenlegs/DockPulse" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.vendor="TheRealChickenlegs" \
      org.opencontainers.image.title="DockPulse" \
      org.opencontainers.image.description="Multi-server Docker dashboard with update detection and changelog aggregation."

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/dockpulse /usr/local/bin/dockpulse

USER 1000:1000
WORKDIR /data
VOLUME ["/data"]
EXPOSE 9787
ENTRYPOINT ["/usr/local/bin/dockpulse"]