# Debt sweep, batches D to F: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close 34 more small VAQ tickets: API, CLI and MCP (D), attachment and export follow-ups (E1, E2), and docs and test chores (F).

**Architecture:** No new subsystems. Each change is the smallest fix the ticket describes, test-first, in the package that owns the behaviour.

**Tech Stack:** As the repo.

**Spec:** each ticket's text is the spec. Read it with `vaquero ticket show VAQ-N --agent claude` (the owner's real instance, read only). Subagents never create, edit, comment on or move tickets: the controller does ticket bookkeeping. The first line of each ticket is copied below. Batches A to C (plan `2026-11-25-debt-sweep-a-c.md`) are merged.

## Global Constraints

- CLAUDE.md hard invariants. Only `internal/core` writes the tracker, with one event per change. CSP has no inline script and no `style=`. Templates use theme tokens only. Generated files are committed. A new command, shortcut or route needs its help-article line. Copy follows `.claude/rules/copy-voice.md`. The API is additive since release: never rename or remove a field, and never refuse input that used to be accepted, unless a ticket asks for it.
- No schema migrations. If a ticket seems to need one, stop and report it.
- **CLAUDE.md and `.claude/rules/*.md` are edited only by the controller.** When a ticket needs either changed, write the change as a patch file in the run's workspace (`claude-md-task-N.patch`), say so in the report, and leave the files alone.
- Never point a dev build, a test or a server at the owner's data directory or port 7878. Use temp dirs, and port 7881 for manual checks.
- Gate, each step separately by exit code: `make gen`, `make fmt`, `make vet`, `make lint`, `make test`; then `make ci`. Commits are `{Area}: {summary} (VAQ-N)` ending with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Make one commit per ticket where practical.
- Dependency versions match startcase's `go.mod`. Add no new dependency.

## Review Focus

1. **An agent with a misspelled or differently-cased name** (VAQ-64, 66, 67): the agent is still never merged with the human Owner, and a warning says when a header is ignored.
2. **A panic inside an API handler or MCP tool** (VAQ-57): the answer is the API's JSON 500 envelope, never plain text, and the panic is logged once.
3. **A 10 MB upload to S3 over a slow link** (VAQ-90): the deadline scales with the size, so a legitimate upload isn't cut off at 10 s.
4. **Ticket pages on a large history** (VAQ-96): the history query uses an index (shown by `EXPLAIN QUERY PLAN` in a test).
5. **Bundled assets drifting from their recorded versions** (VAQ-50): a test fails when the notices' versions don't match the vendored files.

## Rulings (controller, 2026-11-25; the owner can overrule)

- **R1 (VAQ-56):** the API's ticket list accepts a milestone by name as well as by id (additive), resolved per project like status and assignee, with a 422 for an unknown name. The CLI's `--milestone` passes the name.
- **R2 (VAQ-66):** `/admin/mcp` filters by actor id, not name. Agent names stay case-sensitive. Changing identity rules is out of scope; the help says so.
- **R3 (VAQ-67):** `vaquero mcp --agent X` with a profile whose token isn't the owner's prints one warning to stderr ("--agent is ignored: this profile's token isn't the local owner's"). stdout stays JSON-RPC only.
- **R4 (VAQ-81):** the canonical attachment-name redirect becomes 302 (Found), not 301, so a future rename can't be shadowed by a cached redirect.
- **R5 (VAQ-84):** make `ExportedSettings` and `InstanceSettings` unexported, with read-only accessors. Tests use a setter that restores the value on cleanup.
- **R6 (VAQ-90):** the put deadline is 10 s plus 1 s per 256 KiB (about 50 s for 10 MB), computed by one function in `internal/blobs`.
- **R7 (VAQ-89):** import spools into `{data dir}/import-spool-…`, removed on close, with the free-space check `db.FreeBytes` against the archive's total attachment size before spooling.
- **R8 (VAQ-46):** `make dev` passes the same `-ldflags -X` values as `make build`, so the footer shows the commit.
- **R9 (VAQ-72):** do the listed 3a minors except "workflow actions pinned by tag, not SHA". Changing the release workflow needs a release run to verify, so it stays on the ticket. Say so in the report.
- **R10 (VAQ-73):** the pre-migration snapshot is never pruned while it's the only one for its schema version. Name-prefix the kinds (`premigrate-`, `predelete-`, `prerestore-`, `preimport-`) if needed, keeping `BackupKeep` per kind. Old names must still list and restore.
- **R11 (VAQ-101 third item):** CLI commands read only the settings they use, so a bad `VQ_LOG_LEVEL` doesn't stop `vaquero tickets`. Only `serve` validates server-only settings.

---

### Task 1: Batch D, API, CLI and MCP

**Tickets:** VAQ-56, 57, 64, 65, 66, 67, 100.

