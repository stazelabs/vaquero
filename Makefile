GO          ?= go
BIN         := bin/vaquero
PKG         := github.com/stazelabs/vaquero
# VERSION is the newest v* tag without its "v", else 0.0.0-dev.
VERSION     := $(or $(patsubst v%,%,$(shell git describe --tags --match 'v*' 2>/dev/null)),0.0.0-dev)
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
BUILD_DATE  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w -X $(PKG)/internal/version.Version=$(VERSION) -X $(PKG)/internal/version.Commit=$(COMMIT) -X $(PKG)/internal/version.BuildDate=$(BUILD_DATE)
TAILWIND    := ./node_modules/.bin/tailwindcss
CSS_IN      := internal/web/static/css/input.css
CSS_OUT     := internal/web/static/css/tailwind.css
STATICCHECK_VERSION := 2026.2.1
# LINT_TOOLCHAIN is the Go staticcheck runs under. staticcheck 2026.2.1 can't
# read the export data newer Go writes (rechecked under go1.27.3 on
# 2026-11-17: "export data version 5"), so the pin stays until it can.
LINT_TOOLCHAIN      := go1.27.1
TEMPL_VERSION       := v0.3.1020
SQLC_VERSION        := v1.31.1

.DEFAULT_GOAL := help

.PHONY: help tools gen templ sqlc css dev build test cover cover-check vet lint modernize-check modernize fmt migration db-reset ci clean

help: ## Show this list (the default)
	@echo "usage: make <target>"
	@echo
	@awk 'BEGIN { FS = ":.*## " } /^[a-z][a-z-]*:.*## / { printf "  %-16s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

tools: ## Install templ, sqlc, goose, air, staticcheck, and npm packages (one time)
	$(GO) install github.com/a-h/templ/cmd/templ@$(TEMPL_VERSION)
	$(GO) install github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)
	$(GO) install github.com/pressly/goose/v3/cmd/goose@latest
	$(GO) install github.com/air-verse/air@latest
	$(GO) install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
	npm install

templ: ## Generate Go from .templ files
	@out="$$(templ generate 2>&1)"; rc=$$?; printf '%s\n' "$$out"; \
	if [ $$rc -ne 0 ] || printf '%s\n' "$$out" | grep -qE 'parsing error|Command failed'; then \
		echo "ERROR: templ generate failed; generated code was NOT updated." >&2; exit 1; fi

sqlc: ## Generate Go from SQL queries
	cd internal/db && sqlc generate

css: ## Build the Tailwind stylesheet
	$(TAILWIND) -i $(CSS_IN) -o $(CSS_OUT) --minify

gen: templ sqlc css ## Run every generator (templ, sqlc, css); generated files are committed

dev: ## Run the server with hot reload on 127.0.0.1:7878 (logs to tmp/dev.log)
	@lsof -ti tcp:7878 | xargs kill 2>/dev/null || true
	@mkdir -p tmp
	air 2>&1 | tee tmp/dev.log

build: gen ## Generate, then build bin/vaquero for this machine
	@mkdir -p bin
	CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/vaquero

test: ## Run all tests
	CGO_ENABLED=0 $(GO) test ./...

cover: ## Run tests and list coverage by function
	CGO_ENABLED=0 $(GO) test -coverprofile=coverage.out ./... >/dev/null && $(GO) tool cover -func=coverage.out | sort -k3 -n

cover-check: ## Fail if a package is under its floor in coverage-floors.txt
	./scripts/cover-check.sh

vet: ## Run go vet
	$(GO) vet ./...

modernize-check: ## Fail if go fix would change anything
	@out="$$($(GO) fix -diff ./... 2>/dev/null)" || true; \
	if [ -n "$$out" ]; then printf '%s\n' "$$out"; echo "modernize-check: run 'make modernize' and commit" >&2; exit 1; fi

modernize: ## Apply go fix modernizations
	$(GO) fix ./...

lint: modernize-check ## Run modernize-check and staticcheck (under LINT_TOOLCHAIN)
	GOTOOLCHAIN=$(LINT_TOOLCHAIN) staticcheck ./...

fmt: ## Format all Go code
	gofmt -w $$(find . -name '*.go' -not -path './node_modules/*' -not -path './.claude/*')

migration: ## Create a migration: make migration name=add_thing
	@test -n "$(name)" || (echo "usage: make migration name=add_thing" && exit 1)
	goose -dir internal/db/migrations create $(name) sql

db-reset: ## Delete the dev data directory (./data)
	rm -rf data

ci: gen fmt vet lint test cover-check ## The full local gate: gen, fmt, vet, lint, test, cover-check

clean: ## Remove build output
	rm -rf bin tmp coverage.out
