# Phase 3b: export, import, attachments and the backlog move

Amends the Phase 3 entry in `2026-11-18-vaquero-design.md`. Phase 3a (releases and upgrades) has shipped on `main`.

## Intent

Vaquero should track its own work. Data must never be locked in: one command writes everything out, another restores it. The owner wants to attach files to tickets, screenshots first. The order is: build export, import and attachments; merge; cut the first public release; install it through Homebrew; then move the backlog into it (user decision, 2026-11-25), so the owner's real data only ever sits on a released, frozen schema.

## Goals

1. `vaquero export` and `vaquero import`: a full, versioned JSON archive of an instance, restorable into an empty one.
2. Attachments on tickets: paste or drop a screenshot into a description or comment, or add any file on the ticket page; through the API and `vaquero attach` too.
3. Where attachment bytes live is configurable: in the SQLite database (the default), a directory, or an S3-compatible bucket. The directory and S3 backends are groundwork for hosted mode (Phase 4) (user decision, 2026-11-25).
4. After the first release, the open lines of `docs/BACKLOG.md` become tickets in the VAQ project of the owner's real local Vaquero, installed from that release.

## Non-goals

- The Linear importer. Linear's export is CSV only, delivered by an emailed link; the importer is deferred to the backlog (user decision, 2026-11-20).
- Importing into a non-empty instance, or merging two instances.
- Moving attachments from one storage backend to another. Deferred to the backlog; until it exists, changing the backend while attachments exist is refused at boot (user decision, 2026-11-25).
- Backups of the directory or S3 backends. Snapshots and `backup restore` cover the database only; with those backends the files are the operator's to back up, and the docs say so.
- An MCP upload tool, until an agent needs one.
- The stazelabs skills adapter, profile and audit packs, and the Vaquero additions they would need (a ticket `url`, MCP `whoami` and `list_tags`). Deferred to the backlog, so the skills aren't entangled yet (user decision, 2026-11-20).
- Ticked backlog lines. Their history stays in git (user decision, 2026-11-20).

## Anti-goals

- An export that silently drops data, or an import that half-succeeds. Import is all or nothing.
- An archive that carries secrets: no token hashes, no `{data dir}/token`.
- Vaquero needing the network. Local-first: everything works offline with the defaults; a network service (such as an S3 bucket) is used only when the operator configures it. Vaquero still never downloads, checks for releases or sends telemetry.
- An attachment that runs script in the app's origin: SVG, HTML and anything not a known raster image are served as downloads, never inline.
- An attachment record whose bytes are silently missing.

## Approaches

| Piece | Considered | Chosen |
|---|---|---|
| Archive | (a) one JSON file; (b) a tar.gz of JSON plus attachment bytes, as the spec said | (b), now that attachments join this phase. |
| Attachment storage | (A) one `blobs.Store` interface, ported from startcase, with SQLite, directory and S3 backends; (B) bytes in an `attachments` column, the interface only for directory and S3; (C) a second SQLite file for attachments | (A), the user's pick: one code path, every backend passes the same test suite. (B) doubles every read, write, export and delete path and leaves the hosted backends least tested; (C) loses what makes SQLite the default (snapshots include the files). |
| Default backend | SQLite, directory, S3 | SQLite, the user's pick: one file stays the whole truth, and every snapshot and restore includes the files. Cost: a bigger database and backups (500 one-megabyte screenshots add about 500 MB to each of the seven kept). |
| Import target | (a) empty instance only; (b) merge into a live one | (a): merging renumbers tickets and breaks keys in branches and commits. |
| Backlog move | (a) open lines only, as todo tickets; (b) every line, ticked ones as done | (a), the user's pick: a clean board of real work. |

## Design

### 1. Export and import

