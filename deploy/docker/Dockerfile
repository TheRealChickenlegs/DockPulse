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

ARG GO_VERSION=1.22
ARG NODE_VERSION=20

# --- Stage 1: web bundle ---------------------------------------------------
FROM node:${NODE_VERSION}-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund
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
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
LABEL org.opencontainers.image.source="https://github.com/TheRealChickenlegs/DockPulse" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.vendor="TheRealChickenlegs" \
      org.opencontainers.image.title="DockPulse" \
      org.opencontainers.image.description="Multi-server Docker dashboard with update detection and changelog aggregation."

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/dockpulse /usr/local/bin/dockpulse

USER nonroot:nonroot
WORKDIR /data
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/dockpulse"]