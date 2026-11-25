# Debt sweep, batches A to C: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close 23 small, well-scoped VAQ tickets left over from earlier reviews: boot and config hardening (A), core correctness (B) and web fixes (C).

**Architecture:** No new subsystems. Each change is the smallest fix the ticket describes, test-first, inside the package that already owns the behaviour.

**Tech Stack:** As the repo: Go 1.27, templ, htmx, app.js, SQLite (modernc).

**Spec:** each ticket's text is the spec. Read it with `vaquero ticket show VAQ-N --agent claude` (the owner's real instance; read only, and never create, edit or move tickets from a subagent: the controller does ticket bookkeeping). The ticket texts are also copied below. CLAUDE.md and `.claude/rules/*.md` bind every task.

## Global Constraints

- CLAUDE.md hard invariants. In particular: only `internal/core` writes the tracker, with one event per change; CSP has no inline script and no `style=`, so behaviour lives in `app.js`; templates use theme tokens only; generated files are committed; a new command, shortcut or route needs its help-article line; copy follows `.claude/rules/copy-voice.md`.
- No schema migrations in this sweep (v0.1.0 and v0.1.1 are released; a migration would need its own release fixture). If a ticket seems to need one, stop and report it.
- Never point a dev build, a test or a server at the owner's data directory or port 7878. Tests use temp dirs; manual checks use `VQ_DATA_DIR=$(mktemp -d)` and a port such as 7881.
- Gate, each step separately by exit code: `make gen`, `make fmt`, `make vet`, `make lint`, `make test`; then `make ci`. Commits are `{Area}: {summary} (VAQ-N)` ending with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. One commit per ticket where practical.

## Review Focus

1. **A config that boots then refuses every request** (VAQ-10): `VQ_ADDR=127.0.0.2:7878` must be refused at boot with a message naming the allowed hosts, never accepted and then answered 403.
2. **Permissions on an existing data directory** (VAQ-9): a directory such as the working directory, or one shared with other tools, keeps its permissions. A warning says what to do, and a new directory is still created 0700.
3. **A second Ctrl-C during the 30 s drain** (VAQ-13) exits at once, without corrupting the database: the WAL is left for SQLite to recover, never half-checkpointed.
4. **The live stream after a server restart** (VAQ-36): a page open across a restart catches up on changes made while it was disconnected, with no duplicate refreshes.
5. **Tag folding for accented names** (VAQ-29): `École` and `école` resolve to one tag, NFC and NFD spellings too, and an existing tag's spelling is kept.

## Rulings (controller, 2026-11-25; the owner can overrule)

