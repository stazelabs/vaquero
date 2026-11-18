# CLAUDE.md

Guidance for working in this repo. Keep it short and current: edit it when an invariant or a product decision changes. Feature detail goes in the package's doc comment, not here.

## Summary

Vaquero is an agent-native, local-first, lightweight ticket tracker with coordination primitives for agents. One Go binary, one SQLite file. Local mode (one person, loopback) now; hosted mode (sign-in, cloud) from Phase 4. Design: docs/superpowers/specs/2026-11-18-vaquero-design.md. Plans: docs/superpowers/plans/2026-11-18-phase-0-foundation.md. Infrastructure is ported from the sibling projects startcase and healthcheck-email (read-only); the porting reference is /Users/cstaszak/Stazelabs/projects/startcase/docs/superpowers/plans/2026-11-17-porting-map.md.

## Commands

    make tools     # one-time
    make gen       # templ + sqlc + tailwind (generated files are committed)
    make dev       # air hot reload on 127.0.0.1:7879 (7878 is a real vaquero's port)
    make test

Run `make` with no target to list every target; give each new target a `## description`.

Gate before every commit that touches Go, SQL, templ or CSS: `make gen && make fmt && make vet && make lint && make test`. Before gen inputs exist the gate is `make fmt vet lint test`. `make ci` adds `cover-check` (floors in coverage-floors.txt).

## Package map

- `cmd/vaquero`: the binary; `run(args, stdout, stderr) int` dispatches the subcommands (`serve`, `version`, `help`); `serve.go` has `serve(ctx, cfg, logger)` (binds the address first, then creates the 0700 data dir, opens and migrates the DB, serves, drains 30 s on cancel, checkpoints the WAL); `main.go` has `openDB` and `newLogger`.
- `internal/version`: `Version`, `Commit`, `BuildDate` stamped via `-ldflags -X`; `Line()`.
- `internal/config`: `Load()`/`FromEnv(get)` read `VQ_MODE`, `VQ_ADDR`, `VQ_DATA_DIR`, `VQ_LOG_FORMAT`, `VQ_LOG_LEVEL` into one flat `Config`; local mode refuses a non-loopback address; hosted mode is refused for now; `DefaultDataDir(goos, get)`; `AllowedHosts()`.
- `internal/idgen`: `New()` mints ULID primary keys.
- `internal/timefmt`: the only place instants are formatted for storage or display.
- `internal/db`: SQLite writer/reader pools (`Open`, `OpenReader`, `Checkpoint`), embedded goose migrations (`Migrate` refuses a database from a newer vaquero; `Newest`, `Version`, `MigrateTo`); `queries/` feeds sqlc into `db/gen`; `db/dbtest.New(t)` gives a migrated per-test database. Tables: `settings`.
- `internal/web`: `NewRouter(handlers.Deps)` mounts every route (global middleware, `/healthz`, `/static/*`, `/`); `static.go` serves the embedded tree.
- `internal/web/middleware`: request id, recover, `HostCheck` (403 for any Host but localhost, 127.0.0.1, ::1), request log, security headers, CSP nonce, `CrossOrigin` (403 for cross-site posts), `BodyLimit`, `CurrentPath`.
- `internal/web/handlers`: `Deps` (Cfg, DB, Q, R, Logger, Now) plus `render`; `Healthz`, `NotFound`, `MethodNotAllowed`, `Home`; each handler is `func(Deps) http.HandlerFunc`.
- `internal/web/templates`: templ components: `Shell`, `HomePage`, `NotFoundPage`, `ErrorPage`; class recipes in `helpers.go`. Theme tokens only.
- `internal/web/static`: embedded `css` (input.css + generated tailwind.css), `js/app.js`, `vendor/htmx.min.js`, `favicon.svg`; `URL(path)` adds a content hash.
- `internal/web/webtest`: `New(t)` boots the real router in local mode on a test database with a fixed clock; `Client` (`Get`, `GetWithHost`, `Post`, `PostCrossSite`; never follows redirects).

Agent rules for each area are in `.claude/rules/` (database, handlers, security, ui-theming, copy-voice).

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
