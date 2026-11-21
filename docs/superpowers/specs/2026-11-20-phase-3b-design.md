# Phase 3b: export, import and the backlog move

Amends the Phase 3 entry in `2026-11-18-vaquero-design.md`. Phase 3a (releases and upgrades) has shipped on `main`.

## Intent

Vaquero should track its own work. Data must never be locked in: one command writes everything out, another restores it. The first public release follows this phase.

## Goals

1. `vaquero export` and `vaquero import`: a full, versioned JSON archive of an instance, restorable into an empty one.
2. The open lines of `docs/BACKLOG.md` become tickets in the VAQ project of the owner's real local Vaquero.

## Non-goals

- The Linear importer. Linear's export is CSV only, delivered by an emailed link; the importer is deferred to the backlog (user decision, 2026-11-20).
- Importing into a non-empty instance, or merging two instances.
- Attachments (Phase 6). The archive format leaves room for them; it carries none now.
- The stazelabs skills adapter, profile and audit packs, and the Vaquero additions they would need (a ticket `url`, MCP `whoami` and `list_tags`). Deferred to the backlog, so the skills aren't entangled yet (user decision, 2026-11-20).
- Ticked backlog lines. Their history stays in git (user decision, 2026-11-20).

## Anti-goals

- An export that silently drops data, or an import that half-succeeds. Import is all or nothing.
- An archive that carries secrets: no token hashes, no `{data dir}/token`.
- Vaquero making outbound calls (invariant).

## Approaches

| Piece | Considered | Chosen |
|---|---|---|
| Archive | (a) one JSON file; (b) a tar.gz of JSON plus attachment bytes, as the spec said | (a): there are no attachments until Phase 6; a later format version can become an archive. Builder's call made by me. |
| Import target | (a) empty instance only; (b) merge into a live one | (a): merging renumbers tickets and breaks keys in branches and commits. |
| Backlog move | (a) open lines only, as todo tickets; (b) every line, ticked ones as done | (a), the user's pick: a clean board of real work. |

## Design

### 1. Export and import

- `vaquero export FILE` (or `-` for stdout) reads through the API, so it works against a running local server and, later, a hosted one with a token. My call: an `/api/v1/export` route owned by the owner token, streamed; the CLI writes it to a temporary file and renames it into place.
- The file is one JSON object: `{"format": "vaquero-export", "format_version": 1, "schema": <migration number>, "exported_at", "vaquero_version", ...tables}`. Tables: settings, actors (no credentials), projects (with key and counter), statuses, milestones, tickets (every column, including soft state such as `status_changed_at` and `version`), tags, ticket_tags, comments (soft-deleted ones included, flagged), ticket_links, events (with `seq`). Left out: API tokens, live claims (claim columns are exported empty), `mcp_calls`.
- Rows are written in primary-key order, so two exports of an unchanged instance are byte-identical.
- `vaquero import FILE` needs the server stopped (like `backup restore`; it takes the data-directory lock) and an instance with no projects and no tickets. A data directory that has been served already holds the owner actor, its token and their two events, which would collide with the archive's. My call: import builds a brand-new database file beside the old one (migrated to head, then filled in one transaction, bypassing `core` as the spec says), rebuilds the FTS indexes, runs `integrity_check` and `foreign_key_check`, and only then swaps it in the way `backup restore` does: the old database is snapshotted into `backups/` first (or moved aside when it can't be copied), and `{data dir}/token` is removed so the next `serve` mints a fresh owner token for the imported owner. Any failure deletes the new file and leaves the old one untouched. Import writes no event of its own, so an export of the result is byte-equal to the archive. CLI profiles holding a token for the old instance stop working, and the message says so.
- An archive whose `schema` is newer than the binary is refused with "This export is from a newer vaquero (schema N). Upgrade vaquero and import again." An older one is accepted when every field it carries maps onto the current schema; format version 1 is the only version so far.
- Round-trip test: seed a database (reuse the Phase 3a fixture generator's seed), export, import into an empty one, export again: the two files are byte-equal.
- The owner actor: the import keeps the archive's actors; the importing instance's local owner token is re-minted for the imported owner actor on next `serve`.
- Docs: a help article section (`data-and-backups`), the CLI table, and the API route.

### 2. The backlog move

- Until the first release, the owner's real Vaquero is a build of `main` (`make build`, installed as their `vaquero`, using the normal data directory); my job to install it, with the owner's yes (user decision, 2026-11-20).
- A one-time script, `scripts/backlog-import`, kept in the repo for the record. It reads `docs/BACKLOG.md` and prints the plan (each ticket's title, tag and body) for the owner to read first; with `--apply` it creates them through the CLI against the local server.
- Each unticked `- [ ]` line becomes one todo ticket in VAQ. Title: the line's first clause (up to the first " — ", ";" or "(review", cut at 80 characters on a word); body: the whole line, plus "From docs/BACKLOG.md, section: …". A multi-item line stays one ticket. My call.
- Tag: one per section, slugged (`phase-0-review`, `phase-1a`, `phase-1b`, `footer-key-delete`, `phase-2a`, `icons-help`, `phase-2b-review`, `admin`, `phase-3a`, `open-question` for the "Open questions" section). The existing Linear-importer line under Phases becomes a ticket tagged `linear-import`. My calls.
- The phase lines (Phase 4 to 7) become milestones, not tickets. My call: a phase is a goal with many tickets, which is what a milestone is. A line that names a phase ("with hosted mode, Phase 4") goes into that milestone. The Phase 3 and 3b lines are the work in progress: they are ticked when this phase lands and are not imported.
- Nothing is written to the real instance without the owner's yes on the printed plan. Before `--apply`, the script takes a backup through the server's normal snapshot path (`vaquero backup` is list and restore only, so my call: export first, with the new `vaquero export`, as the undo point).
- Afterwards `docs/BACKLOG.md` becomes a pointer ("Work is tracked in the VAQ project of vaquero itself; this file keeps the record up to Phase 3b") with the ticked history kept below it, and CLAUDE.md's backlog sentence changes to match.

## Constraints

Everything in CLAUDE.md's hard invariants holds. In particular: only `core` writes the tracker except import, which is the spec's named exception and runs offline; every write path keeps one event per change; no telemetry or outbound calls; documentation in `internal/help/articles` with tests enforcing it; new CLI commands and API routes need their article lines.

## Left to the builder

- The export's exact JSON field names per table (snake case, matching the API where a table has an API type).
- Whether export streams row by row or builds in memory (the database is small; streaming is preferred if it costs little).
- The slug rule for tags and the clause-cutting details for titles, as long as the printed plan is what gets applied.
