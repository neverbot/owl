SHELL := /bin/bash
GO ?= go
PKG := github.com/neverbot/owl
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(PKG)/internal/version.Version=$(VERSION)
BIN := owl

.PHONY: all build test vet fmt tidy clean docs docs-serve docs-check js-check frontend-check frontend-format

all: build

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/owl

test:
	$(GO) test ./... -race -count=1

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

tidy:
	$(GO) mod tidy

clean:
	rm -f $(BIN) coverage.txt

docs:
	$(GO) run ./cmd/owl-docs --in docs/site/content --out docs/site/dist

docs-serve: docs
	@echo "Serving docs at http://localhost:8000/"
	@python3 -m http.server -d docs/site/dist 8000

docs-check:
	$(GO) run ./cmd/owl-docs --check --in docs/site/content

# Frontend syntax gate. owl ships vanilla JS without a build step, so
# a stray backtick or similar typo otherwise surfaces only at runtime
# in the browser. `node --check` parses every `.js` we own
# (excluding gitignored paths) without executing it, catching syntax
# errors during `make check`-style flows.
js-check:
	@command -v node >/dev/null 2>&1 || { echo "node is required for js-check (install Node 20+)"; exit 1; }
	@find internal/design cmd/owl-docs/static -name '*.js' -print0 \
		| xargs -0 -I{} node --check {}

# Frontend lint + format gate via Biome. The Rust binary is cached
# under `~/.npm/_npx/` after the first invocation, so no global
# install is required and the repo carries no node_modules. Use
# `frontend-format` locally to auto-fix; `frontend-check` is the
# read-only gate run by CI.
frontend-check:
	@command -v npx >/dev/null 2>&1 || { echo "npx is required for frontend-check (install Node 20+)"; exit 1; }
	npx --yes @biomejs/biome check internal/design cmd/owl-docs/static

frontend-format:
	@command -v npx >/dev/null 2>&1 || { echo "npx is required for frontend-format (install Node 20+)"; exit 1; }
	npx --yes @biomejs/biome check --write internal/design cmd/owl-docs/static
