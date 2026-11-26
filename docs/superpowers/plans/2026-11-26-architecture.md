# Architecture section Implementation Plan (VAQ-118)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Per-project architecture: decisions asked and answered through Vaquero (`AD-N`, with state and supersedes), each accepted one written once to its own repo file, an Architecture page (overview, decision index, detected stack, Mermaid diagrams), the same for agents through the CLI, API and MCP, and VAQ's own decisions backfilled.

**Architecture:** Core extends decisions with a `topic` and owns a new `architecture_decisions` table whose state moves with the decision's answer, in the same transaction and the same event. `internal/gitread` gains one fixed folder listing; a new `internal/stack` reads facts from manifest files at main. One read helper feeds the page, the API and through it the CLI and MCP.

**Tech Stack:** Go 1.27, SQLite (modernc), sqlc, templ, htmx, `internal/gitread`, Mermaid (vendored).

**Spec:** `docs/superpowers/specs/2026-11-26-architecture-design.md`. Built after VAQ-136 (agent workflow part 2: repo settings, gitread, references and the file view).

## Global Constraints

- Gate before every commit touching Go, SQL, templ or CSS: `make gen && make fmt && make vet && make lint && make test`; page, template, CSS or app.js changes also run `make e2e`.
- Commit messages `{Area}: {summary} (VAQ-118)`, ending with the `Co-Authored-By` line.
- Only core writes `architecture_decisions`, `decisions.topic` and the project's architecture settings; one event per change (an answer's record change rides on `decision.answered`'s payload as `architecture: {record_id, number, state}`).
- Default paths: decisions `docs/decisions`, overview `docs/architecture.md`, diagrams `docs/architecture`; changing them is owner-only ("Only the owner changes the architecture paths.").
- File names `AD-{n}-{slug}.md`, slug from the title (lower case, `[a-z0-9-]`, at most 60 characters).
- Title at most 120 characters; messages exact: "An architecture question needs a title.", "Only the owner records an architecture ruling; ask a question instead.", "AD-12 is already being replaced by AD-31.", "Not on main yet", "The file doesn't name AD-12".
- `Decline` is the one declining option (any case), added when the asker gives none.
- Never use a dev build or the test port against the owner's data directory: Task 6 runs on the owner's Homebrew Vaquero only after a release with this work is installed.
- Theme tokens, no `style=`, no `<svg>` in templates; Mermaid output must pass the CSP (check `securityLevel` and `htmlLabels`; if it can't, show the source with a note).

## Review Focus

1. **Two agents asking architecture questions at once:** distinct `AD-` numbers, never the same one. Test in Task 1.
2. **A batch file with one bad entry** (no `Decision:`, an unknown `Supersedes:`): nothing recorded, the error names the entry's title and line. Test in Task 2.
3. **A folder listing for a path outside the configured ones, or with `..`:** refused before git runs. Test in Task 3.
4. **A `go.mod` or `package.json` that doesn't parse:** listed as present, no contents, no error on the page. Test in Task 3.
5. **A record whose file was renamed on main:** followed (gitread `Follow`), shown at its new path with no check message. Test in Task 5.

---

### Task 1: Records, topic, states and files (core)

**Files:**
- Create: `internal/db/migrations/00NN_architecture.sql`, `internal/db/queries/architecture.sql`, `internal/core/architecture.go`
- Modify: `internal/core/decisions.go` (`DecisionInput.Topic`, `.Title`, `.Supersedes`; `checkDecision`; `AnswerDecision`; `WithdrawDecision`; `RecordRuling`), `internal/core/projects.go` (`ProjectPatch.ArchPaths *ArchPaths`, owner check; `DeleteProject`), `internal/core/events.go` (`EvArchitectureFileRecorded = "architecture.file_recorded"`, payload docs), `internal/archive/archive.go` (`Tables`: `architecture_decisions` after `decisions`), `internal/core/coreseed/seed.go`, `internal/web/templates/activity.go` (`EventSentence`)
- Test: `internal/core/architecture_test.go`, `TestEveryOperationEmitsOneEventOrNothing`, archive round trip

**Interfaces:**
- Migration: `decisions.topic TEXT NOT NULL DEFAULT '' CHECK (topic IN ('', 'architecture'))`; `projects.next_ad INTEGER NOT NULL DEFAULT 1`, `projects.arch_paths TEXT NOT NULL DEFAULT '' CHECK (arch_paths = '' OR json_valid(arch_paths))`; `architecture_decisions (id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects (id), number INTEGER NOT NULL, title TEXT NOT NULL, decision TEXT NOT NULL DEFAULT '', state TEXT NOT NULL CHECK (state IN ('proposed','accepted','superseded','declined')), decision_id TEXT NOT NULL REFERENCES decisions (id), supersedes_id TEXT REFERENCES architecture_decisions (id), path TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE (project_id, number))`.
- Produces:
  - `type ArchPaths struct{ Decisions, Overview, Diagrams string }`, `func EffectiveArchPaths(stored string) ArchPaths`.
  - `func (c *Core) AskDecision` with `Topic: "architecture"` creates the record (`proposed`, `number` = `next_ad`, then `next_ad+1`), and its `decision.asked` payload gains `architecture: {record_id, number, title}`; `gen.Decision` answers carry it through a new `func (q) GetArchitectureByDecision`.
  - `AnswerDecision`: `Decline` → `declined`; any other option or text → `accepted`, `decision` = the chosen label or text; `supersedes` → that record `superseded` in the same tx. `WithdrawDecision` → `declined`. Overruling an architecture ruling: allowed while `path` is "", else `*Invalid` "AD-12 has its file; record a new decision that supersedes it."
  - `RecordRuling` with topic `architecture`: owner only (`IsOwner`, no `RelayedBy`), accepted at once.
  - `type ArchRef struct{ Project string; Number int }`, `func ParseArchRef(s string) (int, error)` ("AD-12", "ad-012" → 12).
  - `func (c *Core) RecordArchitectureFile(ctx context.Context, by, projectID string, number int, path string) (gen.ArchitectureDecision, error)`: any actor; refused on `proposed` or `declined`; path must be under the decisions directory and named `AD-{n}-…md` for its own number; one `architecture.file_recorded` event `{record_id, number, path, previous?}`.
- [ ] **Step 1: Write the failing tests**: `TestAskArchitectureNumbers` (two asks in two goroutines get 1 and 2; Review Focus 1); `TestArchitectureNeedsTitle`; `TestDeclineAddedAndDeclines`; `TestAnswerAccepts` (decision text, one `decision.answered` event carrying `architecture`); `TestWithdrawDeclines`; `TestSupersedes` (accept marks the old one; a second superseder → "AD-12 is already being replaced by AD-31."; superseding a proposed one refused); `TestAgentArchitectureRulingRefused`; `TestOwnerRulingAccepted`; `TestOverruleAfterFileRefused`; `TestRecordArchitectureFile` (wrong number or directory refused, re-record allowed, `previous` in payload); `TestArchPathsOwnerOnly`; `TestDeleteProjectRemovesArchitecture`.
- [ ] **Step 2:** `go test ./internal/core/ -run 'Architecture|Arch|Decline|Supersede'` → FAIL.
- [ ] **Step 3:** Implement; `make gen`. **Step 4:** `go test ./internal/core/... ./internal/archive/... ./internal/db/...` → PASS.
- [ ] **Step 5:** Gate, commit `Core: architecture decisions, numbers, states and files (VAQ-118)`.

### Task 2: The owner's batch (`architecture add --from`)

**Files:**
- Create: `internal/core/archbatch.go`, `internal/core/archbatch_test.go`

**Interfaces:**
- Produces:
  - The batch format (Markdown, owner-readable): an entry starts at `## {title}`; the lines right under it are fields, `Decision: {text}` (required), `Supersedes: AD-{m}` (optional, an existing accepted record or an earlier entry as `#{k}`, its position in the file); the rest of the entry, up to the next `## `, is the body (context and rejected options) kept for the files, not stored.
  - `type ArchEntry struct{ Title, Decision, Supersedes, Body string; Line int }`; `func ParseArchBatch(src string) ([]ArchEntry, error)` (errors name the entry's title and line; Review Focus 2).
  - `func (c *Core) AddArchitectureBatch(ctx context.Context, owner, projectID string, entries []ArchEntry) ([]gen.ArchitectureDecision, error)`: owner acting directly only; one transaction; each entry is an owner ruling with topic `architecture` (its own `decision.ruled` event), all or none.
- [ ] **Step 1: Write the failing tests**: `TestParseArchBatch` (three entries, a `#1` supersedes, bodies kept); `TestParseArchBatchErrors` (no `Decision:`; an empty title; a `#5` that isn't earlier); `TestAddArchitectureBatchAllOrNone` (an unknown `AD-99` in entry 3 records nothing); `TestAddArchitectureBatchOwnerOnly` (an agent actor refused); `TestAddArchitectureBatchEvents` (one event per entry).
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS.
- [ ] **Step 5:** Gate, commit `Core: the owner's architecture batch (VAQ-118)`.

### Task 3: Folder listing and stack facts

**Files:**
- Modify: `internal/gitread/ops.go`, `internal/gitread/gitread.go` (allow-list doc comment and the `ls-tree` form check)
- Create: `internal/stack/stack.go`, `internal/stack/parse.go`
- Test: `internal/gitread/ops_test.go`, `internal/gitread/gitread_test.go` (allow-list test), `internal/stack/stack_test.go`

**Interfaces:**
- Produces:
  - `func (g *Repo) ListDir(ctx context.Context, dir string) ([]string, error)`: `ls-tree -z <main commit> -- <dir>/`, file names (blobs only) sorted; `dir` must pass `ValidPath` and have no `..`; the form check in `gitread.go` accepts exactly this shape and nothing near it. Callers pass only configured paths (`ArchPaths.Diagrams`, `ArchPaths.Decisions`, `.github/workflows`); the handler checks that before calling (Review Focus 3).
  - `type Fact struct{ Group /* "Go", "Node", "Python", "Rust", "Infrastructure" */, Name, Version, Source string /* repo path */ }`; `type Stack struct{ Facts []Fact; Present []string /* files found but not parsed */ }`.
  - `func Read(ctx context.Context, g *gitread.Repo) (Stack, error)`: reads `go.mod` (module, `go` line, direct `require`s), `package.json` (`encoding/json`), `pyproject.toml` `[project] dependencies` else `requirements.txt` (line-based), `Cargo.toml` `[dependencies]` (line-based), workflow `name:` lines from `ListDir(".github/workflows")`, and presence of `Dockerfile`, `compose.yaml`, `docker-compose.yml`, `.goreleaser.yaml`. A missing file is skipped; one that won't parse goes to `Present` (Review Focus 4).
  - A cache in handlers keyed by (project id, main head), at most 64 entries: `func (d Deps) stackOf(ctx, p gen.Project) (stack.Stack, bool)`.
- [ ] **Step 1: Write the failing tests** with gitread's test repo helpers: `TestListDir` (two files and a subfolder: the files only); `TestListDirRefused` (`../x`, absolute, empty); the allow-list test names the new form; `TestStackGo`, `TestStackNode`, `TestStackPython`, `TestStackRust`, `TestStackInfra`, `TestStackUnparsable`.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS.
- [ ] **Step 5:** Gate, commit `Git, Stack: list a folder at main and read the stack (VAQ-118)`.

### Task 4: API, CLI and MCP

**Files:**
- Create: `internal/web/handlers/api_architecture.go` (with `func architectureOf(ctx context.Context, d Deps, p gen.Project, all bool) (api.Architecture, error)`, shared with Task 5), `cmd/vaquero/cli_architecture.go`
- Modify: `internal/api/types.go` (`Architecture{Overview string; Decisions []ArchDecision; Stack []Fact; Present []string; Diagrams []Diagram}`, `ArchDecision{Number, Title, Decision, State, Path, Check, Supersedes, SupersededBy, DecisionID, AnsweredBy, AnsweredAt}`, `Diagram{Path, Source}`; `Decision.Topic`, `.Architecture`), `internal/web/handlers/api_decisions.go` (`topic`, `title`, `supersedes` on ask; unknown topic 422), `internal/web/router.go` (`GET /projects/{key}/architecture`, `GET`/`PATCH /projects/{key}/architecture/{number}`, `POST /projects/{key}/architecture/batch` owner-only), `internal/mcpserver/tools.go` (`get_architecture`; `ask_decision` gains `topic`, `title`, `supersedes`), `cmd/vaquero/cli.go`, `cmd/vaquero/cli_decisions.go` (`decision ask --topic architecture --title T [--supersedes AD-m]`), `internal/help/articles/cli.md`, `api.md`, `mcp.md`, `agent-workflow.md` ("Architecture")
- Test: `internal/web/handlers/api_architecture_test.go`, `cmd/vaquero/cli_architecture_test.go`, `internal/mcpserver/tools_test.go`

**Interfaces:**
- Consumes: Tasks 1–3.
- Produces:
  - `architectureOf`: overview at main through gitread (`""` without a repo path), records (accepted by number, then proposed, then superseded and declined; `all` false keeps accepted only), each record's `Check` ("" ok, "Not on main yet", "The file doesn't name AD-12", or a `Supersedes:` mismatch message), its path after `Follow`; the stack; diagrams' sources from `ListDir(Diagrams)` filtered to `.mmd`.
  - CLI: `vaquero architecture KEY [--all]` (Markdown: `# Architecture of VAQ`, the overview, `## Decisions` lines `AD-12 · title · path`, `## Stack`); `vaquero architecture show KEY AD-12` (record plus its file's text at main); `vaquero architecture file KEY AD-12 PATH`; `vaquero architecture add KEY TITLE --decision TEXT [--supersedes AD-m]` and `vaquero architecture add KEY --from FILE` (both refuse `--agent` and `VAQUERO_AGENT`, as `import linear` does; `--from` prints "Recorded AD-1 … AD-18.").
  - MCP `get_architecture` (`project`, `all?`) → one `GET`.
- [ ] **Step 1: Write the failing tests**: API read with and without a repo path; `PATCH` path recording and its refusals; batch as an agent token → 422 with the owner message; CLI Markdown golden; `--from` with `--agent` → exit 2; MCP schema and call; help tests pass with the new lines.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS.
- [ ] **Step 5:** Gate, commit `API, CLI, MCP: architecture (VAQ-118)`.

### Task 5: The page and Mermaid

**Files:**
- Create: `internal/web/handlers/architecture.go`, `internal/web/templates/architecture.templ`, `internal/web/static/vendor/mermaid.min.js` (pinned release), `internal/legal/vendor/mermaid-LICENSE`
- Modify: `internal/web/router.go` (`/p/{key}/architecture`), `internal/web/templates/board.templ` (`projectHeader`: an "Architecture" icon link beside Settings when the project has a repo path or any record), `internal/web/templates/projects.templ` (settings: the three paths, owner form), `internal/web/static/js/app.js` (start Mermaid on `[data-mermaid]` only on this page; on failure show the source), `internal/web/icons/ui.json`, `scripts/notices` input list (Mermaid with htmx and SortableJS), `internal/help/articles/architecture.md` (new, section `using`)
- Test: `internal/web/handlers/architecture_test.go`, `internal/e2e/architecture_test.go`, `internal/legal` (`TestThirdPartyCoversEveryLinkedModule` and the vendored list)

**Interfaces:**
- Consumes: Task 4's `architectureOf`.
- Sections per the spec: Overview (`internal/markdown`), Decisions (rows `AD-12 · title · state · answered by you, Nov 26 · file`; proposed with "Question {id}: `vaquero decision show {id}`"; superseded and declined in a `<details>`), Stack (grouped facts, each source linked to the file view), Diagrams (`<pre data-mermaid>` with the source). Without a repo path: the three messages from the spec, the index still shown.
- [ ] **Step 1: Write the failing tests**: page sections with a test repo; no repo path messages; the header link present only with a repo path or a record, absent otherwise (`TestWorkflowOffUnchanged` untouched); a renamed decision file shown at its new path with no check message (Review Focus 5); e2e: a diagram renders without a CSP refusal (the e2e harness fails on any), at 375 px and 1280 px, both themes.
- [ ] **Step 2:** → FAIL. **Step 3:** implement; if Mermaid can't render inside the CSP, keep the `<pre>` with "Diagrams show as source; rendering needs …" and say so in the commit. **Step 4:** → PASS (`make e2e`).
- [ ] **Step 5:** `make ci`; CLAUDE.md package map (core architecture, `internal/stack`, `ListDir`, the routes, Mermaid in the vendored list and the hard invariants' vendored scripts); commit `Web: the architecture page (VAQ-118)`.

### Task 6: Backfill VAQ (dogfooding)

Runs only after a release containing Tasks 1–5 is installed through Homebrew (`brew upgrade vaquero`) on the owner's machine. Never a dev build against the owner's data.

**Files:**
- Create: `docs/architecture/backfill-vaq.md` (the batch), then `docs/decisions/AD-{n}-{slug}.md` × 18, `docs/architecture.md`, `docs/architecture/overview.mmd`
- Modify: `CLAUDE.md` (one line under Summary: "Why things are as they are: `vaquero architecture VAQ` and `docs/decisions/`.")

- [ ] **Step 1:** Draft `docs/architecture/backfill-vaq.md` in Task 2's format with the spec's 18 entries in order: each a title, `Decision:` in one sentence, and a body of context and rejected options drawn from CLAUDE.md's hard invariants and Rejected list and the specs. Commit it on a branch.
- [ ] **Step 2: Stop for the owner.** Ask them to read it and run `! vaquero architecture add VAQ --from docs/architecture/backfill-vaq.md`. Their output lists the numbers given.
- [ ] **Step 3:** Write one file per entry, `docs/decisions/AD-{n}-{slug}.md`, using the given numbers: `# AD-{n}: {title}`, a `Supersedes:` line only where the batch had one, then the entry's body. Write `docs/architecture.md` (the system's shape, distilled from CLAUDE.md's summary: binary, SQLite, core, handlers and API, CLI, MCP, sweep, blob store, git reads) and `docs/architecture/overview.mmd`.
- [ ] **Step 4:** After merge to main, record each path: `vaquero architecture file VAQ AD-{n} docs/decisions/AD-{n}-{slug}.md` (as an agent, through `--agent`). Then `vaquero architecture VAQ` shows 18 accepted decisions, each with no check message.
- [ ] **Step 5:** Delete `docs/architecture/backfill-vaq.md` (its content now lives in the files), add the CLAUDE.md line, commit `Docs: VAQ's architecture decisions (VAQ-118)`, and move VAQ-118 to Done.
