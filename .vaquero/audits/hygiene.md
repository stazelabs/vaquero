# Audit pack: hygiene

Vaquero's own hygiene pack, read by the audit workflow prompt (`vaquero workflow audit --project VAQ --kind hygiene`). Vaquero is one Go binary and one SQLite file; most of its rules are written down as hard invariants and many are already held by tests. A finding is a hazard the gate doesn't catch.

## Standards

Read in full before the sweep:

- [CLAUDE.md](../../CLAUDE.md): the package map (what each package owns, and the tests that hold it), "Hard invariants" and "Rejected". Quote dimension rules from "Hard invariants" first.
- [.claude/rules/database.md](../../.claude/rules/database.md) (pools, `gen.New(tx)`, migrations, search indexes, instants and ids), [.claude/rules/handlers.md](../../.claude/rules/handlers.md) (core-only writes, htmx and the stream, API handlers, MCP tools, feedback, logging), [.claude/rules/security.md](../../.claude/rules/security.md) (local mode, tokens, MCP, the CSP, body limits, attachments, outbound network).
- When a dimension needs the design: [docs/superpowers/specs/2026-11-26-direction-reset.md](../../docs/superpowers/specs/2026-11-26-direction-reset.md) (what may break, the one readiness rule) and [docs/superpowers/specs/2026-11-25-agent-workflow-design.md](../../docs/superpowers/specs/2026-11-25-agent-workflow-design.md).

## Pre-pass

Run from the repository root, in this order; stop and tell the owner if the tree isn't clean first (`git status --porcelain`):

```
make gen && git diff --exit-code    # generated files match their inputs
make fmt && git diff --exit-code    # gofmt has nothing to change
make vet                       # go vet, and the e2e files under their tag
make lint                      # modernize-check, then staticcheck under LINT_TOOLCHAIN (go1.27.1)
VQ_GIT_REQUIRE=1 make test     # every test; the git tests run instead of skipping
make cover-check               # the floors in coverage-floors.txt
```

`make gen` and `make fmt` write to the tree, so they are checks here: when either `git diff` isn't empty, say so in the chat and leave the tree for the owner (the audit never edits code). Any failure here is the gate's, not a ticket: report it and sweep on. `make cover` (coverage by function, lowest first) is context for dimension 8.

Optional, for dimension 5 only, from the main loop, with the owner's say-so because it downloads the tool and the vulnerability database: `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`. Never `go get` it or set `GOFLAGS=-mod=mod`; `git status` must be clean afterwards. A reachable hit is a finding.

## Dimensions

