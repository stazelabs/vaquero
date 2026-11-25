# Phase 3b: Export, import and attachments: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tickets carry attachments: paste a screenshot into a description or comment, or add any file. Their bytes live in a configurable store: SQLite by default, a directory, or S3. `vaquero export` writes a whole instance to a `.tar.gz` and `vaquero import` restores one into an empty data directory. Then v0.1.0 is released, and the backlog moves into the released Vaquero.

**Architecture:** `internal/blobs` is ported from startcase, with its `Dir` and `S3` backends, and gains a `SQLite` backend that writes the `blobs` table. Attachment records live in `attachments`, written only by `internal/core`. An upload puts the bytes first and then writes the row, and the sweep removes orphans. `internal/archive` dumps and loads every exported table generically (column name → value), so new columns are exported without code changes. Export is an API route. Import runs offline under the data-directory lock, builds a new database beside the old one and swaps it in the way `backup restore` does.

**Tech Stack:** As Phase 3a, plus `github.com/minio/minio-go/v7` v7.4.1 (startcase's version). The standard library covers `archive/tar`, `compress/gzip`, `mime/multipart` and `http.DetectContentType`.

**Spec:** `docs/superpowers/specs/2026-11-20-phase-3b-design.md` (approved 2026-11-25). Read it first: it holds the decisions, and this plan only sequences them.

**Patterns to follow (read-only):** `/Users/cstaszak/Stazelabs/projects/startcase/internal/blobs` (all of it), and `startcase/internal/config/config.go` `readBlobs`.

## Global Constraints

- CLAUDE.md invariants hold, except "No telemetry, no outbound network calls". Task 2 rewrites that rule to: "Local-first: everything works offline with the defaults; network services are optional and used only when the operator configures them. No telemetry; Vaquero never downloads or checks for releases." The same wording goes in `.claude/rules/security.md` "Outbound network".
- Env: `VQ_BLOB_STORE` = `sqlite` (default), `dir` or `s3`; `VQ_BLOB_DIR` (default `{data dir}/attachments`); `VQ_S3_ENDPOINT`, `VQ_S3_BUCKET`, `VQ_S3_ACCESS_KEY_ID`, `VQ_S3_SECRET_ACCESS_KEY` (all required for `s3`), and `VQ_S3_REGION` (default `auto`). The secret is never logged.
- Blob keys are `attachments/{instance}/{attachment ulid}`, all lower case. `instance` is the `settings` row `instance_id`.
- Attachments are at most 10 MB (`10 << 20` bytes) per file. PNG, JPEG, GIF and WebP are served inline. Everything else is served as `application/octet-stream` with `Content-Disposition: attachment`. Every answer carries `X-Content-Type-Options: nosniff` and `Content-Security-Policy: sandbox`.
- Attachment URL: `/attachments/{id}/{name}`, with no project key.
- The archive is a gzip with no name and no time, holding a tar. Its entries are `export.json` first, then `attachments/{id}` in id order. Each tar header has mtime 0 (Unix epoch), uid and gid 0, no user or group name, and mode 0644. Header fields: `format: "vaquero-export"`, `format_version: 1`, `schema`, `exported_at`, `vaquero_version`.
- Exported tables, rows in primary-key order: settings (except `instance_id` and `blob_store`), actors, projects, statuses, milestones, tickets (claim columns exported as null), tags, ticket_tags, comments, ticket_links, attachments, events. Left out: api_tokens, mcp_calls, blobs, the FTS tables and goose's table.
- Only `core` writes the tracker tables. `attachments` is a tracker table. `blobs` is written only through the blob store, like `mcp_calls`, and `TestHandlersNeverWriteDirectly` must still pass. Import is the named exception and runs offline.
- New CLI commands (`attach`, `attachments`, `detach`, `export`, `import`) and API routes get lines in the `cli` and `api` help articles; tests enforce this. Copy follows `.claude/rules/copy-voice.md`.
- Gate steps run separately and are judged by exit code: `make gen`, `make fmt`, `make vet`, `make lint`, `make test`. `make ci` ends each task. Commits are `{Area}: {summary}` with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- `testdata/upgrade/v0.1.0.*` is unreleased (no tag exists), so Task 10 remakes it after the migration lands. No other fixture exists.

## Review Focus

1. **A file over the limit.** `middleware.BodyLimit` caps every request at 1 MiB today. The upload routes need 10 MB plus multipart overhead. A file over 10 MB must get the API's 413 envelope with "Files can be at most 10 MB." On the web, the paste placeholder is removed and that toast is shown. It must never be a bare 413 or a reset connection. Task 4: `TestUploadTooLarge`, at 10 MB + 1 byte, and `TestUploadAtLimit`, at exactly 10 MB.
2. **A disguised file.** An HTML page named `shot.png`, or an SVG sent with `Content-Type: image/png`, is sniffed as text, stored with the sniffed type and served as a download. It never renders inline, and an `<img>` pointing at it shows nothing. Task 4: `TestDisguisedUploadServedAsDownload`.
3. **Awkward file names.** Spaces, `#`, `?`, `%`, slashes, `..`, RTL marks, emoji and names over 200 characters. The stored name is cleaned. The URL escapes it and round-trips to the canonical redirect without looping. `Content-Disposition` uses `filename*=UTF-8''…`. Task 3: `TestAttachmentNameCleaned`. Task 4: `TestAttachmentURLRoundTrip`.
4. **A missing object at export.** If a dir or S3 object was deleted out of band, export fails, names the attachment and its ticket, and leaves no file at the destination. It never writes an archive that silently lacks the bytes. Task 7: `TestExportMissingBlobFails`.
5. **Import and serve disagree on the backend.** For example, import writes to `dir` from the shell, but `brew services` runs `sqlite`. The next `serve` refuses with the backend-lock message, names both backends and changes nothing. Task 8: `TestImportThenServeWithOtherBackendRefuses`.

---

### Task 1: The blob store

**Files:** `internal/blobs/{blobs.go,dir.go,s3.go,sqlite.go}` (+ tests), `internal/blobs/blobtest/{blobtest.go,s3fake.go}`, `internal/db/migrations/0007_attachments.sql`, `go.mod`/`go.sum`, `internal/legal/third_party.json` (regenerated by `make gen`)

**Interfaces:**
- Produces: `blobs.Store` (`Put(ctx, key, r io.Reader, size int64, contentType string) error`, `Get(ctx, key) (io.ReadCloser, int64, error)`, `Delete(ctx, key) error`, `List(ctx, prefix, fn func(key string, modified time.Time) error) error`); `blobs.ErrNotFound`; `blobs.ValidKey(key string) error`; `blobs.PutDeadline`; `blobs.NewDir(root string) (*Dir, error)`; `blobs.NewS3(endpoint, bucket, accessKey, secretKey, region string) (*S3, error)`; `blobs.NewSQLite(w, r *sql.DB, now func() time.Time) *SQLite`; `blobtest.Run(t, func(t) blobs.Store)`.
- The migration `0007_attachments.sql` creates two tables:
  - `blobs (key TEXT PRIMARY KEY, bytes BLOB NOT NULL, content_type TEXT NOT NULL, size INTEGER NOT NULL, modified TEXT NOT NULL) WITHOUT ROWID`.
  - `attachments (id TEXT PRIMARY KEY, ticket_id TEXT NOT NULL REFERENCES tickets(id), uploaded_by TEXT NOT NULL REFERENCES actors(id), name TEXT NOT NULL, content_type TEXT NOT NULL, size INTEGER NOT NULL, blob_key TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL)`, plus an index on `ticket_id`.

Port startcase's package without its text-by-hash helpers (`TextKey`, `PutText`, `GetText`) or their tests, and rewrite the package comment for attachments. `SQLite` writes through `w` and reads through `r`. `Put` reads the whole body (at most size+1 bytes, refusing a size mismatch) and then upserts. `List` orders by key. Its sqlc queries go in `internal/db/queries/blobs.sql`.

- [ ] **Tests first:**
  - `TestDir`, `TestS3` and `TestSQLite` each call `blobtest.Run`. `TestS3` runs against the ported fake.
  - `TestSQLiteRefusesSizeMismatch`: a Put given 3 bytes with size 4 fails and stores nothing.
  - `TestSQLiteKeyValidated`: `../x` is refused before any SQL runs.
- [ ] Watch them fail, port and implement, run `make gen` (sqlc, notices), gate, and commit `"Attachments: the blob store, ported with a SQLite backend"`.

### Task 2: Choosing the store, instance id and backend lock

**Files:** `internal/config/config.go` (+test), `internal/blobs/open.go` (+test), `cmd/vaquero/serve.go`, `cmd/vaquero/serve_test.go`, `CLAUDE.md`, `.claude/rules/security.md`

**Interfaces:**
- Consumes: Task 1.
- Produces:
  - `Config` fields `BlobBackend`, `BlobDir`, `S3Endpoint`, `S3Bucket`, `S3AccessKeyID`, `S3SecretAccessKey []byte` and `S3Region`, read the way startcase reads them. Every missing S3 variable is named in one joined error.
  - `blobs.Open(cfg config.Config, w, r *sql.DB, now func() time.Time) (Store, error)`, which makes `BlobDir` with mode 0700 for `dir`.
  - `ensureInstance(ctx, w *sql.DB, now time.Time) (string, error)` in `serve.go`, which mints `instance_id` once.
  - `checkBlobBackend(ctx, r *gen.Queries, configured string) error`.

`checkBlobBackend` refuses exactly when `settings.blob_store` is set, differs from the configured backend and `attachments` has rows. The refusal reads: `Attachments are stored in {old}; VQ_BLOB_STORE is {new}. Set it back to {old}.` `serve` runs `ensureInstance` and `checkBlobBackend` after `openDB` and before the HTTP server starts, and returns that refusal as its error. Settings rows are not tracker tables, so `serve` writes them through `gen.New(w)`, as it does for the token. Update `CLAUDE.md`: the invariant (exact wording in Global Constraints), the `config` line, a new `blobs` package line, and `serve`'s sequence. Update `security.md` "Outbound network".

- [ ] **Tests first:**
  - `TestBlobConfigDefaults`: `sqlite`, `{data dir}/attachments`, region `auto`.
  - `TestBlobConfigS3Missing`: names all four variables.
  - `TestBlobConfigUnknown`: `ftp` is refused.
  - `TestInstanceIDStable`: two boots give the same id.
  - `TestServeRefusesBackendChange`: insert one attachment row and `blob_store=sqlite`, then serve with `VQ_BLOB_STORE=dir`. The error matches exactly and the database is unchanged.
  - `TestServeAllowsBackendChangeWhenEmpty`.
- [ ] Watch them fail, implement, gate, and commit `"Attachments: choose the blob store, guard against switching it"`.

### Task 3: Attachments in core

**Files:** `internal/core/attachments.go` (+test), `internal/core/events.go`, `internal/core/projects.go` (DeleteProject), `internal/core/core_test.go` (the every-operation table), `internal/db/queries/attachments.sql`, `internal/web/templates/activity.go` (wording)

**Interfaces:**
- Produces:
  - `core.AttachmentInput{TicketID, Name, ContentType, BlobKey string; Size int64}`.
  - `(*Core).AddAttachment(ctx, by string, in AttachmentInput) (gen.Attachment, error)`. It records `blob_store` in settings in the same transaction when that setting is unset. The caller passes the backend name as `Core.BlobStore string`, a new field set in `serve`.
  - `(*Core).DeleteAttachment(ctx, by, id string) (blobKey string, err error)`: only the uploader or the owner may delete. It returns the key so the caller can delete the bytes.
  - `core.CleanName(name string) string`.
  - `EvAttachmentAdded = "attachment.added"` and `EvAttachmentDeleted = "attachment.deleted"`, with payload `{"attachment_id", "name"}`.
  - `DeleteProject` deletes the project's attachment rows before its tickets.

`CleanName` takes the base name after `/` and `\`, drops control and bidi-override characters, trims spaces and dots, and cuts to 200 characters on a rune boundary while keeping the extension. An empty result becomes `file`. `AddAttachment` validates name, size (1 to 10 MB) and that the ticket exists. Everything is refused through `*Invalid`.

- [ ] **Tests first:**
  - `TestAddAttachmentEmitsOneEvent`.
  - `TestDeleteAttachmentOnlyUploaderOrOwner`.
  - `TestAttachmentNameCleaned`: a table covering `../../etc/passwd` → `passwd`, `a‮exe.png` → `aexe.png`, `"  .  "` → `file`, and 300 `é` characters plus `.png` → 200 runes ending in `.png`.
  - `TestFirstAttachmentRecordsBackend`.
  - `TestDeleteProjectRemovesAttachments`: extend `TestDeleteProjectRemovesEverything`.
  - Add both operations to `TestEveryOperationEmitsOneEventOrNothing` and `TestEveryOperationRollsBackPartWay`.
  - Wording tests for both events: "attached shot.png" and "removed the attachment shot.png".
- [ ] Watch them fail, implement, gate, and commit `"Attachments: core operations and events"`.

### Task 4: Upload, serve and sweep

**Files:** `internal/web/handlers/attachments.go` (+test), `internal/web/handlers/deps.go` (`Blobs blobs.Store`), `internal/web/router.go`, `internal/web/middleware/bodylimit.go`, `internal/markdown/markdown.go` (+test), `internal/sweep/sweep.go` (+test), `cmd/vaquero/serve.go`, `internal/web/webtest` (gives Deps a SQLite store)

**Interfaces:**
- Consumes: Tasks 1 to 3.
- Produces:
  - `attachKey(instance, id string) string`.
  - `storeUpload(ctx, d Deps, by, ticketID string, part io.Reader, filename string) (gen.Attachment, error)`, shared by the web and API uploads.
  - Routes: `POST /t/{ticket}/attachments` (web: answers JSON `{id, name, url, markdown, image}`, or the API failure envelope), `POST /t/{ticket}/attachments/{id}/delete` (form, flash, redirect) and `GET /attachments/{id}/{name}`.
  - `middleware.MaxUpload = 10<<20 + 64<<10`.
  - `Deps.Instance string`.
  - Sweep fields `Blobs blobs.Store`, `Instance string` and `OrphanAge = time.Hour`.

The body limit for the two upload routes (this one and the API's in Task 6) is `MaxUpload`, chosen by path inside `BodyLimit` or by mounting those routes outside the 1 MiB wrapper. Pick whichever keeps the global chain readable, and say which in the commit. `storeUpload` reads at most 10 MB + 1 bytes and refuses anything over 10 MB with 413 and "Files can be at most 10 MB." It sniffs the first 512 bytes, mints the id, puts the bytes under `PutDeadline`, then calls `core.AddAttachment`. If that refuses or fails, it deletes the key. The serving handler redirects with 301 when `{name}` isn't the canonical escaped name. It sets the headers from Global Constraints and streams from the store. In markdown, a goldmark image renderer emits `<img>` only when the destination starts with `/attachments/` (bluemonday must keep `src` for those). Any other image becomes a link to its URL with the alt text as its label. The sweep lists `attachments/{instance}/` and deletes a key with no `attachments` row whose modified time is older than `OrphanAge`. A failed blob delete after `DeleteAttachment` is logged, and the sweep then catches it.

- [ ] **Tests first:**
  - `TestUploadAndServeImage`: PNG bytes are served inline as `image/png`, with nosniff and the sandbox CSP.
  - `TestUploadTooLarge` and `TestUploadAtLimit`.
  - `TestDisguisedUploadServedAsDownload`.
  - `TestAttachmentURLRoundTrip`: names `a b#?%.png` and `résumé 📎.pdf`. A wrong name redirects once to the canonical one, and the canonical one answers 200.
  - `TestUploadRefusedLeavesNoBlob`: upload to a missing ticket, then the store's `List` is empty.
  - `TestDeleteAttachmentRemovesBytes`.
  - `TestMarkdownImagesOnlyForAttachments`: `![x](/attachments/01h/x.png)` renders an `img`. `![x](https://e.com/x.png)` renders `<a href="https://e.com/x.png" rel="nofollow">x</a>`. `![x](javascript:alert(1))` renders no link.
  - `TestSweepDeletesOldOrphans`: a fresh orphan stays, an old orphan goes, a referenced blob stays, and another instance's prefix is never listed.
  - `TestHandlersNeverWriteDirectly` still passes.
- [ ] Watch them fail, implement, gate, and commit `"Attachments: upload, serve, sweep orphans; markdown images only from attachments"`. Tick the backlog line about external images rendering broken.

### Task 5: The ticket page

**Files:** `internal/web/templates/tickets.templ` (+ generated), `internal/web/handlers/tickets.go` (loads the attachment list), `internal/web/static/js/app.js`, `internal/web/static/css/input.css` if needed, `internal/help/articles/keyboard-shortcuts.md` only if a shortcut is added (none is planned)

**Interfaces:**
- Consumes: Task 4's routes and JSON answer.
- Produces: an `Attachments` section on the ticket page. Each row shows the name (linking to the URL), the size in words (reuse `sizeWords` by moving it to `internal/web/templates/labels.go`, or equivalent), the uploader, a relative time and, for the uploader or owner, a delete button. A drop well with a hidden file input and a "Choose files" button sits on the page. The description and comment textareas carry `data-attach-url` on the edit and ticket pages, and nothing on the new-ticket page, whose hint reads "Attach files after creating the ticket."

In `app.js`, paste or drop on a `[data-attach-url]` textarea, or a drop or file choice on the well, uploads each file with `fetch` (`FormData` with a `file` part, `credentials: same-origin`). Before the upload it inserts `Uploading {name}…` at the cursor, then replaces that line with the answer's `markdown`, or removes it and shows the answer's message as a toast. A well upload refreshes the attachments section (`hx-trigger="vq:change"` already reloads live parts, so wire it as a `data-live` part). There is no inline style, and theme tokens only.

- [ ] **Tests first (Go, through webtest):**
  - `TestTicketPageListsAttachments`: name, size and a download link.
  - `TestDeleteButtonOnlyForUploaderOrOwner`.
  - `TestNewTicketFormHasNoAttach`.
- [ ] Implement, then run `make gen`. Check in Chrome by hand, as Phase 1b did for drag and drop:
  - Paste a screenshot into a comment and into the description.
  - Drop a PDF on the well.
  - Drop an 11 MB file and get a toast.
  - Check dark mode.
  - Check a narrow viewport.
  Record what was checked in the commit body.
- [ ] Gate, then commit `"Attachments: paste, drop and list on the ticket page"`.

### Task 6: API, MCP and CLI

**Files:** `internal/api/types.go` (`Attachment`; `Ticket.Attachments`), `internal/web/handlers/api_attachments.go` (+test), `internal/web/handlers/api_read.go` (ticket body), `internal/web/router.go` (`mountAPI`), `cmd/vaquero/cli_attach.go` (+test), `cmd/vaquero/cli_cmds.go` (register), `internal/client/client.go` (a multipart upload helper), `internal/help/articles/{api.md,cli.md,attachments.md}` (new article), `internal/mcpserver` (check the `get_ticket` description mentions attachments)

**Interfaces:**
- Consumes: Task 4's `storeUpload`.
- Produces:
  - `api.Attachment{ID, Name, ContentType string; Size int64; UploadedBy, CreatedAt, URL string}`.
  - `POST /api/v1/tickets/{ticket}/attachments` (multipart, `file` part; 201 with the `Attachment`).
  - `GET /api/v1/tickets/{ticket}/attachments`.
  - `GET /api/v1/attachments/{id}` (the bytes, same headers as the web).
  - `DELETE /api/v1/attachments/{id}` (204).
  - `(*client.Client).Upload(ctx, path, filename string, r io.Reader, out any) error`.
  - Commands `attach KEY FILE...`, `attachments KEY` and `detach ID`.

`attach` prints one Markdown reference per file. A file over 10 MB is refused before upload with the same message. `-` reads stdin and needs `--name`. With `--json` it prints the list of `Attachment`. `attachments` prints a table with the columns ID, NAME, SIZE, BY and WHEN. The `attachments` help article covers what can be attached, the limit, how files are served, the three backends with their variables, and that only SQLite storage is covered by snapshots and `backup restore`.

- [ ] **Tests first:**
  - `TestAPIAttachmentLifecycle`: upload, list, the ticket body includes it, get bytes, delete, then 404.
  - `TestAPIAttachRefusedForMissingTicket`.
  - `TestCLIAttach`: a temp PNG prints `![…](/attachments/…)`. `--json` output parses.
  - `TestCLIAttachTooLarge`: exit 1 and no request made.
  - `TestMCPGetTicketListsAttachments`.
  - The doc tests (`TestCLICommandsDocumented` and the API-route test) pass with the new lines.
- [ ] Watch them fail, implement, gate, and commit `"Attachments: API routes, vaquero attach, attachments and detach"`.

### Task 7: Export

**Files:** `internal/archive/{archive.go,dump.go}` (+test), `internal/core/coreseed/seed.go` (moved from `scripts/fixture`'s `seed`, now also attaching two files), `scripts/fixture/main.go` (calls `coreseed.Seed`), `internal/web/handlers/api_export.go` (+test), `internal/web/router.go`, `cmd/vaquero/cli_export.go` (+test), `internal/help/articles/{data-and-backups.md,cli.md,api.md}`

**Interfaces:**
- Consumes: Tasks 1 and 3.
- Produces:
  - `archive.Tables []string`: the exported tables, in dependency order.
  - `archive.Header{Format string; FormatVersion int; Schema int64; ExportedAt, VaqueroVersion string}`.
  - `archive.Write(ctx, w io.Writer, db *sql.DB, store blobs.Store, h Header) error`: runs inside one read transaction on `db`, so every table sees one snapshot.
  - `coreseed.Seed(ctx, c *core.Core, q *gen.Queries, store blobs.Store, instance string, now time.Time) error`.
  - `GET /api/v1/export`, owner token only (403 `"Only the owner can export."` for any other actor). It streams `application/gzip` with `Content-Disposition: attachment; filename="vaquero-{YYYYMMDD-HHMMSS}.tar.gz"`.
  - `vaquero export FILE|-`.

`dump.go` reads columns from `pragma_table_info` and the primary key from the `pk` column, then writes each table as a JSON array of objects. Values are emitted as-is: int64, float64, string, null; there are no BLOB columns in exported tables, so refuse one if found. It nulls the claim columns on tickets and skips the two instance-local settings keys. The JSON is encoded with sorted keys and no indentation, one table per top-level key in `Tables` order. The handler must not hold the writer. If any blob is missing, `Write` returns an error naming the attachment id and its ticket key before the first byte reaches the client, so check all keys exist before streaming. The CLI writes to `FILE.tmp` and renames it, so a failure leaves nothing at `FILE`.

- [ ] **Tests first:**
  - `TestExportDeterministic`: seed with a fixed clock and version. Two exports are byte-equal.
  - `TestExportLeavesOutSecrets`: no `api_tokens`, no token hash and no `mcp_calls` anywhere in the archive bytes. The claim columns are null.
  - `TestExportEveryColumn`: every column of every exported table appears in `export.json`. This guards future migrations.
  - `TestExportMissingBlobFails`.
  - `TestAPIExportOwnerOnly`.
  - `TestCLIExportAtomic`: a server error leaves no `FILE`.
- [ ] Watch them fail, implement, gate, and commit `"Data: vaquero export, a reproducible tar.gz of the instance"`.

### Task 8: Import

**Files:** `internal/archive/load.go` (+test), `cmd/vaquero/cli_import.go` (+test), `cmd/vaquero/local_cmds.go` (factor the swap out of `backupRestore` into `swapDatabase(ctx, cfg, newPath string) (aside string, err error)`), `internal/help/articles/{data-and-backups.md,cli.md}`

**Interfaces:**
- Consumes: Tasks 1, 2 and 7.
- Produces:
  - `archive.Read(r io.Reader) (*Reader, error)`. The `Reader` has `Header`, `Tables map[string][]map[string]any` and `Blob(id) (io.Reader, int64, bool)`. It spools the blob entries to a temp dir, deleted by `Close`.
  - `archive.Load(ctx, tx *sql.Tx, rd *Reader) error`.
  - `vaquero import FILE`.

`Read` refuses each of these with a sentence:
- `format` other than `vaquero-export` or `format_version` other than 1.
- `schema` newer than `appdb.Newest()`: "This export is from a newer vaquero (schema N). Upgrade vaquero and import again."
- An attachment row without its entry.
- An entry without a row.

`Load` inserts rows table by table in `Tables` order, with column names checked against `pragma_table_info`. An unknown column is refused, naming table and column. A missing column takes its default. `import` runs these steps:
1. Takes the lock (refusing a running server the way `backup restore` does).
2. Refuses unless the current database has no projects and no tickets.
3. Creates `{data dir}/vaquero.import.db`, migrates it to head and opens the configured store. For SQLite storage that is the new file.
4. Puts each blob under the new instance id's prefix, and records each key put.
5. In one transaction, calls `Load`, sets `instance_id` (fresh) and `blob_store` (configured), and rebuilds both FTS tables with `INSERT INTO x(x) VALUES('rebuild')`.
6. Runs `integrity_check` and `foreign_key_check`.
7. Calls `swapDatabase`.
8. Removes `{data dir}/token`.
9. Prints "Imported N projects, N tickets and N attachments into {backend}. Profiles holding a token for the old instance need a new one: vaquero token create." (The wording is exact. Counts use real plurals.)

Any failure deletes the new file and every key put, and leaves the old database untouched.

- [ ] **Tests first:**
  - `TestRoundTrip`: seed, export, import into an empty data dir, export again with the same fixed clock and version. The archives are byte-equal. Run it for both `sqlite` and `dir` storage.
  - `TestImportRefusesNonEmpty`.
  - `TestImportRefusesNewerSchema`.
  - `TestImportRefusesUnknownColumn`.
  - `TestImportFailureLeavesOldDatabase`: corrupt one attachment entry. The old database still reads, no `vaquero.import.db` remains and the dir store is empty.
  - `TestImportRefusesRunningServer`.
  - `TestImportThenServeWithOtherBackendRefuses`.
  - `TestImportMintsNewToken`: after the next `serve`, the old token file's value no longer works and a new one does.
- [ ] Watch them fail, implement, gate, and commit `"Data: vaquero import into an empty instance"`.

### Task 9: Branch review and merge

- [ ] Run `make ci`.
- [ ] Run a whole-branch review on the most capable model, against the spec and this plan's Review Focus. Fix what it finds that is real. Move what is deferred to `docs/BACKLOG.md` under "Deferred from Phase 3b".
- [ ] Update CLAUDE.md's package map: `blobs`, `archive`, `coreseed`, the new routes, the commands, `Deps.Blobs` and `Deps.Instance`, the sweep and the migration line.
- [ ] Get the owner's yes, then merge in the repo's usual style.

### Task 10: The first release (v0.1.0)

These steps are outward-facing, so each needs the owner's yes when it happens.

- [ ] Remake the unreleased fixture. Delete `testdata/upgrade/v0.1.0.*`, run `make fixture VERSION=0.1.0` (the seed now includes attachments, and `scripts/fixture`'s `tables` gains `attachments` and `blobs`), and check that `TestFixturesMigrateToHead` passes. Commit `"Upgrades: remake the v0.1.0 fixture with attachments"`.
- [ ] Follow `docs/RELEASING.md` exactly: tag `v0.1.0` and run the manual `release` workflow. The owner confirms before the tag is pushed and before the workflow runs.
- [ ] Verify the release on the owner's machine:
  1. `brew install stazelabs/vaquero/vaquero`, then `brew services start vaquero`.
  2. `vaquero version` reads `v0.1.0`.
  3. Attach one screenshot through the web, then `vaquero export /tmp/x.tar.gz`.
  4. Report the results.

### Task 11: Move the backlog in

This runs only against the released v0.1.0 from Task 10.

**Files:** `scripts/backlog-import/main.go`, `docs/BACKLOG.md`, `CLAUDE.md`

- [ ] Write `scripts/backlog-import` to the spec's section 3 rules:
  - Open `- [ ]` lines become todo tickets, with the title cut at the first clause and the body carrying the whole line plus its section.
  - Each ticket gets one tag per section slug, and the Linear line gets `linear-import`.
  - Phase 4 to 7 lines become milestones. A line naming a phase joins that milestone.
  - The Phase 3 and 3b lines are not imported.

  With no flag the script prints the plan. With `--apply` it first runs `vaquero export` to `{data dir}/backups/before-backlog-{time}.tar.gz`, then creates everything through the CLI. A unit test feeds it a small markdown sample and checks the plan.
- [ ] Show the owner the printed plan. Apply it only on their yes.
- [ ] Replace `docs/BACKLOG.md` with the pointer text from the spec, keeping the ticked history below it, and tick the Phase 3 and 3b lines. Change CLAUDE.md's backlog sentence and the project memory note to say work is tracked in VAQ. Commit `"Backlog: moved into Vaquero"`.
