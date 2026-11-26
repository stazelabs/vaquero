# Architecture section design (VAQ-118)

Owner brainstorm, 2026-11-26. Builds on the agent workflow design (`2026-11-25-agent-workflow-design.md`: decisions, Decision 2 "product artifacts are repo files; plans live in Vaquero", Part 2's read-only git and references). Related: VAQ-5 (design artifacts), VAQ-2 (structured requirements, where the new-project intake belongs).

## Intent

Each project gets an Architecture section: the technical decisions (what was chosen, why, what was rejected), the facts of its stack (languages, libraries, infrastructure), and diagrams. It is read by **people**, to get their bearings, and by **agents**, as context before they work. For a new project, the technical and architecture questions of a later intake interview produce its first decisions (owner).

The owner's constraint: **no git collisions**. Several agents on several branches must never edit the same file to record architecture. So the repo holds write-once text, and Vaquero holds everything that changes and every index.

## Goals

- Architecture decisions asked and answered through Vaquero's decisions, numbered `AD-N` per project, with a state.
- Each accepted decision written once to its own repo file, linked and checked by Vaquero.
- An Architecture page per project: the overview, the decision index, detected facts, diagrams.
- The same for agents: `vaquero architecture KEY` and an MCP tool.

## Non-goals

- The new-project intake interview (VAQ-2 and the workflow prompts ask the questions; this design gives them somewhere to go).
- Deployment as a separate section. Infrastructure facts and decisions cover what exists now; a deployment view can follow when Vaquero knows about deployments.
- Editing repo files from Vaquero. Vaquero never writes to the repo; agents do.
- Fact detection beyond the first set below.

## Anti-goals

- A shared file that two branches both have to change to record a decision.
- A committed generated index (it would conflict whenever two branches regenerate it).
- State kept in file headers (it changes after the file is written).

## Approaches

1. **Architecture records tied to decisions.** A record per `AD-N` with its state and file; each comes from a decision with topic `architecture`, so asking, owner-only answers, relaying and provenance are reused. **Chosen** (owner, 2026-11-26).
2. Standalone records with a new approval subject for accepting them. Duplicates what decisions already do. Rejected.
3. Repo files only, state in each file's header. Brings back edits to the same file. Rejected.

## Where things live

| Part | Where | Changes |
|---|---|---|
| Decision text (context, decision, rejected options) | `docs/decisions/AD-{n}-{slug}.md`, one per decision | written once, never edited |
| Superseding | a new decision file whose header says `Supersedes: AD-{m}` | the old file untouched |
| State, who answered, when, the question it came from, the file path | Vaquero | as decisions move |
| The index of decisions | Vaquero (page, CLI, MCP) | never a repo file |
| Overview | `docs/architecture.md`, owner-edited | rarely; the only shared file |
| Diagrams | `docs/architecture/*.mmd`, one per diagram | one file each |
| Facts | read from the repo at main each time | never stored |

The three paths are per-project settings with these defaults, owner-only like the repo settings (controller's call).

## The record

- **Table `architecture_decisions`:** `id`, `project_id`, `number` (`AD-{n}`, per project, from the project's own counter, given out by core in the writer transaction so two agents never get the same one), `title`, `state` (`proposed`, `accepted`, `superseded`, `declined`), `decision_id` (the decision it came from), `supersedes_id` (nullable), `path` (repo path, "" until written), `created_at`, `updated_at`. Exported and imported; removed with the project; `coreseed` seeds an accepted one with a file and a proposed one.
- **Decisions gain a `topic`** (`''` or `architecture`). Asking with topic `architecture` creates the record in `proposed`, its number returned with the decision, so the asking agent can name the file. Such a question also needs a `title` (at most 120 characters; the index, the payloads and the file slug use it; "An architecture question needs a title.") and may name `supersedes` (`AD-m` of the same project) (controller's calls).
- **Decline is an option:** an architecture question's options must include `Decline` (any case; added when the asker gives none), the only option that declines, as `Approve` is the only one that approves an approval; any other answer accepts, recording the chosen option. Withdrawing also declines (controller's call).
- **Supersedes:** set when asking (`supersedes`) or with `architecture add --supersedes AD-m`; it must name an `accepted` record not already superseded by another accepted or proposed record ("AD-12 is already being replaced by AD-31."). The file's `Supersedes:` header is checked against it on read, a mismatch shown as a check message (controller's calls).
- **Who may make one** (controller's calls, after the workflow's "who answers" rule):
  - an agent asks a **question** with topic `architecture`; only the owner answers;
  - an architecture **ruling** is the owner's only (an agent's ruling with that topic is refused: "Only the owner records an architecture ruling; ask a question instead."), so `vaquero architecture add KEY TITLE --decision TEXT` lets the owner record one directly, accepted at once.
- **States:**
  - the owner's answer moves `proposed` to `accepted`, recording the chosen option as the decision;
  - withdrawing the question moves it to `declined`;
  - accepting a record with `supersedes` set moves the superseded one to `superseded`, in the same transaction;
  - an accepted record whose file is recorded is final: changing it is a new `AD` that supersedes it. Overruling an architecture ruling before its file is recorded updates the record; after, it is refused with that advice.
- **The file:** after acceptance the agent (any agent, or the owner) writes the file on its branch and records the path; the path can be recorded again (a typo, a rename) on an accepted or superseded record, each time an event, never on a proposed or declined one: "final" is about the decision, not the pointer (controller's calls). The path (`vaquero architecture file AD-12 PATH`, `PATCH`, MCP). Core checks the path's form (under the decisions directory, named `AD-12-…md`). With a repo path, the page checks on read through `internal/gitread`: on main or not yet ("Not on main yet"), followed across renames, its first heading naming `AD-12` ("The file doesn't name AD-12" otherwise). Without a repo path the record still works and the file isn't checked.
- **Events:** `architecture.proposed`, `architecture.accepted`, `architecture.declined`, `architecture.superseded`, `architecture.file_recorded`, each on the project, `{record_id, number, title, …}`; each is part of the decision's own event where one already fires (an answer emits `decision.answered` and the record's change in the same transaction as one event, payload extended), so the one-event rule holds. Activity's "Plans and decisions" group takes them.

## Listing a folder (owner, 2026-11-26)

`internal/gitread`'s allow-list gains exactly one form, `ls-tree -z <commit> -- <dir>/`, for one folder at main, where `<dir>` must be one of the project's configured paths (the decisions and diagrams directories, `.github/workflows`), checked by `ValidPath` and against the settings before the call. Read-only and fixed like the forms already allowed; the allow-list test names it.

## Facts (first set)

Read at main through `internal/gitread`, cached by main's head:

- **Go:** `go.mod` (module, Go version, direct requirements).
- **Node:** `package.json` (dependencies and devDependencies names and versions).
- **Python:** `pyproject.toml` `[project] dependencies`, else `requirements.txt` (names).
- **Rust:** `Cargo.toml` `[dependencies]` (names).
- **Infrastructure:** the files under `.github/workflows/` (each workflow's `name:`), `Dockerfile` and `compose.yaml`/`docker-compose.yml` present, `.goreleaser.yaml` present.

Each fact names the file it came from, linked to the file view (Part 2). Parsing is line-based for TOML (no new dependency), `encoding/json` for `package.json`. A file that can't be parsed is listed as present without its contents.

## The page (`/p/{key}/architecture`)

A link in the project header beside Settings (not a view tab; controller's call: the tabs are views of tickets), shown only when the project has a repo path or any architecture record, so a project using neither, and `TestWorkflowOffUnchanged`'s goldens, are unchanged. Sections:

1. **Overview:** `docs/architecture.md` at main, rendered through `internal/markdown`; "No overview yet: add docs/architecture.md." when missing.
2. **Decisions:** the index from Vaquero, accepted first by number, then proposed (with the open question's id and `vaquero decision show ID`; there is no web decisions page yet, and Part 5's queue replaces the hint with a link; controller's call), then superseded and declined folded; each row `AD-12 · title · state · answered by you, Nov 26 · file` (the file view link, or its check message).
3. **Stack:** the detected facts, grouped.
4. **Diagrams:** each `.mmd` file rendered with Mermaid, vendored like htmx (`internal/web/static/vendor/mermaid.min.js`, its licence in `internal/legal/vendor/`, loaded only on this page, started from `app.js`; controller's call: text diagrams need a renderer, and it ships in the binary, offline). A diagram that fails to render shows its source.

Without a repo path, sections 1, 3 and 4 say "Set the project's repo path in settings to show this." and the decisions index still shows.

## For agents

- Records are named by project and number everywhere: `vaquero architecture show VAQ AD-12`, `vaquero architecture file VAQ AD-12 PATH`, `/api/v1/projects/{key}/architecture/AD-12`, MCP arguments `project` and `number` (controller's call).
- `vaquero architecture KEY` prints the overview, then the accepted decisions (number, title, file path), then the stack facts, as Markdown; `--all` adds proposed, superseded and declined. `vaquero architecture show AD-12` prints one record with its file's text at main.
- `GET /api/v1/projects/{key}/architecture` (the same content as JSON), `PATCH /api/v1/projects/{key}/architecture/{number}` (the file path).
- MCP `get_architecture` (one API request; the tool list grows by exactly this one, and asking uses `ask_decision` with `topic`).
- The `agent-workflow` help article gains an "Architecture" section: how to ask, the file naming, superseding, and "never edit an accepted decision's file".

## Dogfooding: backfill VAQ (owner, 2026-11-26)

The build ends by recording Vaquero's own architecture in the VAQ project, so the feature is used on day one.

- **How (owner, 2026-11-26):** an agent drafts all of them into one file (title, decision, rejected options, supersedes, per entry); the owner reads it and runs `vaquero architecture add VAQ --from FILE` themselves, without `--agent`, which records each entry as the owner's own ruling in one transaction (all or none, one event each) and prints the numbers given. No relayed batch: `--from` refuses `--agent` and `VAQUERO_AGENT`, as `import linear` does. Each record gets its `AD-` number; then one file per decision under `docs/decisions/`, committed on one branch; then each path recorded. Numbers are given by Vaquero, never chosen in advance, so the files are written after the records.
- **The overview:** `docs/architecture.md`, distilled from CLAUDE.md's summary and package map (the shape of the system, not the package-by-package detail, which stays in CLAUDE.md).
- **Diagrams:** one `docs/architecture/overview.mmd` (binary, SQLite, core, handlers, API, MCP, CLI, sweep, blob store, git reads).
- **The starting list**, from CLAUDE.md's hard invariants and Rejected list and the specs (each file states context, the decision and what was rejected):
  1. One Go binary, one SQLite file, `CGO_ENABLED=0`; pure-Go SQLite, cgo SQLite rejected.
  2. Local mode binds loopback only and checks Host; hosted mode refused until sign-in.
  3. Only `internal/core` writes the tracker, one event per change in the same transaction.
  4. One writer connection; reads through the reader pool.
  5. ULIDs from `internal/idgen`; instants as text through `internal/timefmt`.
  6. Server-rendered templ and htmx; no React or Vite; CSP with no inline script or style.
  7. No ORM, no validation library, no testify, no cobra, no gin/echo/fiber; chi; sqlc for queries.
  8. The CLI and MCP are clients of the HTTP API; MCP tools are API requests.
  9. Ticket versions and stale-save refusals; claims are advisory and lapse.
  10. Only the owner answers; agents relay with the owner's words.
  11. Product artifacts are repo files; plans and decisions live in Vaquero.
  12. Read-only git through the system `git`, an allow-list of subcommands; Vaquero never writes to a repo.
  13. Local-first: offline by default, no telemetry, never checks for releases; upgrades through Homebrew.
  14. Attachments behind a blob store (SQLite default, directory, S3).
  15. Snapshots before every destructive operation.
  16. Docs ship in the binary; Lucide icons through `internal/web/icons`.
  17. Apache 2.0.
  18. Architecture records: write-once files in the repo, state and index in Vaquero (this design).
- CLAUDE.md keeps its invariants (agents load it today); a line there points at `vaquero architecture VAQ` and `docs/decisions/` as the record of why.

## Constraints

- Only core writes the new table and `decisions.topic`; one event per change.
- Theme tokens; no `style=`; no `<svg>` in templates (Mermaid draws into the page from vendored script, which the CSP allows as a self-hosted script; check its output needs no inline style the CSP forbids, and if it does, render diagrams as their source with a note instead).
- e2e for the page at 375 px and desk widths, both themes.

## Left to the builder

- Column and index names; the project counter column for `AD-` numbers.
- The exact payload fields beyond those named.
- The file-path settings' form layout.