- **R1 (VAQ-9):** never chmod an existing directory. Create a missing one 0700. If an existing one grants group or other any access, log a warning naming the mode and `chmod 700 {dir}`. The token file stays 0600.
- **R2 (VAQ-29):** match tags Unicode-case-insensitively in core (NFC-normalise, then `strings.EqualFold` or `cases.Fold` against the project's tags), with no migration. New tags are stored NFC. Existing duplicates are left as they are.
- **R3 (VAQ-59):** the new token still shows once, on the POST answer (security rule). That page marks itself, and `app.js` calls `history.replaceState` so a refresh is a GET, not a resubmit.
- **R4 (VAQ-43):** `j`/`k` move real focus (`focus()` on the card's link), keeping the visible ring.
- **R5 (VAQ-11):** port 0 is refused too, because local mode needs a fixed, known port.

---

### Task 1: Batch A, boot and config hardening

**Tickets:** VAQ-9, 10, 11, 12, 13, 14, 15, 16.

**Files (expected):** `internal/config/config.go` (+test), `cmd/vaquero/serve.go`, `cmd/vaquero/main.go` (+tests), `internal/web/middleware/hostcheck.go` and `requestlog.go` (+tests), `internal/db/dbtest`, `internal/web/static.go`.

Ticket texts:
- VAQ-9: Stop tightening an existing `VQ_DATA_DIR` to 0700 silently. `VQ_DATA_DIR=.` changes the working directory's permissions; decide refuse or warn, and test it. Ruling R1.
- VAQ-10: Reject loopback addresses outside the Host allowlist. `VQ_ADDR=127.0.0.2` boots, then every request gets 403.
- VAQ-11: Validate the port (1–65535, ruling R5), and only say "is vaquero already running?" for address-in-use.
- VAQ-12: Assert the bound socket is loopback after `net.Listen`.
- VAQ-13: Release the signal handler once shutdown starts, so a second Ctrl-C exits at once.
- VAQ-14: Test the non-loopback refusal through `run("serve")`, including the hosted-mode pointer in the message.
- VAQ-15: Log Host-check refusals (Host value cut to 80 characters, no other headers).
- VAQ-16: Small items: validate `VQ_LOG_LEVEL`; log status 200 rather than 0 for empty responses; drop the duplicate `MkdirAll` in `openDB`; remove the dbtest template directory (if one still exists; say so if not); stop serving `input.css`.

- [ ] For each ticket: write a failing test (name it after the behaviour), watch it fail, fix, watch it pass, commit `{Area}: … (VAQ-N)`.
- [ ] Gate and `make ci`.

### Task 2: Batch B, core correctness

**Tickets:** VAQ-22, 23, 25, 29, 41.

**Files (expected):** `internal/core/{links.go,core.go,comments.go,text.go,tickets.go,move.go}` (+tests), `internal/web/templates/activity.go` (history wording, if VAQ-22 needs it).

Ticket texts:
- VAQ-22: Link history is one-sided: link events attach to the "from" ticket only, so a blocked ticket's history never says it became blocked. Keep exactly one event per operation (the invariant). Make the ticket page's history and the activity line read correctly on both tickets: the history query includes link events where the ticket is the "to" side (by payload), and the wording takes the reader's side ("VAQ-3 now blocks this ticket"). No new event rows.
- VAQ-23: `c.tx` should `defer tx.Rollback()` so a panic mid-operation can't hold the only writer connection. Test: a panicking operation, recovered by the test, leaves the writer usable.
- VAQ-25: Comments lose leading indentation (a comment starting with an indented code block). Trim trailing space and leading blank lines only. Check whether ticket bodies share the problem, and fix both if so.
- VAQ-29: Tag names `école` and `École` are one tag in the input but two in the database (SQLite NOCASE folds ASCII only). Ruling R2.
- VAQ-41: `MoveTicket` reports any `GetStatus` failure as a bad status. Map only `sql.ErrNoRows` to the field error; anything else is a plain error.

- [ ] Test-first per ticket, commit per ticket. `TestEveryOperationEmitsOneEventOrNothing` must still pass.
- [ ] Gate and `make ci`.

### Task 3: Batch C, web fixes

**Tickets:** VAQ-20, 24, 36, 37, 38, 39, 43, 44, 45, 59.

**Files (expected):** `internal/web/handlers/{projects.go,tickets.go,stream.go,board.go,activity.go,admin.go,misc.go}` (+tests), `internal/web/templates/*.templ` (+ generated), `internal/board`, `internal/web/static/js/app.js`, help articles if a behaviour they describe changes.

Ticket texts:
- VAQ-20: A refused milestone or status form on the settings page loses the typed values (it redirects with a toast). Re-render the settings page with 422, the field errors and the typed values, the way the ticket form does.
- VAQ-24: `/p/vaq/new?parent=VAQ-1` loses its query string on the case redirect (`ticketFromURL` keeps it).
- VAQ-36: A change between a page render and its stream connecting (or during a reconnect after a restart) isn't announced. Emit `id:` (the event sequence) and honour `Last-Event-ID`. The page also carries the sequence it was rendered at, so the first connection catches up.
- VAQ-37: A failed drop (500 or network error) leaves the card where it was dropped until the next change. Refresh the board on `htmx:responseError` and `htmx:sendError`, with an error toast.
- VAQ-38: A drop's response can swap the board while the next card is being dragged. Defer swaps of `#board` while dragging, then apply the latest.
- VAQ-39: "N older" links to the whole list, not the done tickets. Add a status or category filter to the list view (`?category=done`), with a chip, and link to it.
- VAQ-43: Accessibility: `aria-pressed` on chip links becomes `aria-current`; `role="group"` with a label on the filters; `j`/`k` move real focus (ruling R4).
- VAQ-44: Activity: "Nothing has happened yet." stays after the first live event (it sits outside `#feed`). Move it inside the live part.
- VAQ-45: Filter and search clipping cuts by bytes, not characters. Cut on runes (use `units` or a small helper), and test with accented and emoji input.
- VAQ-59: Refreshing the tokens page right after creating a token offers to resubmit the form, creating another token. Ruling R3.

- [ ] Test-first per ticket where Go can test it (handlers through `webtest`). For app.js changes (VAQ-37, 38, 43, 59), check by hand in Chrome against a throwaway server (`VQ_DATA_DIR=$(mktemp -d) VQ_ADDR=127.0.0.1:7881`), and record what was checked in the commit body. If the browser tools are unavailable, say so and list the checks still to do.
- [ ] Gate and `make ci`.
