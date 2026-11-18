# Backlog

Vaquero's own work list until a vaquero instance can track it (planned for Phase 3, when import arrives). Each line is a future ticket: when import exists, these move into vaquero and this file is deleted.

Format: `- [ ] Title — detail (source)`. Tick and keep a line when it ships, with the commit; prune ticked lines at the end of each phase.

## Phases

- [x] Phase 0: Foundation — merged a69c53a
- [ ] Phase 1: Core tracker
  - [x] Phase 1a: schema, `internal/core`, events, plain pages, activity feed
  - [ ] Phase 1b: drag-and-drop board, list view with filters, search, live updates, keyboard shortcuts
- [ ] Phase 2: API, CLI, MCP, coordination
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
- [ ] View the theme in dark mode and check contrast — only verified by stylesheet inspection so far

## Deferred from Phase 1a

- [ ] Tag management: rename, merge and delete tags (tags are created inline only)
- [ ] Moving a ticket to another project (keys and numbers would change)
- [ ] Editing an existing milestone's description (the settings form edits name and due date only)
- [ ] A refused milestone or status form on the settings page loses the typed values (it redirects with a toast)
- [ ] Ticket history shows "changed the description" without the old text (events record that the body changed, not its contents)
- [ ] Before Phase 2 exposes core: an empty `StatusID` on update silently moves the ticket to the first todo status — make empty mean "keep" or refuse it (review 1a #5)
- [ ] Before Phase 2: decide link direction rules — "A blocks B" plus "B blocks A", and duplicates both ways, are accepted today (review 1a #8)
- [ ] Before Phase 2: optimistic concurrency (an `updated_at` check) for the whole-ticket edit, so two agents don't silently overwrite each other (review 1a recommendation)
- [ ] Link history is one-sided: link events attach to the "from" ticket only, so a blocked ticket's history never says it became blocked (review 1a #10)
- [ ] `c.tx` should `defer tx.Rollback()` so a panic mid-operation can't hold the only writer connection (review 1a #4)
- [ ] `/p/vaq/new?parent=VAQ-1` loses its query string on the case redirect (`ticketFromURL` keeps it) (review 1a #6)
- [ ] Comments lose leading indentation (a comment starting with an indented code block) — trim trailing space and leading blank lines only (review 1a #7)
- [ ] Fix stale docs: the payload list in `internal/core/events.go`; `Deps.Q` mentioned in `.claude/rules/database.md` and CLAUDE.md (review 1a #11, #12)
- [ ] External images in markdown render broken (CSP `img-src 'self' data:`) — strip `<img>` or say so (review 1a #14)
- [ ] Destructive buttons (comment delete, status delete, archive) act on one click with no confirmation (review 1a #16)
- [ ] Test gaps: cross-site POST to a real mutating route with a no-write assertion; hostile markdown in a comment at page level; cross-project parent through the form (review 1a #15)
- [ ] Tag names: `école` and `École` are one tag in the input but two in the database (SQLite NOCASE folds ASCII only) (review 1a #1, related)

## Open questions

- [ ] Prevent two servers on different ports sharing one data directory (lock file?) — needed before the CLI starts the server on demand (Phase 2)
- [ ] CI triggers once outside contributors send pull requests (the repo is private and workflows are manual for now)
- [ ] Check the name `vaquero` for collisions on Homebrew and the container registry before the first public release
- [ ] Whether local mode should ever offer a LAN bind with sign-in (current answer: use hosted mode)
- [ ] Make the repository public — planned around the first release in Phase 3
