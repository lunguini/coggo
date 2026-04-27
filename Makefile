.PHONY: help build build-gateway install install-gateway install-all \
	run dev serve serve-public test test-verbose fmt vet tidy clean version

# Port Coggo binds (must match server.listen_address in config.toml)
COGGO_PORT ?= 6177
# Port the OAuth gateway binds (Funnel exposes this for claude.ai)
GATEWAY_PORT ?= 8080

# Version stamp: git describe if available, else "dev"
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/lunguini/coggo/internal/cli.Version=$(VERSION)

# Where `go install` puts the binary
GOBIN := $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

help:
	@echo "Coggo — make targets:"
	@echo "  build              build ./coggo binary in repo root"
	@echo "  build-gateway      build ./coggo-oauth-gateway binary"
	@echo "  install            install coggo to $(GOBIN)"
	@echo "  install-gateway    install coggo-oauth-gateway to $(GOBIN)"
	@echo "  install-all        install both binaries"
	@echo "  run                run from source (e.g. make run ARGS='today')"
	@echo "  dev                build + serve locally (foreground, Ctrl-C to stop)"
	@echo "  serve              build + serve + expose via Tailscale Funnel (bearer-token auth)"
	@echo "  serve-public       build coggo + gateway, serve both, Funnel exposes gateway (OAuth)"
	@echo "  test               run all tests"
	@echo "  test-verbose       run all tests with -v"
	@echo "  fmt                gofmt the tree"
	@echo "  vet                go vet"
	@echo "  tidy               go mod tidy"
	@echo "  clean              remove build artifacts"
	@echo "  version            print the version stamp"

build:
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o coggo ./cmd/coggo
	@echo "built ./coggo ($(VERSION))"

build-gateway:
	CGO_ENABLED=0 go build -o coggo-oauth-gateway ./cmd/coggo-oauth-gateway
	@echo "built ./coggo-oauth-gateway"

install:
	CGO_ENABLED=1 go install -ldflags "$(LDFLAGS)" ./cmd/coggo
	@echo "installed coggo $(VERSION) to $(GOBIN)/coggo"
	@command -v coggo >/dev/null 2>&1 || echo "warning: $(GOBIN) is not on PATH — add it to use 'coggo' directly"

install-gateway:
	CGO_ENABLED=0 go install ./cmd/coggo-oauth-gateway
	@echo "installed coggo-oauth-gateway to $(GOBIN)/coggo-oauth-gateway"

install-all: install install-gateway

run:
	CGO_ENABLED=1 go run -ldflags "$(LDFLAGS)" ./cmd/coggo $(ARGS)

dev: build
	./coggo serve

# `make serve` exposes the local Coggo via Tailscale Funnel so claude.ai
# (or any public MCP client) can reach it. Funnel is reset on Ctrl-C.
# Requires: tailscale installed + logged in, Funnel enabled in your tailnet ACLs.
# Logic is in scripts/serve-with-funnel.sh so signals route cleanly when
# invoked through make.
#
# NOTE: claude.ai's custom-connector UI requires OAuth, not bearer tokens.
# For claude.ai access use `make serve-public` instead, which puts the OAuth
# gateway in front. `make serve` is for clients that accept bearer tokens
# directly (curl, scripts, Claude Code via Funnel).
serve: build
	@./scripts/serve-with-funnel.sh $(COGGO_PORT)

# `make serve-public` is the claude.ai path: runs coggo on localhost, runs
# coggo-oauth-gateway on $(GATEWAY_PORT), exposes the gateway via Tailscale
# Funnel, and shuts everything down cleanly on Ctrl-C.
#
# Required env (the script will fail with clear messages if any are missing):
#   COGGO_TOKEN              from `coggo token create --all`
#   GOOGLE_CLIENT_ID         from Google Cloud Console
#   GOOGLE_CLIENT_SECRET     from Google Cloud Console
serve-public: build build-gateway
	@./scripts/serve-public.sh $(COGGO_PORT) $(GATEWAY_PORT)

test:
	go test ./...

test-verbose:
	go test -v ./...

fmt:
	gofmt -s -w .

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -f coggo
	rm -f *.test *.out

version:
	@echo $(VERSION)
