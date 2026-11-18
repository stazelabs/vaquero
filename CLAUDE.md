# CLAUDE.md

Guidance for working in this repo. Keep it short and current: edit it when an invariant or a product decision changes. Feature detail goes in the package's doc comment, not here.

## Summary

Vaquero is an agent-native, local-first, lightweight ticket tracker with coordination primitives for agents. One Go binary, one SQLite file. Local mode (one person, loopback) now; hosted mode (sign-in, cloud) from Phase 4. Design: docs/superpowers/specs/2026-11-18-vaquero-design.md. Plans: docs/superpowers/plans/ (Phase 0 foundation, Phase 1a core tracker). Work list: docs/BACKLOG.md (no outside tracker; it moves into vaquero itself in Phase 3). Add deferred work and open questions there, and tick lines when they ship. Infrastructure is ported from the sibling projects startcase and healthcheck-email (read-only); the porting reference is /Users/cstaszak/Stazelabs/projects/startcase/docs/superpowers/plans/2026-11-17-porting-map.md.

## Commands

    make tools     # one-time
    make gen       # templ + sqlc + tailwind (generated files are committed)
    make dev       # air hot reload on 127.0.0.1:7879 (7878 is a real vaquero's port)
    make test

Run `make` with no target to list every target; give each new target a `## description`.

Gate before every commit that touches Go, SQL, templ or CSS: `make gen && make fmt && make vet && make lint && make test`. Before gen inputs exist the gate is `make fmt vet lint test`. `make ci` adds `cover-check` (floors in coverage-floors.txt).

## Package map

- `cmd/vaquero`: the binary; `run(args, stdout, stderr) int` dispatches the subcommands (`serve`, `version`, `help`); `serve.go` has `serve(ctx, cfg, logger)` (binds the address first, then creates the 0700 data dir, opens and migrates the DB, builds `core` and the owner actor, serves, drains 30 s on cancel, checkpoints the WAL); `main.go` has `openDB` and `newLogger`.
- `internal/version`: `Version`, `Commit`, `BuildDate` stamped via `-ldflags -X`; `Line()`.
- `internal/config`: `Load()`/`FromEnv(get)` read `VQ_MODE`, `VQ_ADDR`, `VQ_DATA_DIR`, `VQ_LOG_FORMAT`, `VQ_LOG_LEVEL` into one flat `Config`; local mode refuses a non-loopback address; hosted mode is refused for now; `DefaultDataDir(goos, get)`; `AllowedHosts()`.
- `internal/idgen`: `New()` mints ULID primary keys.
- `internal/timefmt`: the only place instants are formatted for storage or display.
- `internal/db`: SQLite writer/reader pools (`Open`, `OpenReader`, `Checkpoint`), embedded goose migrations (`Migrate` refuses a database from a newer vaquero; `Newest`, `Version`, `MigrateTo`); `queries/` feeds sqlc into `db/gen`; `db/dbtest.New(t)` gives a migrated per-test database. Tables: `settings`; the tracker (`0002_tracker.sql`): `actors`, `projects`, `statuses`, `milestones`, `tickets`, `tags`, `ticket_tags`, `comments`, `ticket_links`, `events`.
- `internal/core`: the only code that writes the tracker. `Core{DB, Now}`; each operation is one writer transaction that validates, writes and appends exactly one `events` row (a refused one writes nothing; a no-op update writes nothing). Errors: `*Invalid{Fields}` (form field → sentence-case message) and `ErrNotFound`. Operations: `EnsureOwner`; `CreateProject` (seeds six statuses) / `UpdateProject` / `SetProjectArchived`; `CreateStatus` / `RenameStatus` / `MoveStatus` / `DeleteStatus` (keeps one per category, refuses one in use); `CreateMilestone` / `UpdateMilestone` / `SetMilestoneClosed`; `CreateTicket` / `UpdateTicket` (tags created as needed; parent same project, no cycles; position spaced by 1024, end of column on status change); `AddComment` / `EditComment` / `DeleteComment` (author only, soft delete); `AddLink` / `RemoveLink` (blocks, relates, duplicates; any project). `ParseTicketKey` ("vaq-012" → VAQ-12), `ResolveTicket`, `Categories`, `LinkKinds`, `EventKinds`, the `Ev*` kinds and their payloads (events.go). `TestEveryOperationEmitsOneEventOrNothing` covers every operation.
- `internal/markdown`: `Render(src) template.HTML` for ticket bodies and comments: goldmark (GFM), raw HTML shown as escaped text, then bluemonday's user-content policy with nofollow links.
- `internal/web`: `NewRouter(handlers.Deps)` mounts every route (global middleware with chi `GetHead`, `/healthz`, `/static/*`, `/` projects, `/activity`, `/projects/new`, `/p/{key}/...`, `/t/{ticket}/...`); `static.go` serves the embedded tree.
- `internal/web/middleware`: request id, recover, `HostCheck` (403 for any Host but localhost, 127.0.0.1, ::1), request log, security headers, CSP nonce, `CrossOrigin` (403 for cross-site posts), `BodyLimit`, `CurrentPath`.
- `internal/web/flash`: one toast across a redirect in a 60-second `vq_flash` cookie (local mode has no sessions): `Put`, `Pop`, `WithCurrent`, `Current`.
- `internal/web/handlers`: `Deps` (Cfg, DB for /healthz only, R reader, Core, Actor, Logger, Now) plus `render` (pops the toast), `serverError`, `redirectWithFlash`, `invalidOrError`. `projects.go` (list, new, project page grouped by status, settings: name, statuses, milestones, archive; `{key}` case redirect), `tickets.go` (new/edit form with the parent typed as a key, ticket page with sub-tickets, links, comments and history; `{ticket}` canonical redirect; comment and link actions), `activity.go` (newest 100 events), `misc.go`. Each handler is `func(Deps) http.HandlerFunc`. `TestHandlersNeverWriteDirectly` holds the core-only rule.
- `internal/web/templates`: templ components: `Shell` (toast, nav, tagline footer), project pages (`projects.templ`), ticket form and page (`tickets.templ`), `ActivityPage` and `EventLine`; `TextInput`, `TextArea`, `Select`, `FieldError` (`components.templ`); `EventSentence` wording for every event kind (`activity.go`), `CategoryLabel`, `PriorityLabel` and options (`labels.go`); class recipes in `helpers.go`. Theme tokens only.
- `internal/web/static`: embedded `css` (input.css with the `prose-vq` markdown styles + generated tailwind.css), `js/app.js` (toast fade and close), `vendor/htmx.min.js`, `favicon.svg`; `URL(path)` adds a content hash.
- `internal/web/webtest`: `New(t)` boots the real router in local mode on a test database with a fixed clock, core and the owner (`Deps.Core`, `Deps.Actor` to arrange state); `Client` (cookie jar; `Get`, `Head`, `GetWithHost`, `Post`, `PostCrossSite`, `Follow`; never follows redirects itself).

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
- Only `internal/core` writes the tracker tables, and every successful change appends exactly one `events` row in the same transaction. Handlers read through `Deps.R` and write only through `Deps.Core`.
- Project keys never change once created. Ticket keys are `KEY-N`; URLs accept any case and leading zeros and redirect to the canonical form.
- Departures from the siblings are deliberate: `internal/core` owns every mutation; passwords and passkeys (Phase 4); the CLI is an HTTP client (Phase 2).

## Rejected

mattn/go-sqlite3 (cgo), ORMs, gin/echo/fiber, React/Vite, Redis, validation libraries, testify, cobra, telemetry, Docker as a runtime requirement for local use.
