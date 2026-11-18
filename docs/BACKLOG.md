# Backlog

Vaquero's own work list until a vaquero instance can track it (planned for Phase 3, when import arrives). Each line is a future ticket: when import exists, these move into vaquero and this file is deleted.

Format: `- [ ] Title — detail (source)`. Tick and keep a line when it ships, with the commit; prune ticked lines at the end of each phase.

## Phases

- [x] Phase 0: Foundation — merged a69c53a
- [ ] Phase 1: Core tracker (schema, `internal/core`, events, web UI)
- [ ] Phase 2: API, CLI, MCP, coordination
- [ ] Phase 3: Releases and upgrades (first public release; move this backlog into vaquero)
  - [ ] Linear importer — pick the format(s) to support when Phase 3 is planned; start from https://github.com/linear/linear/tree/master/packages/import. Keep project keys and ticket numbers so existing branch names and commit references still resolve. (Naming the source product is the one exception to "docs never name competing products": an importer has to say what it imports.)
- [ ] Phase 4: Hosted mode
- [ ] Phase 5: Git integration
- [ ] Phase 6: Notifications and attachments
- [ ] Phase 7: LLM assists

## Deferred from the Phase 0 review

- [ ] Fix the stale migration-order line in `.claude/rules/database.md` — the listener now binds before the database opens (review #3)
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

## Open questions

- [ ] Prevent two servers on different ports sharing one data directory (lock file?) — needed before the CLI starts the server on demand (Phase 2)
- [ ] CI triggers once outside contributors send pull requests (the repo is private and workflows are manual for now)
- [ ] Check the name `vaquero` for collisions on Homebrew and the container registry before the first public release
- [ ] Whether local mode should ever offer a LAN bind with sign-in (current answer: use hosted mode)
- [ ] Make the repository public — planned around the first release in Phase 3
