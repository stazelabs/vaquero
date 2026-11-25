# Backlog

Work is tracked in the VAQ project of Vaquero itself, from v0.1.0 on (moved 2026-11-25 by `scripts/backlog-import`; the undo point is an export in the owner's `backups/`). The Direction section is the VAQ project's description; every open line became a VAQ ticket tagged by its section, and Phases 4 to 7 became milestones.

This file keeps the record of what shipped up to Phase 3b. Don't add to it: file a VAQ ticket instead.

## Phases

- [x] Phase 0: Foundation — merged a69c53a
- [x] Phase 1: Core tracker
  - [x] Phase 1a: schema, `internal/core`, events, plain pages, activity feed
  - [x] Phase 1b: drag-and-drop board, list view with filters, search, live updates, keyboard shortcuts
- [x] Phase 2: API, CLI, MCP, coordination
  - [x] Phase 2a: REST API, tokens, the CLI as an API client, admin section
  - [x] Phase 2b: agent identity, ready queue, claims with leases, MCP at `/mcp` and `vaquero mcp`, MCP activity — plan: docs/superpowers/plans/2026-11-19-phase-2b-agents.md
- [x] Phase 3: Releases and upgrades (first public release; move this backlog into vaquero) — v0.1.0, 2026-11-25
  - [x] Phase 3a: snapshot before migrating, `stop`, `backup list|restore`, upgrade fixtures, restart of a stale local server, goreleaser and the Homebrew formula from a manual workflow — plan: docs/superpowers/plans/2026-11-20-phase-3a-releases-upgrades.md
  - [x] Phase 3b: export and import, attachments (SQLite by default, or a directory or S3), the first release (docs/RELEASING.md), then move this backlog into the released vaquero. Design: docs/superpowers/specs/2026-11-20-phase-3b-design.md — v0.1.0 released 2026-11-25; backlog moved into VAQ

## Deferred from the Phase 0 review

- [x] Fix the stale migration-order line in `.claude/rules/database.md` (review #3) — Phase 1a
- [x] Make `VERSION` match its comment — the exact tag on HEAD, else 0.0.0-dev (Phase 3a); was: — `git describe` gives `0.1.0-3-gabc` on untagged commits; decide release-or-dev semantics with the Phase 3 release work (review #5)
- [x] View the theme in dark mode and check contrast — viewed in Phase 1b by applying the dark tokens in Chrome; readable

## Deferred from Phase 1a

- [x] Before Phase 2 exposes core: an empty `StatusID` on update silently moves the ticket — PatchTicket keeps unnamed fields (Phase 2a)
- [x] Before Phase 2: link direction rules — blocking loops and two-way duplicates refused (Phase 2a)
- [x] Before Phase 2: optimistic concurrency — ticket versions, 409 on a stale save (Phase 2a)
- [x] External images in markdown render broken (CSP `img-src 'self' data:`) — strip `<img>` or say so (review 1a #14) — Attachments: upload, serve, sweep orphans; markdown images only from attachments (Phase 3b): only `/attachments/` images render, any other becomes a link

## Deferred from Phase 1b

- [x] FTS tables key on implicit rowids — delete triggers added with project delete (0004_hard_delete.sql); VACUUM would still renumber rowids, so never VACUUM the live database in place (Snapshot uses VACUUM INTO a copy) (review 1b #10)

## Deferred from the footer, key and delete work

- [x] Restoring a pre-delete backup is manual — `vaquero backup restore` (Phase 3a); was: (stop vaquero, copy `backups/vaquero-….sqlite` over `vaquero.db`); a `vaquero admin restore` command belongs with Phase 3's upgrade tooling
- [x] (restore removes them, Phase 3a) When restoring a backup by hand, remove `vaquero.db-wal` and `vaquero.db-shm` first, or a stale WAL is replayed onto the restored file

## Deferred from Phase 2a

- [x] `vaquero stop` (Phase 3a)

## Icons and in-app help (requested 2026-11-19)

- [x] Lucide icons through the UI; in-app help at `/help` (with markdown for agents and `vaquero docs`) — plan: docs/superpowers/plans/2026-11-19-icons-and-help.md

## Folded into Phase 2b

- [x] Exit 3 keys on the `conflict` code, not on `current` being present
- [x] `--version` below 1 is a usage error, not a skipped check

## Admin section (requested 2026-11-19)

- [x] Phase 2a: `/admin` home (version, data directory, database size, schema version) and `/admin/tokens`
- [x] Phase 2b: `/admin/mcp` — log and show every MCP tool call (time, agent, tool, argument summary, outcome, duration), filterable, live

## Open questions

- [x] Prevent two servers sharing one data directory — `vaquero.lock` (Phase 2a)
- [x] Make the repository public — done with the v0.1.0 release (2026-11-25)
