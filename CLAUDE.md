# CLAUDE.md

Guidance for working in this repo. Keep it short and current: edit it when an invariant or a product decision changes. Feature detail goes in the package's doc comment, not here.

## Summary

Vaquero is an agent-native, local-first, lightweight ticket tracker with coordination primitives for agents. One Go binary, one SQLite file. Local mode (one person, loopback) now; hosted mode (sign-in, cloud) from Phase 4. Design: docs/superpowers/specs/2026-11-18-vaquero-design.md. Plans: docs/superpowers/plans/2026-11-18-phase-0-foundation.md. Infrastructure is ported from the sibling projects startcase and healthcheck-email (read-only); the porting reference is /Users/cstaszak/Stazelabs/projects/startcase/docs/superpowers/plans/2026-11-17-porting-map.md.

## Commands

    make tools     # one-time
    make gen       # templ + sqlc + tailwind (generated files are committed)
    make dev       # air hot reload on 127.0.0.1:7878
    make test

Run `make` with no target to list every target; give each new target a `## description`.

Gate before every commit that touches Go, SQL, templ or CSS: `make gen && make fmt && make vet && make lint && make test`. Before gen inputs exist the gate is `make fmt vet lint test`. `make ci` adds `cover-check` (floors in coverage-floors.txt).

## Package map

- `cmd/vaquero`: the binary; `run(args, stdout, stderr) int` dispatches the subcommands (`version`, `help`).
- `internal/version`: `Version`, `Commit`, `BuildDate` stamped via `-ldflags -X`; `Line()`.
- `internal/idgen`: `New()` mints ULID primary keys.
- `internal/timefmt`: the only place instants are formatted for storage or display.

## Hard invariants

- Module `github.com/stazelabs/vaquero`; binary `vaquero`; `go 1.27.0`, `toolchain go1.27.3`; always `CGO_ENABLED=0`.
- Env prefix `VQ_`.
- Dependency versions match startcase's `go.mod`. No ORM, no cgo SQLite, no React, no validation library, no testify, no cobra.
- Licence Apache 2.0. Supported platforms for the local binary: macOS and Linux.
- `VQ_MODE=hosted` is refused at boot until Phase 4 adds sign-in.
- Local mode binds loopback only and rejects any request whose Host is not `localhost`, `127.0.0.1` or `::1`.
- IDs come only from `internal/idgen` (ULID). Instants are stored as text only through `internal/timefmt`.
- Writer pool has exactly one connection. Inside a transaction use `gen.New(tx)`, never `Deps.Q`. Reads on request paths use the reader (`Deps.R`).
- CSP allows no inline script and no `style=` attribute. All behaviour is in `internal/web/static/js/app.js`.
- Templates use only the theme tokens; no raw hex or oklch values in `.templ` files.
- Generated files (`*_templ.go`, `internal/db/gen`, `tailwind.css`) are committed; `make gen && git diff --exit-code` must be clean.
- Copy: the product is "Vaquero" in prose and `vaquero` as a command. Sentence case, no exclamation points. Documentation and UI never name competing products.
- No telemetry, no outbound network calls.
- Never add `push` or `pull_request` triggers to GitHub workflows.
- Commit messages: `{Area}: {summary}`.
- Departures from the siblings are deliberate: `internal/core` owns every mutation (Phase 1); passwords and passkeys (Phase 4); the CLI is an HTTP client (Phase 2).

## Rejected

mattn/go-sqlite3 (cgo), ORMs, gin/echo/fiber, React/Vite, Redis, validation libraries, testify, cobra, telemetry, Docker as a runtime requirement for local use.
