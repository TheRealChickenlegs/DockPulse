# DockPulse Makefile
#
# Most targets assume GNU make. On systems without make, the underlying
# commands are listed in the comments so they can be run directly.

BINARY  := bin/dockpulse
VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

GO       ?= go
NPM      ?= npm
GOLANGCI ?= golangci-lint

# Paths relative to the repository root.
GO_MODULE      := go
WEB_DIR        := web
WEB_BUILD      := $(WEB_DIR)/build
EMBED_BUILD    := $(GO_MODULE)/internal/web/build

# Use a Go module path that matches the on-disk location so internal
# import paths stay stable as the project grows.
GOMODPATH := github.com/TheRealChickenlegs/DockPulse/go

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Targets:\n"} /^[a-zA-Z_-]+:.*##/ { printf "  %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: web-deps
web-deps: ## Install SvelteKit dependencies
	cd $(WEB_DIR) && $(NPM) ci --no-audit --no-fund

.PHONY: web-dev
web-dev: ## Run the SvelteKit dev server (proxies /api to the Go controller)
	cd $(WEB_DIR) && $(NPM) run dev

.PHONY: web-build
web-build: ## Build the SvelteKit static bundle into web/build
	cd $(WEB_DIR) && $(NPM) ci --no-audit --no-fund
	cd $(WEB_DIR) && $(NPM) run build

.PHONY: go-fmt
go-fmt: ## Run gofmt over the codebase
	cd $(GO_MODULE) && $(GO) fmt ./...

.PHONY: go-vet
go-vet: ## Run go vet
	cd $(GO_MODULE) && $(GO) vet ./...

.PHONY: go-test
go-test: ## Run Go tests
	cd $(GO_MODULE) && $(GO) test -count=1 ./...

.PHONY: go-test-race
go-test-race: ## Run Go tests with the race detector (requires CGO)
	cd $(GO_MODULE) && $(GO) test -race -count=1 ./...

.PHONY: web-check
web-check: ## Run svelte-check on the SvelteKit project
	cd $(WEB_DIR) && $(NPM) run check

.PHONY: web-lint
web-lint: ## Lint the SvelteKit project
	cd $(WEB_DIR) && $(NPM) run lint

.PHONY: lint
lint: go-fmt go-vet go-test web-check web-lint ## Run all lint and test targets

.PHONY: tidy
tidy: ## Tidy Go modules
	cd $(GO_MODULE) && $(GO) mod tidy

.PHONY: build
build: web-build ## Build the DockPulse binary with the embedded web bundle
	@mkdir -p $(GO_MODULE)/bin
	rm -rf $(EMBED_BUILD)
	cp -R $(WEB_BUILD) $(EMBED_BUILD)
	cd $(GO_MODULE) && CGO_ENABLED=0 $(GO) build \
	    -trimpath \
	    -ldflags="-s -w -X $(GOMODPATH)/internal/version.Version=$(VERSION) -X $(GOMODPATH)/internal/version.Commit=$(COMMIT) -X $(GOMODPATH)/internal/version.BuildDate=$(DATE)" \
	    -o bin/$(notdir $(BINARY)) ./cmd/dockpulse
	@mkdir -p bin
	mv $(GO_MODULE)/bin/dockpulse bin/dockpulse

.PHONY: run-controller
run-controller: build ## Build and run in controller mode
	./bin/dockpulse --mode=controller --db=./data/dev.db

.PHONY: run-agent
run-agent: build ## Build and run in agent mode (requires --controller=...)
	./bin/dockpulse --mode=agent --controller=https://localhost:8443 --name=local-test

.PHONY: docker-build
docker-build: ## Build the Docker image locally
	docker buildx build -f deploy/docker/Dockerfile --load -t dockpulse:dev .

.PHONY: docker-compose-up
docker-compose-up: ## Bring up the controller stack via docker compose
	DOCKPULSE_IMAGE_TAG=dev docker compose -f deploy/docker-compose.yml up -d

.PHONY: docker-compose-down
docker-compose-down: ## Tear down the controller stack
	docker compose -f deploy/docker-compose.yml down

.PHONY: clean
clean: ## Remove build artefacts
	rm -rf bin $(EMBED_BUILD) $(WEB_BUILD) $(WEB_DIR)/.svelte-kit $(WEB_DIR)/node_modules data