- `vaquero export FILE` (or `-` for stdout) reads through the API, so it works against a running local server and, later, a hosted one with a token. My call: an `/api/v1/export` route owned by the owner token, streamed; the CLI writes it to a temporary file and renames it into place.
- The file is a gzipped tar (`.tar.gz`): `export.json` first, then `attachments/{id}` for each attachment's bytes, in id order. Tar headers carry fixed times, owner and mode so the archive is reproducible. `export.json` is one JSON object: `{"format": "vaquero-export", "format_version": 1, "schema": <migration number>, "exported_at", "vaquero_version", ...tables}`. Tables: settings, actors (no credentials), projects (with key and counter), statuses, milestones, tickets (every column, including soft state such as `status_changed_at` and `version`), tags, ticket_tags, comments (soft-deleted ones included, flagged), ticket_links, attachments (metadata), events (with `seq`). Left out: the `blobs` table itself (its bytes are the `attachments/` entries, whatever the backend), API tokens, live claims (claim columns are exported empty), `mcp_calls`.
- Rows are written in primary-key order, so two exports of an unchanged instance are identical apart from the header's `exported_at` and `vaquero_version` (gzip without a name or time in its header). The round-trip test fixes the clock and version and compares bytes. My call.
- `settings` rows that describe the instance rather than its data (`instance_id`, `blob_store`) are not exported. Import writes them for the new instance: a fresh `instance_id`, and `blob_store` set to the backend import wrote to. Import reads the same `VQ_BLOB_STORE` and `VQ_S3_*` variables as `serve` and says which backend it is writing to; the next `serve`'s backend lock catches a mismatch (for example a `brew services` environment that differs from the shell). My call.
- `vaquero import FILE` needs the server stopped (like `backup restore`; it takes the data-directory lock) and an instance with no projects and no tickets. A data directory that has been served already holds the owner actor, its token and their two events, which would collide with the archive's. My call: import builds a brand-new database file beside the old one (migrated to head, then filled in one transaction, bypassing `core` as the spec says; attachment bytes are put into the configured backend first, and on failure the ones it put are deleted), rebuilds the FTS indexes, runs `integrity_check` and `foreign_key_check`, and only then swaps it in the way `backup restore` does: the old database is snapshotted into `backups/` first (or moved aside when it can't be copied), and `{data dir}/token` is removed so the next `serve` mints a fresh owner token for the imported owner. Any failure deletes the new file and leaves the old one untouched. Import writes no event of its own, so an export of the result is byte-equal to the archive. CLI profiles holding a token for the old instance stop working, and the message says so.
- An archive whose `schema` is newer than the binary is refused with "This export is from a newer vaquero (schema N). Upgrade vaquero and import again." An older one is accepted when every field it carries maps onto the current schema; format version 1 is the only version so far.
- Round-trip test: seed a database (reuse the Phase 3a fixture generator's seed), export, import into an empty one, export again: the two files are byte-equal.
- The owner actor: the import keeps the archive's actors; the importing instance's local owner token is re-minted for the imported owner actor on next `serve`.
- Docs: a help article section (`data-and-backups`), the CLI table, and the API route.

### 2. Attachments

**Storage.** `internal/blobs` is ported from startcase (`Store` with `Put`/`Get`/`Delete`/`List`, `ValidKey`, `PutDeadline`, the `Dir` and `S3` backends on `minio-go` v7.4.1, and the `blobtest` suite and S3 fake), without its text-by-hash helpers. A new `SQLite` backend keeps bytes in a `blobs` table (`key` primary key, `bytes` BLOB, `content_type`, `size`, `modified`) in the main database, through the writer pool. Every backend runs `blobtest.Run`. Keys are `attachments/{instance}/{ulid}` in lower case, where `instance` is a ULID minted into `settings` (`instance_id`) on first boot and never exported, so two instances sharing a bucket or directory never see each other's objects. The sweep and import only touch their own instance's prefix. My call.

**Configuration** (`internal/config`, names mirroring startcase): `VQ_BLOB_STORE` = `sqlite` (default), `dir` or `s3`; `VQ_BLOB_DIR` (default `{data dir}/attachments`); `VQ_S3_ENDPOINT`, `VQ_S3_BUCKET`, `VQ_S3_ACCESS_KEY_ID`, `VQ_S3_SECRET_ACCESS_KEY` (all four required for `s3`, each missing one named), `VQ_S3_REGION` (default `auto`). An unknown backend is refused at boot. The secret key is never logged. S3 works in local mode too; nothing restricts it to hosted mode.

**Backend lock.** `settings` records the backend (`blob_store`) when the first attachment is stored. On boot, if attachments exist and the configured backend differs, `serve` refuses: "Attachments are stored in {old}; VQ_BLOB_STORE is {new}. Set it back to {old}." When there are no attachments, the setting is free to change.

**Schema.** A migration adds `blobs` and `attachments`: `id` (ULID), `ticket_id`, `uploaded_by` (actor), `name` (the original file name, cleaned of path parts and control characters, at most 200 characters), `content_type` (sniffed from the bytes with `http.DetectContentType`, never trusted from the client), `size`, `blob_key`, `created_at`. Attachments belong to a ticket; an image pasted into a comment is an attachment of that comment's ticket.

**Write order** (startcase's rule: a row and its object are not written atomically). An upload puts the bytes through the store first, then `core.AddAttachment` writes the row and one `attachment.added` event. If the core write fails or is refused, the handler deletes the object. Whatever still slips through (a crash between the two) is an orphan: the sweep lists `attachments/` and deletes objects with no row that are older than one hour. `core.DeleteAttachment` (uploader or owner) removes the row and writes `attachment.deleted`; the sweep, or the handler straight after, removes the bytes. Project deletion and ticket hard deletion remove their attachment rows in the same transaction; their bytes go the same way. The pre-delete snapshot therefore still holds the SQLite-backend bytes.

**Limits.** 10 MB per file, enforced while reading the body (413 in the API's envelope). No per-ticket or instance quota for now.

**Serving.** `GET /attachments/{id}/{name}` streams the bytes from the store (the server proxies S3; no redirects to presigned URLs, so CSP stays `'self'` and nothing leaks a bucket URL). PNG, JPEG, GIF and WebP are served inline with their type; everything else, SVG included, as `application/octet-stream` with `Content-Disposition: attachment`. `X-Content-Type-Options: nosniff` and a `sandbox` CSP on every attachment answer. A name in the URL that doesn't match redirects to the canonical one. The URL carries no project key, so links in bodies and comments survive a key change. My call.

**Web.** On the ticket page, an Attachments section lists each file (name, size, uploader, when, download link, delete for the uploader or owner) with a drop well and a file picker. Pasting or dropping an image into the description or comment textarea uploads it (`POST /t/{ticket}/attachments`, htmx-free fetch in `app.js`) and inserts `![name](/attachments/{id}/{name})` at the cursor; a non-image inserts a link. An upload in progress inserts a placeholder line that the answer replaces; a refused upload removes it and toasts the message. On the new-ticket form there is no ticket yet, so paste is off there and the hint says to attach after creating. My call. The Markdown renderer allows `<img>` only for same-origin `/attachments/…` URLs; this also settles the backlog line about external images rendering broken (they become links). My call.

**API, CLI, MCP.** `POST /api/v1/tickets/{key}/attachments` (multipart, one `file` part), `GET /api/v1/tickets/{key}/attachments`, `GET /api/v1/attachments/{id}` (the bytes), `DELETE /api/v1/attachments/{id}`. The ticket's API body gains an `attachments` list (id, name, content type, size, uploader, created, url), so MCP `get_ticket` shows them with no new tool. `vaquero attach KEY FILE...` uploads and prints each Markdown reference; `vaquero attachments KEY` lists; `vaquero detach ID` deletes. Activity and history wording for both events.

**Docs.** A help article (`attachments`): what can be attached, the limit, how files are served, the three backends and their variables, and that only the SQLite backend is covered by snapshots and `backup restore`. Lines in the CLI and API articles.

### 3. The backlog move

- The move runs after the first release, against the owner's real Vaquero installed with `brew install` from that release, using the normal data directory (user decision, 2026-11-25; replaces the 2026-11-20 plan of a `main` build). Never against an unreleased build, so the real database never carries a schema a release doesn't have.
- A one-time script, `scripts/backlog-import`, kept in the repo for the record. It reads `docs/BACKLOG.md` and prints the plan (each ticket's title, tag and body) for the owner to read first; with `--apply` it creates them through the CLI against the local server.
- Each unticked `- [ ]` line becomes one todo ticket in VAQ. Title: the line's first clause (up to the first " — ", ";" or "(review", cut at 80 characters on a word); body: the whole line, plus "From docs/BACKLOG.md, section: …". A multi-item line stays one ticket. My call.
- Tag: one per section, slugged (`phase-0-review`, `phase-1a`, `phase-1b`, `footer-key-delete`, `phase-2a`, `icons-help`, `phase-2b-review`, `admin`, `phase-3a`, `open-question` for the "Open questions" section). The existing Linear-importer line under Phases becomes a ticket tagged `linear-import`. My calls.
- The phase lines (Phase 4 to 7) become milestones, not tickets. My call: a phase is a goal with many tickets, which is what a milestone is. A line that names a phase ("with hosted mode, Phase 4") goes into that milestone. The Phase 3 and 3b lines are the work in progress: they are ticked when this phase lands and are not imported.
- Nothing is written to the real instance without the owner's yes on the printed plan. Before `--apply`, the script takes a backup through the server's normal snapshot path (`vaquero backup` is list and restore only, so my call: export first, with the new `vaquero export`, as the undo point).
- Afterwards `docs/BACKLOG.md` becomes a pointer ("Work is tracked in the VAQ project of vaquero itself; this file keeps the record up to Phase 3b") with the ticked history kept below it, and CLAUDE.md's backlog sentence changes to match.

## Constraints

Everything in CLAUDE.md's hard invariants holds, with one change made by this phase: "No telemetry, no outbound network calls" becomes "Local-first: everything works offline with the defaults; network services are optional and used only when the operator configures them. No telemetry; Vaquero never downloads or checks for releases" (user decision, 2026-11-25; more optional network services are coming). In particular: only `core` writes the tracker except import, which is the spec's named exception and runs offline; the `blobs` table is written by the blob store, like `mcp_calls`, outside core; every write path keeps one event per change; documentation in `internal/help/articles` with tests enforcing it; new CLI commands and API routes need their article lines.

## Left to the builder

- The export's exact JSON field names per table (snake case, matching the API where a table has an API type).
- Whether export streams row by row or builds in memory (the database is small; streaming is preferred if it costs little).
- The sweep's exact orphan grace period and batch size, and whether deletes remove bytes at once or leave them to the sweep.
- How the SQLite backend streams large blobs (whole-value reads are fine at 10 MB).
- The placeholder text and the drop well's look, within the theme tokens.
- The slug rule for tags and the clause-cutting details for titles, as long as the printed plan is what gets applied.
