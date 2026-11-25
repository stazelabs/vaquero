---
paths:
  - "internal/db/**"
  - "internal/web/handlers/**"
  - "internal/timefmt/**"
  - "internal/idgen/**"
  - "**/*.sql"
---

# Database access, schema and instants

One SQLite file. CLAUDE.md "Hard invariants" is the authority.

## Queries

- SQL lives in internal/db/queries; `make sqlc` (part of `make gen`) writes internal/db/gen. Never edit generated files.
- Only internal/core writes the tracker tables (actors through events). Everything else reads through the reader pool. The one offline exception is `vaquero import` (`archive.Load` into a new file, then swapped in). The `blobs` table is written only through internal/blobs.
- Rejected: ORMs and mattn/go-sqlite3.

## Reader and writer pools (internal/db/sqlite.go)

- The reader pool (`Deps.R`) is opened with `query_only(true)`. Never write through it; a stray write fails loudly. Reads go through `Deps.R`.
- The writer pool has exactly one connection (`Deps.Q` / `Deps.DB`). Inside a transaction use `gen.New(tx)`, never `Deps.Q`: using `Deps.Q` inside a tx deadlocks.
- Do reads that decide a write inside the same transaction.

## Migrations (goose, internal/db/migrations)

- Append-only and forward-only. Never edit a migration that has shipped in a release; add a new one.
- Users upgrade by installing a new binary, so every migration must run unattended on any older database. A destructive change ships as an additive step in one release and the removal in a later one.
- Create with `make migration name=add_thing`. Author migrations with `make dev` stopped (air migrates on rebuild and would apply a half-written file).
- Migrations run at boot, after the listener binds and before it serves anything. `Migrate` refuses a database written by a newer vaquero.
- After a schema or query change, run `make gen` and commit the generated files.

## Search indexes

- `tickets_fts` and `comments_fts` are FTS5 external-content tables keyed on the tickets' and comments' implicit rowids, kept current by triggers (insert, update, soft delete, hard delete). Never `VACUUM` the live database in place: it can renumber rowids and attach hits to the wrong tickets. Snapshots use `VACUUM INTO` a copy, which is fine.
- The search query is hand-written in `internal/db/search.go` because sqlc can't read FTS5 tables.

## Instants and IDs

- Format and parse instants only through internal/timefmt. Never call `time.Format` with your own layout for storage.
- IDs only through `idgen.New()` (ULIDs).
- Inject the clock: handlers use `d.Now`, not `time.Now`, so tests control time.

## Tests

internal/db/dbtest gives a migrated scratch database. Test queries against it; never mock the database. Outside internal/db, never call `Migrate` in a test (`TestNoMigrateOutsideDB`).
