# Unattended run: release plumbing, migration release, polish — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Close the remaining mechanical VAQ tickets: release plumbing (G), the deferred database work in one migration (H), and leftover polish (I).

**Spec:** each ticket's text is the spec. Read it with `vaquero ticket show VAQ-N --agent claude` (read only; subagents never create, edit or move tickets). Rulings below settle what the tickets leave open. v0.1.2 is released; this becomes v0.1.3.

## Global Constraints

- CLAUDE.md hard invariants. The API is additive. Copy follows copy-voice. New commands, routes and shortcuts need their help-article line.
- **Exactly one new migration, `0008_*`, in Task 2.** No other task adds one. Released fixtures (`testdata/upgrade/v0.1.0`, `v0.1.1`, `v0.1.2`) are never edited; `TestFixturesMigrateToHead` must pass, so the migration has to cope with each of them.
- **Workflows:** never add `push`, `pull_request` or `pull_request_target` triggers (a test holds it). Any workflow change is verified only by the next release run, so keep changes minimal and say exactly what to check on that run.
- CLAUDE.md and `.claude/rules/*` are edited only by the controller. Subagents write a patch file `claude-md-task-N.patch` in the run workspace.
- Never touch port 7878 or `~/Library/Application Support/vaquero`. Use temp dirs, and port 7881 for manual checks.
- Gate, each step separately by exit code: `make gen`, `make fmt`, `make vet`, `make lint`, `make test`; then `make ci`. Commits `{Area}: {summary} (VAQ-N)` with the Co-Authored-By line. Dependency versions match startcase; add no new Go dependency.

## Review Focus

1. **The next release still produces a working Homebrew formula with a service block** (VAQ-70): the rendered formula is tested against a golden file, with `brew services` support, both architectures for darwin and linux, and checksums taken from `checksums.txt`.
2. **Every released database migrates** (VAQ-96/97): v0.1.0 through v0.1.2 fixtures pass, the backfill never assigns a link event to the wrong ticket when the link row still exists, and the history query uses an index (EXPLAIN QUERY PLAN test).
3. **Compaction is all or nothing** (VAQ-91): a snapshot first, the server stopped, search rebuilt, integrity checked before the swap, and the old file kept on any failure.
4. **`vaquero stop` and upgrades under brew services** (VAQ-112): a custom `VQ_DATA_DIR` instance is never treated as the brew-managed one; the default-dir instance still defers to brew.
5. **No secret or token ever appears in logs or the formula** (VAQ-70, 98).

## Rulings (controller, 2026-11-25; the owner can overrule)

- **R1 (VAQ-70, VAQ-116 — the same issue):** drop goreleaser's `brews`. Keep a formula template in the repo (`packaging/homebrew/vaquero.rb.tmpl`, with the `service` block). A small Go program (`scripts/formula`) renders it from the version and `dist/checksums.txt`, with a golden-file test. The release workflow, after goreleaser, renders it and pushes `Formula/vaquero.rb` to `stazelabs/homebrew-vaquero` with `HOMEBREW_TAP_TOKEN` through the GitHub contents API (`gh api`). VAQ-116 is closed as a duplicate.
- **R2 (VAQ-93):** bump `goreleaser/goreleaser-action` to the newest major that runs on Node 24 (check its releases with `gh api`); if none, say so and leave it.
- **R3 (VAQ-112, SHA pinning):** pin every third-party action in `.github/workflows/*` to a full commit SHA with the tag in a trailing comment (resolve with `gh api repos/{owner}/{repo}/git/ref/tags/{tag}`, dereferencing annotated tags). A test fails on an action referenced by tag or branch.
- **R4 (VAQ-112, brew detection):** `brewService()` is true only when the service file exists and the data directory in use is the default one (the formula sets no `VQ_DATA_DIR`). Rework the three brew tests to match.
- **R5 (VAQ-96, VAQ-97):** migration `0008` adds a partial expression index on `json_extract(payload,'$.to_id')` for link events, and backfills `to_id` into link events that lack it: first from the link row (`payload.link_id` → `ticket_links.to_ticket_id`), else by resolving the payload's `to` key only if it resolves to a ticket created before the event. Unresolvable events keep no `to_id`. `ListTicketEvents` then matches link events by `to_id` only (no key fallback), as a `UNION ALL` of two indexed lookups, with an EXPLAIN QUERY PLAN test that nothing scans `events`. A migration writing `events` is allowed (migrations are the one place besides core and import).
- **R6 (VAQ-91):** add `vaquero backup compact` (offline, like restore): take the maintenance lock, snapshot (kind `precompact-`), `VACUUM INTO` a new file, rebuild both FTS indexes in it, `integrity_check` and `foreign_key_check`, then `swapDatabase`. Never VACUUM the live file in place. Help and CLI article lines.
- **R7 (VAQ-78):** record a location fingerprint in settings next to `blob_store` (`blob_location`: the absolute dir for `dir`, `endpoint/bucket` for `s3`, empty for `sqlite`) when the first attachment is stored, and refuse at boot when attachments exist and it differs, with the same style of message. A settings row, no migration. Back-compat: an absent `blob_location` is recorded on the next boot, not refused.
- **R8 (VAQ-87):** before import puts any blob, write `{data dir}/import.pending` naming the new instance id and backend; remove it on success or after cleanup. On the next `import` or `serve`, if the file exists, delete that instance's prefix from the named backend (best effort, logged) and remove the file.
- **R9 (VAQ-61):** do each listed item that's still true (check first; several may be fixed already), each with a test where there's behaviour. "`vaquero.log` grows without limit": rotate at 10 MB keeping one old file. "Dev builds count as 0.0.0": the upgrade message shows once per process, not per request. Doc items go in the controller patch.

---

### Task 1: Batch G, release plumbing

**Tickets:** VAQ-70, VAQ-116, VAQ-93, VAQ-112 (rulings R1–R4).

- [ ] Test-first where there's behaviour (formula golden test, workflow pin test, brew detection tests). Commit per ticket. Gate and `make ci`. List in the report exactly what the next release run must show (formula pushed with the service block, actions resolved by SHA, no Node 20 warning).

### Task 2: Batch H, the migration release

**Tickets:** VAQ-96, VAQ-97, VAQ-91 (rulings R5, R6).

- [ ] Migration `0008` with tests against every released fixture, the EXPLAIN QUERY PLAN test, and backfill cases (link row present, link deleted with the key resolvable, key reused by a later ticket → no `to_id`). Then `backup compact` with its all-or-nothing tests. Commit per ticket. Gate and `make ci`.

### Task 3: Batch I, polish

**Tickets:** VAQ-98, VAQ-108, VAQ-113, VAQ-61, VAQ-78, VAQ-87 (rulings R7–R9). Read each full ticket; several list multiple items.

- [ ] Test-first per item where there's behaviour; commit per ticket. Gate and `make ci`.
