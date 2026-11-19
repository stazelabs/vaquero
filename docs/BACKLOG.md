# Backlog

Vaquero's own work list until a vaquero instance can track it (planned for Phase 3, when import arrives). Each line is a future ticket: when import exists, these move into vaquero and this file is deleted.

Format: `- [ ] Title — detail (source)`. Tick and keep a line when it ships, with the commit; prune ticked lines at the end of each phase.

## Phases

- [x] Phase 0: Foundation — merged a69c53a
- [x] Phase 1: Core tracker
  - [x] Phase 1a: schema, `internal/core`, events, plain pages, activity feed
  - [x] Phase 1b: drag-and-drop board, list view with filters, search, live updates, keyboard shortcuts
- [x] Phase 2: API, CLI, MCP, coordination
  - [x] Phase 2a: REST API, tokens, the CLI as an API client, admin section
  - [x] Phase 2b: agent identity, ready queue, claims with leases, MCP at `/mcp` and `vaquero mcp`, MCP activity — plan: docs/superpowers/plans/2026-11-19-phase-2b-agents.md
- [ ] Phase 3: Releases and upgrades (first public release; move this backlog into vaquero)
  - [ ] Linear importer — pick the format(s) to support when Phase 3 is planned; start from https://github.com/linear/linear/tree/master/packages/import. Keep project keys and ticket numbers so existing branch names and commit references still resolve. (Naming the source product is the one exception to "docs never name competing products": an importer has to say what it imports.)
- [ ] Phase 4: Hosted mode
- [ ] Phase 5: Git integration
- [ ] Phase 6: Notifications and attachments
- [ ] Phase 7: LLM assists

## Deferred from the Phase 0 review