- VAQ-56: `vaquero tickets --milestone NAME`: the API filters milestones by internal id; accept a name (R1).
- VAQ-57: a JSON 500 for panics inside `/api/v1` (today Recover answers plain text). Cover `/mcp` too.
- VAQ-64: `Vaquero-Agent` requests open a writer transaction for `EnsureAgent` every time; look the agent up through the reader first.
- VAQ-65: `vaquero mcp`: a stdin line over 4 MB ends the bridge without a JSON-RPC error, and an in-flight request finishes before EOF is noticed. Answer an oversized line with a JSON-RPC error (id null, if it can't be parsed) and keep reading. On EOF, wait for in-flight requests to finish.
- VAQ-66: `/admin/mcp` filters by actor name, so an agent named "Owner" merges with the human Owner; agent names are case-sensitive (R2).
- VAQ-67: a `Vaquero-Agent` header on a non-owner token is ignored without a word (R3).
- VAQ-100: API name refusals answer the generic "Some names don't match anything." Make the top-level message name the field when there's exactly one. Check that MCP tool errors carry the field messages, and add them if they don't.

- [ ] Test-first per ticket, commit per ticket. Gate and `make ci`.

### Task 2: Batch E1, attachments and storage

**Tickets:** VAQ-79, 80, 81, 82, 83, 84, 90.

- VAQ-79: redact `Config.S3SecretAccessKey` under `%v`: give it a type with `String`, `LogValue` and `Format`.
- VAQ-80: make the AddAttachment and DeleteAttachment entries in `TestEveryOperationRollsBackPartWay` meaningful; today they fail at setup under the ghost actor.
- VAQ-81: canonical attachment redirects are 301 (R4).
- VAQ-82: plain-text 413 fallback in bodylimit.go for non-upload routes: answer the API envelope on `/api/v1` and `/mcp`, and a plain page elsewhere, never bare text.
- VAQ-83: API tests for delete permissions (an agent deletes its own upload, an agent can't delete the owner's, the owner deletes an agent's) and for HEAD on `/api/v1/attachments/{id}`, plus an api.md note on the 422 for a non-uploader delete.
- VAQ-84: `ExportedSettings` and `InstanceSettings` are mutable exported vars (R5).
- VAQ-90: `blobs.PutDeadline` of 10 s is tight for 10 MB to S3 (R6).

- [ ] Test-first per ticket, commit per ticket. Gate and `make ci`.

### Task 3: Batch E2, export, import and follow-ups

**Tickets:** VAQ-85, 86, 88, 89, 96, 99, 101.

- VAQ-85: HEAD `/api/v1/export` builds export.json then discards it, and export reads every object twice; use a size check or a store Stat for the pre-check (add `Stat` to `blobs.Store` if needed, implemented by all three backends and covered by `blobtest.Run`).
- VAQ-86: S3 bad credentials surface only at the first Put during import: check access (a List of the instance prefix, or a Put and Delete of a probe key) before touching anything.
- VAQ-88: `swapFailed` wording should mention the -wal and -shm files, and the gzip drain is unbounded (cap it with a LimitReader and refuse trailing data beyond, say, 1 MiB).
- VAQ-89: import spools into `os.TempDir()` (R7).
- VAQ-96: the ticket history query scans the whole events table. Rewrite `ListTicketEvents` as a `UNION ALL` of two indexed lookups, with an `EXPLAIN QUERY PLAN` test that neither part scans `events`.
- VAQ-99: overlapping failed drops put back only the last card: key the remembered drop by request. Check by hand in Chrome on :7881.
- VAQ-101: an unknown-tag filter test; `FormSeq` computed lazily on re-render only; CLI commands don't validate server-only settings (R11).

- [ ] Test-first per ticket, commit per ticket. Gate and `make ci`.

### Task 4: Batch F, docs and test chores

**Tickets:** VAQ-26, 28, 42, 46, 48, 49, 50, 51, 52, 62, 71, 72, 73.

- VAQ-26: fix stale docs: the payload list in `internal/core/events.go`; `Deps.Q` mentioned in `.claude/rules/database.md` and CLAUDE.md (rules and CLAUDE.md as a patch for the controller).
- VAQ-28: test gaps: a cross-site POST to a real mutating route with a no-write assertion; hostile markdown in a comment at page level; a cross-project parent through the form.
- VAQ-42: test gaps: stale before/after keys at the move endpoint, the stream's ping, a top-of-column drop into another column, a renumber with a mid-column insert.
- VAQ-46: the footer's commit reads "dev" under `make dev` (R8).
- VAQ-48: `TestDeleteConfirmation` should compare the whole snapshot (`maps.Equal`); extend the delete test with a parent/child pair, a soft-deleted comment, `PRAGMA foreign_key_check` and the other project's event count.
- VAQ-49: notice links for `/vN` module paths (chi v5, ulid v2, goose v3) 404; link to pkg.go.dev instead.
- VAQ-50: the bundled versions in `scripts/notices` (htmx, SortableJS, Tailwind) aren't checked against the files, and `package.json` allows Tailwind `^4.1.0` (pin it exactly).
- VAQ-51: backup.go carries unused ports and startcase-specific comments (trim them; keep anything Task 4's VAQ-73 uses), and Linux free space should use `Frsize`.
- VAQ-52: CLAUDE.md drift (route list, Shell's "tagline footer", logo.svg, `Deps.BackupDir`, `.sqlite`). Patch for the controller; check each item still applies.
- VAQ-62: icons and help review minors, each item. The Activity empty state is already fixed (VAQ-44); say so.
- VAQ-71: no test holds "never add push or pull_request triggers" for `.github/workflows`. Add one that parses each workflow's `on:`.
- VAQ-72: review 3a minors (R9).
- VAQ-73: seven project deletes after an upgrade can prune its pre-migration snapshot (R10).

- [ ] Test-first where there's behaviour; commit per ticket. Gate and `make ci`.