| # | Dimension | What it hunts |
|---|---|---|
| 1 | Dead code and unused surface | Unreferenced exported funcs and types; templ components nothing calls; queries in `internal/db/queries/*.sql` whose `gen` method nothing calls; `app.js` behaviour keyed to a `data-` attribute no template emits; names in `internal/web/icons/ui.json` no template uses; `Deps` fields nothing reads; `VQ_` settings read nowhere; Make targets nothing uses; stale TODOs; a `cli_renamed.go` entry older than its "one release" |
| 2 | Duplication | Helpers that `internal/timefmt`, `internal/idgen`, `internal/units` (`Clip`, `SizeWords`), `core.SameTag`, `core.HasControl`, `config.Redact` or the standard library (`slices`, `maps`, `strings.Cut`) already provide; copied handler skeletons that `invalidOrError`, `redirectWithFlash`, `apiCoreFail` or `refreshWith` cover; near-identical templ blocks where `TextInput`, `Select`, `TextArea`, `emptyState` or `cardItem` serve; the same check written in core and again in a handler with different words; SQL repeated across query files |
| 3 | Architecture and layering | A tracker write outside `internal/core` (CLAUDE.md: "Only `internal/core` writes the tracker tables, and every successful change appends exactly one `events` row in the same transaction"); a core operation that writes no event, or two; an exported `Core` method missing from the `ops` list `TestEveryOperationEmitsOneEventOrNothing` walks; a handler not shaped `func(Deps) http.HandlerFunc` or a route mounted outside `internal/web/router.go`; an MCP tool with its own reads or writes instead of one `/api/v1` request; git run outside `internal/gitread`; an API handler using `d.Actor` instead of `apiActor(r)`; owner-only rules (workflow switch, tier map, repo settings, answers, profile) enforced only in a handler, not in core |
| 4 | Data and query correctness | A write through `Deps.R` (query_only); the pool used inside a transaction instead of `gen.New(tx)` (one writer connection: a deadlock); a read that decides a write taken outside its transaction; instants formatted outside `internal/timefmt`; ids outside `idgen.New()`; a shipped migration edited (append-only, every one runs unattended on any older database); a `VACUUM` of the live database; a version not raised on a ticket change; a `ticket_ids` payload that misses a changed ticket (its history then misses the event); an event payload shape added without its `coreseed` line |
| 5 | Security | Local mode: anything that lets a non-loopback address or another Host through (`config`, `HostCheck`, serve's bound-socket check); a token, header, request body, S3 secret or `vaquero.env` value logged or echoed (`config.Redact` is the one display form); an API refusal that differs by reason (one 401); the agent header honoured for a non-owner token; user bytes served without nosniff and `Content-Security-Policy: sandbox`, or a type taken from the client; Markdown letting raw HTML, a script link or a non-`/attachments/` image through; a body read without `BodyLimit`; gitread: a command outside `Allowed()`, a missing guard `-c` option or environment rule, a repo string reaching a page unescaped or a terminal with control characters, a fetch that isn't the one in `Fetch`; a path from a request reaching the filesystem without `ValidPath` or the backup name check; a CSP break (inline script, `style=`, an inline handler); the optional govulncheck hits |
| 6 | Performance and resource use | N+1 reads on a page or list answer where a batch exists (`ListTicketKeys`, `readiness.CheckAll`, `decisionDTOs`, `LoadProjectStats`); a ticket history or search query that scans `events` or `comments_fts` instead of its index (the plan tests hold some); readiness or a page running git when nothing is pinned; git calls without `Timeout` or `MaxOutput`; unbounded result sets; a long-lived handler that keeps its deadlines or ignores `d.Shutdown`; goroutines the sweeper or thumbnails start and never end; `defer` in loops |
| 7 | Error handling and observability | Ignored errors on a write or close that matters; `err.Error()` shown to a person instead of a plain message; a 5xx path with no log line; log calls not in the context form, not lower case, or without `"err"`; a transient git failure read as "no file" or kept as a cached answer; a failure in a multi-step offline command (import, restore, compact, attachment move) that leaves a half state without saying where things are; a refusal message that doesn't say what to do |
| 8 | Test coverage and quality | A package under or near its floor in `coverage-floors.txt`, or a new package with no floor line; a core operation, API route, CLI command or MCP tool with no test; `Migrate` called outside `internal/db` (`TestNoMigrateOutsideDB`); a hand-built database where `dbtest.New(t)` or `webtest.New(t)` belongs; a mocked database; a git test that never runs because it skips without `VQ_GIT_REQUIRE`; `time.Now` in code a test needs to control (`d.Now`, `Core.Now`); goldens rewritten with `-update` without a behaviour change; tests asserting nothing |

The cross-cutting invariants, and the dimension that owns each:

- Core-only writes and one event per change: 3. A missing event payload field: 4.
- Workflow off byte-identical (`TestWorkflowOffUnchanged`): 3 when the code path differs, 8 when the goldens don't cover the answer.
- Git through gitread only, reads never write or touch the network, the repo is untrusted: 5.
- Local-first (no telemetry, nothing downloaded, network only when configured): 5.
- The reader pool on request paths, one writer connection: 4, or 6 when it costs throughput.
- MCP tools as API requests: 3.
- Every snapshot before a destructive change (delete, migrate, restore, import, compact, attachment move): 4.
- Never a token in a log: 5.

## Search surface

Search: `cmd/`, `internal/` (Go, the `.sql` files in `internal/db/queries/` and `internal/db/migrations/`, the `.templ` sources, `internal/web/static/js/app.js`, `internal/web/static/css/input.css`), `scripts/`, `Makefile`, `.github/workflows/`, `workflows_test.go`, `.goreleaser.yaml`, `packaging/`.

Skip, never cite: `*_templ.go`, `internal/db/gen/`, `internal/web/static/css/tailwind.css`, `internal/web/icons/ui_gen.go`, `internal/legal/third_party.json`, `internal/legal/vendor/`, `internal/web/static/vendor/`, `node_modules/`, `testdata/` (fixtures and goldens), `bin/`, `tmp/`, `data/`, `.claude/`, `.superpowers/`, `docs/superpowers/` (dated history).

## Known non-findings

- **Generated files are committed on purpose** (`*_templ.go`, `internal/db/gen/`, `tailwind.css`, `ui_gen.go`, `third_party.json`). Never dead code, duplication or evidence; the `.templ`, `.sql`, `ui.json` and `input.css` sources are the truth.
- **The "Rejected" list stands**: mattn/go-sqlite3 or any cgo SQLite, ORMs, gin, echo, fiber, React, Vite, Redis, validation libraries, testify, cobra, telemetry, Docker. Never propose one. `chromedp` is allowed for the e2e tests only (owner, 2026-11-25).
- **The single-connection writer pool** is the design; the reader pool is the lever.
- **Writes outside core that CLAUDE.md lists**: the token middleware's last-use touch (`Deps.Touch`, at most every 5 minutes) and the MCP log (`Deps.MCPCalls`); the `blobs` table, written only through `internal/blobs`; `vaquero import`, which fills a new database with `archive.Load` before swapping it in; migrations that backfill rows (0008, 0013, 0014) and the same SQL run again after an import (`BackfillLinkTargets`, `BackfillEventTickets`, `ClearMovedApprovals`).
- **Heartbeat writes no event**: the one exception to the event rule, by design.
- **`emit` writing `event_tickets` rows** is derived data, not a second event.
- **Thumbnails** are derived data, made best effort at upload or on first view, never exported.
- **`SearchTickets` is hand-written SQL** because sqlc can't read FTS5 tables.
- **The `renamed` table in `cli_renamed.go`** and the `VAQUERO_PROFILE` spelling are kept for one release on purpose.
- **The git fetch** is the one network command: off by default, per project (`fetch_minutes`). Not "network in a request" when the owner pressed Fetch now.
- **The S3 attachment store** is network use the operator configured.
- **No CSRF token**: `middleware.CrossOrigin` (Sec-Fetch-Site and Origin) is the chosen defence. Never propose a hidden-field token.
- **No rate limits** in local mode; they come with hosted mode.
- **`archive.MaxDocument` is a variable** only so tests can lower it.
- **The API may break until hosted mode** (direction reset: "break anything"); a changed answer shape is not a compatibility finding. Data on disk still migrates.
- **Claims never block an edit**: a write to a claimed ticket goes through with a `warning`.
- **`VQ_MODE=hosted` is refused** at boot until Phase 4.
- **The e2e tests skip without Chrome** locally; the gate workflow requires it.
- **Workflow push and pull_request triggers are absent on purpose** (`TestWorkflowsHaveNoPushOrPullRequestTriggers`).

## Verification notes

- **Dead code:** grep the symbol in `.templ` sources (`@Name(` and `{ Name(`), `router.go` (`NewRouter` and `mountAPI`), `internal/mcpserver/tools.go`, the CLI's `cliCommands` table in `cmd/vaquero/cli.go`, `cli_renamed.go`, string references in `app.js` and its `data-` attributes in templates, `Icon("name")` strings against `ui.json`, `scripts/`, `internal/core/coreseed`, and test-only use. A `gen` method counts as used only through a non-test caller.
- **Core-only writes:** `TestHandlersNeverWriteDirectly` scans only `internal/web/handlers`; check other packages by grepping for `gen.New(`, `.ExecContext(` and `BeginTx(` outside `internal/core`, `internal/db`, `internal/blobs`, `internal/archive` and the offline commands in `cmd/vaquero`.
- **Events:** an operation emits through `emit` inside its transaction; confirm the `ops` list in `internal/core/*_test.go` holds the operation before calling it untested.
- **Middleware:** the global chain is the `r.Use` list at the top of `NewRouter`; `/api/v1` and `/mcp` add `APIHeaders` and `APIToken`. A route inside a group inherits them; check the group before calling a guard missing.
- **Git:** the runner refuses what `Allowed()` doesn't list before any `Wrap` hook; read `internal/gitread`'s runner, not a call site, to decide whether a flag can reach git.
- **Workflow off:** read `TestWorkflowOffUnchanged` and its goldens in `internal/web/handlers/testdata/workflow-off` before calling an answer uncovered.
- **Performance:** a page is hot when Focus, the board, the list, a ticket page or a polling API answer (`GET /api/v1/events`, `GET /api/v1/ready`) reads it; the admin pages are not.