- [x] Fix the stale migration-order line in `.claude/rules/database.md` (review #3) — Phase 1a
- [ ] Make `VERSION` match its comment — `git describe` gives `0.1.0-3-gabc` on untagged commits; decide release-or-dev semantics with the Phase 3 release work (review #5)
- [ ] Stop tightening an existing `VQ_DATA_DIR` to 0700 silently — `VQ_DATA_DIR=.` changes the working directory's permissions; decide refuse vs warn, and test it (review #6)
- [ ] Reject loopback addresses outside the Host allowlist — `VQ_ADDR=127.0.0.2` boots, then every request gets 403 (review #7)
- [ ] Validate the port (1–65535) and only say "is vaquero already running?" for address-in-use (review #8)
- [ ] Assert the bound socket is loopback after `net.Listen` (review #9)
- [ ] Release the signal handler once shutdown starts, so a second Ctrl-C exits at once (review #10)
- [ ] Test the non-loopback refusal through `run("serve")`, including the hosted-mode pointer in the message (review #11)
- [ ] Log Host-check refusals (review #12)
- [ ] Small items: validate `VQ_LOG_LEVEL`; log status 200 rather than 0 for empty responses; drop the duplicate `MkdirAll` in `openDB`; remove the dbtest template directory; stop serving `input.css` (review #13)
- [x] View the theme in dark mode and check contrast — viewed in Phase 1b by applying the dark tokens in Chrome; readable

## Deferred from Phase 1a

- [ ] Tag management: rename, merge and delete tags (tags are created inline only)
- [ ] Moving a ticket to another project (keys and numbers would change)
- [ ] Editing an existing milestone's description (the settings form edits name and due date only)
- [ ] A refused milestone or status form on the settings page loses the typed values (it redirects with a toast)
- [ ] Ticket history shows "changed the description" without the old text (events record that the body changed, not its contents)
- [x] Before Phase 2 exposes core: an empty `StatusID` on update silently moves the ticket — PatchTicket keeps unnamed fields (Phase 2a)
- [x] Before Phase 2: link direction rules — blocking loops and two-way duplicates refused (Phase 2a)
- [x] Before Phase 2: optimistic concurrency — ticket versions, 409 on a stale save (Phase 2a)
- [ ] Link history is one-sided: link events attach to the "from" ticket only, so a blocked ticket's history never says it became blocked (review 1a #10)
- [ ] `c.tx` should `defer tx.Rollback()` so a panic mid-operation can't hold the only writer connection (review 1a #4)
- [ ] `/p/vaq/new?parent=VAQ-1` loses its query string on the case redirect (`ticketFromURL` keeps it) (review 1a #6)
- [ ] Comments lose leading indentation (a comment starting with an indented code block) — trim trailing space and leading blank lines only (review 1a #7)
- [ ] Fix stale docs: the payload list in `internal/core/events.go`; `Deps.Q` mentioned in `.claude/rules/database.md` and CLAUDE.md (review 1a #11, #12)
- [ ] External images in markdown render broken (CSP `img-src 'self' data:`) — strip `<img>` or say so (review 1a #14)
- [ ] Destructive buttons (comment delete, status delete, archive) act on one click with no confirmation (review 1a #16)
- [ ] Test gaps: cross-site POST to a real mutating route with a no-write assertion; hostile markdown in a comment at page level; cross-project parent through the form (review 1a #15)
- [ ] Tag names: `école` and `École` are one tag in the input but two in the database (SQLite NOCASE folds ASCII only) (review 1a #1, related)

## Deferred from Phase 1b

- [ ] The milestone panel is hidden below laptop width (`max-lg:hidden`); small screens get no milestone filter except through the URL
- [ ] At narrow widths an open rail pushes the active columns into sideways scroll; consider overlaying it instead
- [ ] Reordering cards writes a "reordered" history line each time; consider folding repeated reorders or leaving them out of the activity page
- [ ] Keyboard shortcuts have no automated test (app.js is untested JavaScript); checked by hand in Chrome
- [ ] Drag-and-drop has no automated browser test; checked by hand in Chrome with scripted pointer events (the browser tool's own drag gesture doesn't trigger Sortable)
- [ ] Each open page polls the database once a second for the newest event; fine locally, revisit for hosted mode with many viewers (an in-process broadcast from core would replace polling)

- [ ] A change between a page render and its stream connecting (or during a reconnect after a restart) isn't announced — emit `id:` and honour `Last-Event-ID` (review 1b #4)
- [ ] A failed drop (500 or network error) leaves the card where it was dropped until the next change — refresh the board on `htmx:responseError`/`sendError` (review 1b #5)
- [ ] A drop's response can swap the board while the next card is being dragged — defer swaps of `#board` while dragging (review 1b #6)
- [ ] "N older" links to the whole list, not the done tickets — add a status or category filter (review 1b #7)
- [ ] Search: decomposed accents (NFD "résumé") find nothing — normalise to NFC; terms split across a ticket and its comments never match; bm25 from two tables compared directly; title hits repeat the title as snippet; results past 50 cut silently (review 1b #8, #9)
- [x] FTS tables key on implicit rowids — delete triggers added with project delete (0004_hard_delete.sql); VACUUM would still renumber rowids, so never VACUUM the live database in place (Snapshot uses VACUUM INTO a copy) (review 1b #10)
- [ ] `MoveTicket` reports any `GetStatus` failure as a bad status — map only `sql.ErrNoRows` (review 1b #11)
- [ ] Test gaps: stale before/after keys at the endpoint, the stream's ping, top-of-column drop into another column, renumber with a mid-column insert (review 1b #12)
- [ ] Accessibility: `aria-pressed` on chip links → `aria-current`; `role="group"` on the filters; `j`/`k` should move real focus, not just a ring (review 1b #13)
- [ ] Activity: "Nothing has happened yet." stays after the first live event (it sits outside `#feed`) (review 1b #14)
- [ ] Filter and search clipping cuts by bytes, not characters (review 1b #15)

## Deferred from the footer, key and delete work

- [ ] Restoring a pre-delete backup is manual (stop vaquero, copy `backups/vaquero-….sqlite` over `vaquero.db`); a `vaquero admin restore` command belongs with Phase 3's upgrade tooling
- [ ] The footer's commit reads "dev" under `make dev` (air builds without ldflags); release builds stamp it
- [ ] When restoring a backup by hand, remove `vaquero.db-wal` and `vaquero.db-shm` first, or a stale WAL is replayed onto the restored file
- [ ] Surviving tickets' history still names deleted or re-keyed tickets ("marked OTH-1 as blocking VAQ-1") with no "link removed" line (review fkd minor 2)
- [ ] `TestDeleteConfirmation` should compare the whole snapshot (`maps.Equal`); extend the delete test with a parent/child pair, a soft-deleted comment, `PRAGMA foreign_key_check` and the other project's event count (review fkd minor 3)
- [ ] Notice links for `/vN` module paths (chi v5, ulid v2, goose v3) 404 — link to pkg.go.dev instead (review fkd minor 4)
- [ ] The bundled versions in `scripts/notices` (htmx 2.0.4, SortableJS 1.15.6, Tailwind 4.3.5) aren't checked against the files; `package.json` allows Tailwind `^4.1.0` (review fkd minor 5)
- [ ] backup.go carries unused ports (`EffectiveKeep`, `KeepFor`, `SpaceLow`, `OpenBackup`, `ParseBackupName`) and startcase-specific comments — trim or use in Phase 3 (review fkd minor 6); Linux free space should use `Frsize` (minor 7)
- [ ] CLAUDE.md drift: route list lacks `/about/licenses`, `POST /p/{key}/key` and `/delete`; `Shell` still says "tagline footer"; static entry lacks `logo.svg`; `Deps` list lacks `BackupDir`; backups are `.sqlite`, not `.db` as the plan says (review fkd minor 8)
- [ ] The logo's colours are fixed in the SVG, so it doesn't follow the dark-mode accent (review fkd minor 9)
- [ ] When sessions arrive (Phase 4), don't port startcase's snapshot `scrub` as is: its `VACUUM` can renumber the rowids the search indexes key on — scrub, then rebuild the FTS tables

## Deferred from Phase 2a

- [ ] Per-token API rate limits (and on failed authentication) — with hosted mode, Phase 4
- [ ] `vaquero tickets --milestone NAME` — the API filters milestones by internal id; accept a name
- [ ] A JSON 500 for panics inside `/api/v1` (today Recover answers plain text)
- [ ] `vaquero stop` (stop a background local server) and `vaquero logs`
- [ ] Refreshing the tokens page right after creating a token offers to resubmit the form, creating another token

- [ ] Local mode trusts every process on the machine: any local program (another user's included) can reach the server and mint a token at `/admin/tokens`. Fine on a personal laptop; decide on a guard before shared machines or Phase 4 (review 2a, declined to judge)
- [ ] Review 2a minors: project writes answer `open_count: 0`; `--version` below 1 silently skips the check; exit 3 should key on the `conflict` code, not on `current` being present; sub-tickets in the API are partial summaries; `ticketDTO` loads the whole board for one `ready`; unknown `before`/`after` keys on move are ignored rather than refused; token revoke by prefix should compare exactly (base64 is case-sensitive); `profile add` takes the token in argv (accept `-`) and doesn't check the URL; `.tmp` files keep an old mode; `ensureLocalToken` doesn't check the token's actor or revoke old "local" tokens; a lock pid of 0 reads "(pid 0)"; the loop walk caps nodes, not depth; name lookups report database errors as 422; "dev" builds count as 0.0.0 and the upgrade message repeats per request; `vaquero.log` grows without limit; create ignores `version`/`remove_tags`; docs: CLAUDE.md `Deps` lacks `Touch`/`RenewLocalToken`, handlers.md "no writer queries" vs `Deps.Touch` (extend the guard test to ban `d.Touch.`), the token-create exception to redirect-after-POST, and `VAQUERO_AGENT` is undocumented

## Icons and in-app help (requested 2026-11-19)

- [x] Lucide icons through the UI; in-app help at `/help` (with markdown for agents and `vaquero docs`) — plan: docs/superpowers/plans/2026-11-19-icons-and-help.md
- [ ] Review (icons and help) minors: the icon generator should fail on an SVG element it can't parse rather than drop it; `TestUIGlyphsAreDrawingOnly` should assert the allowlist shape; `icon.templ` says icon-only buttons use `IconLg` but the toast close uses `Icon`; `TestIconOnlyButtonsLabelled` misses an icon wrapped in a span; a shortcut added to app.js but not to `templates.shortcuts` escapes the docs test; CLI subcommands and flags have no docs guard; the Activity empty state sits outside the live-swapped feed, so it stays after the first event
- [ ] Help: search the articles from the header search box

## Folded into Phase 2b

- [x] Exit 3 keys on the `conflict` code, not on `current` being present
- [x] `--version` below 1 is a usage error, not a skipped check

## Phase 2b review minors

- [ ] `Vaquero-Agent` requests open a writer transaction for `EnsureAgent` every time; look the agent up through the reader first
- [ ] `vaquero mcp`: a stdin line over 4 MB ends the bridge without a JSON-RPC error; an in-flight request finishes before EOF is noticed
- [ ] `/admin/mcp` filters by actor name, so an agent named "Owner" merges with the human Owner; agent names are case-sensitive
- [ ] A `Vaquero-Agent` header on a non-owner token is ignored without a word (`vaquero mcp --agent` should say so)
- [ ] Decide: claims are advisory for edits (anyone can update or move a claimed ticket); revisit if agents need hard locks

## Admin section (requested 2026-11-19)

- [x] Phase 2a: `/admin` home (version, data directory, database size, schema version) and `/admin/tokens`
- [x] Phase 2b: `/admin/mcp` — log and show every MCP tool call (time, agent, tool, argument summary, outcome, duration), filterable, live
- [ ] Later: backups (list, prune, restore), API call log, users and roles (Phase 4), system details (disk space, uptime)

## Open questions

- [x] Prevent two servers sharing one data directory — `vaquero.lock` (Phase 2a)
- [ ] CI triggers once outside contributors send pull requests (the repo is private and workflows are manual for now)
- [ ] Check the name `vaquero` for collisions on Homebrew and the container registry before the first public release
- [ ] Whether local mode should ever offer a LAN bind with sign-in (current answer: use hosted mode)
- [ ] Make the repository public — planned around the first release in Phase 3
