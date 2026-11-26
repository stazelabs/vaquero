# Agent workflow, part 2: read-only git, references, the remote and fetch — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A project knows its git repo, and Vaquero reads it to:
- show referenced files, at main or at the commit an approval pinned, with "changed since approval" and a diff;
- list the commits that mention a ticket;
- make readiness fail when a file an approval rests on changes;
- report the remote's state as of the last fetch, with an optional fetch that is off by default;
- answer the git reads the Trace panel (part 5) is built from.

**Architecture:**
- **One new package, `internal/gitread`,** is the only code that runs `git`: fixed arguments, checked inputs, a timeout, capped output and a scrubbed environment. It exposes typed reads, plus the one fetch.
- **Core gains `Core.Git`** (`*gitread.Repos`), so it can check a repo path on save and pin files when an approval is asked. Readiness takes a `readiness.Files` checker, so the board, `ready` and the API compare pins in one batch, cached on main's head.
- **One migration, `0012_git.sql`.**
- **Web changes stop at the minimum:** settings, the references list, commits on the ticket page, and the file view. The Trace panel itself is part 5 (VAQ-139), built on this part's API.

**Tech Stack:** Go 1.27, the system `git` (2.30 or later), modernc SQLite, goose, sqlc, templ and htmx. No new dependency.

**Spec:** `docs/superpowers/specs/2026-11-25-agent-workflow-design.md`. Read Part 2 (amended 2026-11-26: the Trace reads, the remote and fetch), Part 1's Readiness (file pins), and Part 5's Trace panel. Ticket: VAQ-136.

## Global Constraints

- CLAUDE.md hard invariants:
  - only core writes, with one event per change;
  - the API is additive;
  - MCP tools are API requests;
  - CSP: no inline script, no `style=`;
  - theme tokens only;
  - local-first, with no telemetry;
  - the agent workflow trust model;
  - "workflow off is unchanged".
- **Exactly one migration, `0012_git.sql`,** written in Task 1:
  - `projects` gains:
    - `repo_path TEXT NOT NULL DEFAULT ''`;
    - `main_branch TEXT NOT NULL DEFAULT 'main'`;
    - `remote TEXT NOT NULL DEFAULT ''` ('' means `origin` when it exists);
    - `fetch_minutes INTEGER NOT NULL DEFAULT 0 CHECK (fetch_minutes = 0 OR fetch_minutes BETWEEN 5 AND 1440)`.
  - `ticket_refs`:
    - `id` TEXT PK, `ticket_id` REFERENCES tickets;
    - `kind` CHECK IN ('requirement', 'design', 'plan', 'spec', 'other');
    - `target` TEXT (a repo path or an http(s) URL), `anchor` TEXT DEFAULT '', `label` TEXT DEFAULT '';
    - `created_by`, `created_at`;
    - UNIQUE (`ticket_id`, `target`, `anchor`), and an index on `ticket_id`.
  - `decision_tickets.files TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(files) AND json_type(files) = 'array')`.

  Released fixtures v0.1.0 to v0.1.5 must still migrate.
- **Git runs only through `gitread`** (`TestOnlyGitreadRunsGit` greps the tree for `exec.Command("git"` and `exec.CommandContext(ctx, "git"` outside it). Every call:
  - uses `git -C {repo}`;
  - passes `-c core.fsmonitor=false -c core.hooksPath=/dev/null -c credential.interactive=never`, and `--no-textconv --no-ext-diff` wherever it applies;
  - runs with this environment: `GIT_TERMINAL_PROMPT=0`, `GIT_OPTIONAL_LOCKS=0`, `GIT_PAGER=cat`, `LC_ALL=C`, and `GIT_SSH_COMMAND="ssh -o BatchMode=yes"` (fetch only);
  - times out at 5 s (fetch 60 s);
  - stops reading output at 1 MiB (a file shown: 512 KiB, after which it says "too large to show").
- **Only fetch touches the network or writes** (to remote-tracking refs and `refs/vaquero/remote-tags/`). No other command may be added that does either (`TestGitreadCommandsAreReadOnly` holds an allow-list of subcommands).
- **Repo content is untrusted:** Markdown renders through `internal/markdown`, everything else as escaped text. A URL from `git remote get-url` is shown with its userinfo removed.
- **Workflow off:** `TestWorkflowOffUnchanged` still holds byte for byte. The new fields `references` (on a ticket answer) and `repo` (on a project answer) are left out when empty.
- **The repo settings are the owner's (ruling P2-R1):** `repo_path`, `main_branch` and `remote` are readiness inputs, like the workflow switch (N-R6). Only the owner, acting directly, changes them: "Only the owner changes the repo settings." `fetch_minutes` is open to anyone.
- Every new route, command and MCP tool gets its line in the api, cli or mcp article. A new help article, `repositories`, explains the settings, the references, fetching, and that only fetch uses the network.
- **Gate,** each step separately by exit code: `make gen`, `make fmt`, `make vet`, `make lint`, `make test`, then `make ci`, plus `make e2e` from Task 4 on.
- **Commits:** `{Area}: {summary} (VAQ-136)`, with the Co-Authored-By line, staging only the changed paths.
- **Tests make real repos** with `gitread/gittest.Repo(t)`, which runs `git init` in `t.TempDir()` with a fixed author and date and helpers to commit, rename, tag and branch. A test skips without `git`, unless `VQ_GIT_REQUIRE` is set, which the gate workflow sets.

## Rulings (controller, 2026-11-26; the owner can overrule)

- **P2-R1:** only the owner, acting directly, changes the repo path, main branch and remote. See Global Constraints.
- **P2-R2:** an approval pins every repo-path reference of its ticket: its path, main's commit, and the file's blob id at that commit.
  - URL references aren't pinned.
  - Asking is refused when a referenced file isn't on main: "docs/x.md isn't on main; merge it before asking for approval."
  - Asking is also refused when the repo can't be read: "Can't read the repo: {reason}."
- **P2-R3:** file changes are judged by content, not by commits. A pin holds while the file's blob on main, with renames followed from the pinned commit, equals the pinned blob. A commit that leaves the file's content unchanged doesn't unready anyone.
- **P2-R4:** a repo-path reference added or removed since the approval counts as a file change.
- **P2-R5 (new readiness reasons):** three are added to `Reasons`, in this order after `EpicPlanChanged`:
  - `FilesChanged = "Files changed since approval."`
  - `EpicFilesChanged = "Epic's files changed since approval."`
  - `FilesUnchecked = "Files can't be checked."` (no repo path, no git, or a read error; never a silent pass)

  The answer's new `files` (additive, workflow on only) says per path: `unchanged`, `changed`, `renamed` (with the new path), `gone`, `added` or `unchecked`.

---

### Task 1: Migration 0012 and `internal/gitread`

**Files:**
- Create: `internal/db/migrations/0012_git.sql` (the whole schema above)
- Create: `internal/gitread/gitread.go` (the runner), `ops.go` (the reads), `fetch.go`, `check.go` (input checks), `url.go`, `internal/gitread/gittest/gittest.go`
- Test: `internal/gitread/*_test.go`, plus `internal/gitread/tree_test.go` (`TestOnlyGitreadRunsGit`)

**Interfaces (produced):**
```go
type Repos struct{ /* git path, timeouts, per-repo fetch singleflight */ }
func New() *Repos                         // finds git on PATH; Available() false without it
func (r *Repos) Available() bool
func (r *Repos) Open(ctx context.Context, path, mainBranch, remote string) (*Repo, error) // ErrNotRepo, ErrNoGit
type Repo struct{ Path, Main, Remote string } // Remote resolved: "" → "origin" if present, else none
func (g *Repo) Head(ctx) (commit string, err error)                                 // main's commit
func (g *Repo) Show(ctx, rev, path string) (File, error)                             // File{Blob, Size, Text, TooLarge}; ErrNoFile
func (g *Repo) Blob(ctx, rev, path string) (string, error)                           // "" + ErrNoFile when absent
func (g *Repo) Follow(ctx, path, since string) (current string, gone bool, err error) // renames since `since` up to main
func (g *Repo) PathLog(ctx, path, since string, max int) ([]Commit, error)
func (g *Repo) Mentions(ctx, key string, max int) ([]Commit, error)                  // main's log, --fixed-strings --grep "(KEY)"
func (g *Repo) BranchCommits(ctx, branch string, max int) ([]Commit, error)          // main..branch
func (g *Repo) Merged(ctx, branch string) (bool, error)                              // merge-base --is-ancestor
func (g *Repo) TagExists(ctx, tag string) (bool, error)
func (g *Repo) Diffstat(ctx, commits []string) (Diffstat, error)                    // Added, Removed, Files []string (sorted, distinct)
func (g *Repo) FirstTagContaining(ctx, commit string) (string, error)              // tag --contains, --sort=v:refname, first; "" when none
func (g *Repo) Compare(ctx, local, remoteRef string) (ahead, behind int, ok bool, err error) // ok false: no such remote ref
func (g *Repo) RemoteTag(ctx, tag string) (on bool, known bool, err error)           // from refs/vaquero/remote-tags/{remote}/; known false before Vaquero's first fetch
func (g *Repo) Remotes(ctx) ([]string, error)
func (g *Repo) RemoteURL(ctx) (string, error)                                        // DisplayURL applied
func (g *Repo) LastFetch() (time.Time, bool)                                         // FETCH_HEAD mtime
func (g *Repo) Fetch(ctx) error                                                      // the one network call; singleflight per repo
type Commit struct{ Hash, Short, Subject, Author string; At time.Time }
func ValidPath(p string) bool; func ValidRev(s string) bool; func DisplayURL(u string) string
```

- [ ] **Step 1: Write the failing tests.**
  - `TestValidPath`:
    - true: `docs/a.md`, `a b/c.md`;
    - false: ``, `/etc/passwd`, `../x`, `a/../b`, `-rf`, `a//b`, `a\x00b`, `.git/config`.
  - `TestValidRev`:
    - true: 40-hex, 7-hex, `main`, `v0.1.6`, `run-o`, `origin/main`;
    - false: `-x`, `a..b`, `HEAD@{1}`, `main~1`, `:x`, spaces, a trailing `.lock`.
  - `TestDisplayURL`: `https://u:tok@github.com/o/r.git` → `https://github.com/o/r.git`, while `git@github.com:o/r.git` is unchanged.
  - `TestShowAndBlob` and `TestShowTooLarge`, with a 600 KiB file.
  - `TestFollowRename`: rename `a.md` to `b.md` after commit C, so `Follow("a.md", C)` gives `b.md`, not gone. A deleted file gives gone.
  - `TestMentions`: only messages containing `(VAQ-12)` match, not `(VAQ-123)`.
  - `TestDiffstat`, `TestFirstTagContaining` (`v0.1.10` sorts after `v0.1.9`), `TestMerged` and `TestTagExists`.
  - `TestCompare`: a bare repo serves as the remote. Push, commit twice locally, and the answer is ahead 2, behind 0, ok. A branch never pushed gives ok false.
  - `TestLastFetch`: false before any fetch, and set after `Fetch`.
  - `TestFetchUpdatesOnlyRemoteRefs`: a local branch and the work tree are byte-identical before and after.
  - `TestFetchNeverPrompts`: a remote URL of `https://127.0.0.1:1/x.git` fails within the timeout and doesn't hang.
  - `TestHostileRepoConfig`: a repo whose `.git/config` sets `core.fsmonitor`, `core.pager` and a `diff.external` to a script that touches a marker file. Every read leaves the marker absent.
  - `TestGitreadCommandsAreReadOnly`: the runner refuses any subcommand outside the allow-list `rev-parse`, `show`, `cat-file`, `log`, `merge-base`, `tag`, `rev-list`, `for-each-ref`, `remote`, `diff`, `fetch`, with `fetch` reachable only from `Fetch`.
  - `TestOnlyGitreadRunsGit`.
- [ ] **Step 2:** Run `go test ./internal/gitread/...`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement.**
  - **`Fetch` runs exactly** `git fetch --prune --no-tags {remote} +refs/heads/*:refs/remotes/{remote}/* +refs/tags/*:refs/vaquero/remote-tags/{remote}/*`.
    - Local tags and local branches are never touched.
    - The remote's tags land in Vaquero's own namespace, so `RemoteTag` is exact as of the last fetch Vaquero ran.
    - Before that first fetch, `known` is false, and the trace says "unknown until fetched".
  - `TestFetchUpdatesOnlyRemoteRefs` also checks that `refs/tags/*` is unchanged, and that the remote's tags appear under `refs/vaquero/remote-tags/origin/`.
  - Output is parsed with `-z` or `--format` using `%x00` separators wherever git offers them.
  - Every error wraps git's first stderr line, cut to 200 characters.
- [ ] **Step 4:** The tests pass, and `TestFixturesMigrateToHead` passes.
- [ ] **Step 5:** Commit, then gate.

### Task 2: Repo settings (core, API, CLI, settings page)

**Files:** `internal/core/projects.go` (ProjectPatch gains `RepoPath`, `MainBranch`, `Remote *string`, `FetchMinutes *int64`), `internal/core/core.go` (`Git *gitread.Repos`), `cmd/vaquero/serve.go` (wires `gitread.New()`), `internal/web/handlers/projects.go` and `api_write.go`, `internal/api/types.go` (`Project.Repo`), `cmd/vaquero/cli_cmds.go`, `internal/web/templates/projects.templ` (a Repository section in settings), `internal/help/articles/repositories.md` (new), plus api.md and cli.md.

**Interfaces (produced):**
- `api.ProjectRepo{Path, MainBranch, Remote, RemoteURL, FetchMinutes, LastFetchAt?, LastFetchError?, Available bool}`, as `repo` on the project answer when `repo_path` is set.
- The CLI's `vaquero project edit KEY --repo PATH --main-branch NAME --remote NAME --fetch-minutes N`.

- [ ] **Step 1: Write the failing tests.**
  - `TestUpdateProjectRepoPath`:
    - core refuses a relative path ("Use an absolute path.");
    - it refuses a path that isn't a work tree ("That isn't a git work tree.");
    - it refuses a `main_branch` with no such branch ("The repo has no branch main.");
    - it refuses a remote not in `git remote` ("The repo has no remote upstream.");
    - an accepted save writes one `project.updated` event with `changes.repo_path`.
  - The check runs before the transaction, and never inside it.
  - `TestRepoSettingsOwnerOnly`: an agent (`Vaquero-Agent`, `--agent` or MCP) changing any of the three is refused with "Only the owner changes the repo settings.", while an agent's `fetch_minutes` change goes through.
  - `TestFetchMinutesBounds`: 0, 5 and 1440 are accepted; 4 and 1441 get the 422 "Fetch every 5 to 1440 minutes, or 0 for never."
  - `TestProjectAnswerRepo`: a project with no repo has no `repo` key. With a repo, `remote_url` comes with its userinfo removed.
  - The settings page round-trip test (web form) and the CLI's flag test.
  - `TestWorkflowOffUnchanged` still passes, unedited.
- [ ] **Step 2:** The tests fail.
- [ ] **Step 3: Implement.**
  - The Repository section shows the path, main branch, remote (a select fed by `Remotes`), the remote's URL read-only, and Fetch every N minutes (a number, 0 = never).
  - When git is missing, the section says "Git isn't installed, so repository features are off." and disables the fields.
- [ ] **Step 4:** The tests pass, and the screenshots are checked in light and dark.
- [ ] **Step 5:** Commit, then gate.

### Task 3: References on tickets

**Files:** `internal/core/refs.go` (`AddRef`, `RemoveRef`), `internal/core/events.go` (`ref.added`, `ref.removed`, payload `{ref_id, kind, target, anchor?}`), `internal/db/queries/refs.sql`, `internal/archive/archive.go` (`ticket_refs` after `tickets` in `Tables`; round-trip in the export test), `internal/core/coreseed` (one repo reference and one URL reference), `internal/web/handlers/api_refs.go`, `internal/mcpserver/tools.go` (`add_reference`, `remove_reference`; the MCP article), `cmd/vaquero/cli_refs.go`, the ticket template (a References section), and `templates/activity.go` (sentences for both events).

**Interfaces (produced):**
- `core.AddRef(ctx, by, ticketID string, r RefInput) (gen.TicketRef, error)`, where `RefInput{Kind, Target, Anchor, Label string}`.
- `core.RemoveRef(ctx, by, refID string) error`.
- The routes `POST /api/v1/tickets/{ticket}/refs` and `DELETE /api/v1/tickets/{ticket}/refs/{id}`, and `references` on the ticket answer (omitted when empty).
- The CLI's `vaquero ref add TICKET KIND TARGET [--anchor A] [--label L]`, `vaquero ref remove TICKET ID`, and `ticket show` listing references.

- [ ] **Step 1: Write the failing tests.**
  - `TestAddRef`:
    - an http(s) URL is accepted with no repo needed;
    - a repo path needs the project's `repo_path` ("Set the project's repository first.") and must pass `gitread.ValidPath`;
    - any other scheme is refused, `javascript:` included;
    - the anchor must match `^[a-z0-9][a-z0-9-]{0,79}$`;
    - the label is at most 120 characters;
    - a duplicate target and anchor is refused ("VAQ-12 already references docs/a.md.");
    - each add or remove writes one event.
  - A repo path doesn't have to exist on main when it's added. The file view and pins flag it.
  - `TestRefsMoveWithTicket`: after `MoveTicketToProject`, the references are still there. A repo-path reference into a target project with a different repo is kept, and the file view reads it against the new project's repo.
  - `TestDeleteProjectRemovesRefs` and the archive round trip.
  - MCP: the tool list grows by exactly these two, and `ToolNames` and the article both list them.
  - `TestEveryOperationEmitsOneEventOrNothing` covers `AddRef` and `RemoveRef`.
- [ ] **Step 2:** The tests fail.
- [ ] **Step 3:** Implement. The ticket page's References list shows kind, label (else target) and anchor. A repo path links to the file view, a URL opens in a new tab with `rel="noopener noreferrer"`, and a remove button sits behind the confirm dialog.
- [ ] **Step 4:** The tests pass.
- [ ] **Step 5:** Commit, then gate.

### Task 4: The file view and commits on tickets

**Files:** `internal/web/handlers/files.go`, `internal/web/templates/files.templ`, `internal/web/handlers/api_git.go`, `internal/web/router.go`, `cmd/vaquero/cli_git.go`, `internal/mcpserver/tools.go` (`read_file`), the ticket template (a Commits section), and `internal/e2e/files_test.go`.

**Interfaces (produced):**
- `GET /p/{key}/files/*?at=REV&ref=ID`: the file view.
- `GET /api/v1/projects/{key}/files/*?at=REV` returns `{path, at, blob, size, text?, too_large, markdown}`.
- `GET /api/v1/tickets/{ticket}/commits` returns `{commits: [Commit], diffstat: {added, removed, files}, released?: "v0.1.6"}`. That's the trace data part 5 renders.
- `GET /api/v1/projects/{key}/repo/status` returns `{main: {ahead, behind, tracked}, unpushed_branches: [...], last_fetch_at?, last_fetch_error?}`.
- The CLI's `vaquero file KEY PATH [--at REV]` and `vaquero commits TICKET`.

- [ ] **Step 1: Write the failing tests.**
  - `TestFileViewRendersMarkdownSafely`: a repo `.md` file containing `<script>` and a `javascript:` link renders escaped, as `internal/markdown` does.
  - A `.go` file shows as an escaped `<pre>`.
  - `TestFileViewRefusesBadInput`: `..`, a leading `-`, a revision `HEAD@{1}` and a NUL each get a 404 page (422 in the API), and git is never run (a fake runner counts calls).
  - `TestFileViewChangedSinceApproval`: with `?ref=ID` on an approved ticket whose file changed, the page says "Changed since approval" and carries a Show changes link to the diff. The diff is the `LineDiff` between the pinned blob and main, rendered only when opened (VAQ-143's pattern). A renamed file says "Renamed to b.md since approval". A gone file says "Removed from main since approval".
  - `TestTicketCommits`: only main's commits with `(VAQ-12)` show, newest first, at most 50, with the diffstat. `released` is the first tag containing the newest commit.
  - `TestRepoStatusNoNetwork`: the status route runs no `fetch` (the fake runner records subcommands).
  - Without a repo path, the Commits section and `/commits` say "No repository set for this project." (the API: 409 `no_repo`).
  - Without git, both say "Git isn't installed." (the API: 409 `no_git`).
  - e2e: the ticket page with references and commits, and the file view, at 375 and 1280 px in light and dark. No page is wider than 375 px at 375 px; long paths wrap.
- [ ] **Step 2:** The tests fail.
- [ ] **Step 3:** Implement. Git reads in handlers go through `Deps.Git` (the same `*gitread.Repos` as core). A request's git calls share its context, so a closed tab cancels them.
- [ ] **Step 4:** The tests pass, and `make e2e` passes.
- [ ] **Step 5:** Commit, then gate.

### Task 5: File pins in approvals and readiness

**Files:** `internal/core/decisions.go` (`createDecision` pins files; `askedUnchanged` compares them; `ApprovalPins` gains `Files []FilePin`), `internal/readiness/readiness.go` and a new `files.go`, `internal/board/*.go` and `internal/web/handlers/api_read.go` (pass the checker), `internal/api/types.go` (`Readiness.Files`), and the agent-workflow article (the new reasons).

**Interfaces (produced):**
```go
type FilePin struct{ RefID, Path, Commit, Blob string }      // core; JSON in decision_tickets.files and approvals.pins.files
type Files interface {                                       // readiness
    // State of each pin for a project, compared against main once per call.
    Check(ctx context.Context, project gen.Project, pins []core.FilePin, refs []gen.TicketRef) ([]FileState, error)
}
type FileState struct{ Path, State, Now string }            // State: unchanged|changed|renamed|gone|added|unchecked
func CheckAll(ctx context.Context, q *gen.Queries, files Files, ids []string) (map[string]Readiness, error) // files nil → pins unchecked
func NewFiles(r *gitread.Repos) Files                       // caches by (repo, main head, pin): one Head per project per call
```

- [ ] **Step 1: Write the failing tests.**
  - `TestApprovalPinsFiles`: asking a plan approval for a ticket with two repo-path references pins both, at main's head, with their blob ids.
  - `TestAskRefusesFileNotOnMain`, with P2-R2's message.
  - `TestAnswerRefusesFileChangedSinceAsked`: a commit changing the file between ask and answer makes Approve refuse with "A file VAQ-12 references changed since this was asked; ask again.". Any other answer is still taken.
  - `TestReadinessFileChanged`:
    - after the approval, a commit changing the file on main makes the ticket not ready, with `FilesChanged` and `files[0].state == "changed"`;
    - a commit touching the file without changing its content (P2-R3) leaves it ready;
    - a rename leaves it ready, with `renamed` and `now`;
    - deleting the file gives `gone`, and the ticket isn't ready.
  - `TestReadinessRefAddedAfterApproval`: a repo-path reference added after the approval gives `added` and `FilesChanged` (P2-R4). A URL reference added changes nothing.
  - `TestReadinessEpicFilesChanged`: a child leaning on its epic gets `EpicFilesChanged` when the epic's pinned file changes.
  - `TestReadinessFilesUnchecked`: removing the repo path, a nil checker and git missing each give `FilesUnchecked`, never ready.
  - `TestReadinessGitBatched`: `CheckAll` over 40 tickets in two projects runs `rev-parse` for main's head at most once per project (the fake runner counts calls). A second call with main unchanged runs no blob reads.
  - `TestWorkflowOffUnchanged` still passes: with the workflow off, no git runs (counted).
  - The `Reasons` order test is updated with P2-R5's three.
- [ ] **Step 2:** The tests fail.
- [ ] **Step 3:** Implement. Pins are read in the same batch queries as part 1's (`ListPlanApprovalAnswers` carries `pins`). The checker never runs git inside a database transaction.
- [ ] **Step 4:** The tests pass.
- [ ] **Step 5:** Commit, then gate.

### Task 6: Fetching

**Files:** `internal/sweep/sweep.go` (a `Fetcher` step), `internal/web/handlers/api_git.go` (`POST /api/v1/projects/{key}/repo/fetch`), `internal/web/handlers/projects.go` (Fetch now on the project's settings page; the Trace panel's button comes in part 5), `cmd/vaquero/cli_git.go` (`vaquero fetch KEY`), and the repositories article.

**Interfaces (produced):**
- `sweep.Sweeper.Fetch func(ctx) // projects with fetch_minutes > 0 whose last fetch is older`.
- `POST …/repo/fetch` answers `{last_fetch_at, error?}`, 200 when the fetch ran (even if it failed: the failure is data), and 409 `no_repo` or `no_git`.

- [ ] **Step 1: Write the failing tests.**
  - `TestSweepFetchesWhenDue`: with `fetch_minutes` 0, no fetch happens. With 5, a fetch runs when the last is more than 5 minutes old, and not before. Fetches across projects run one at a time, and two projects sharing a repo path fetch once.
  - `TestFetchNowSingleflight`: two concurrent POSTs run one `git fetch`.
  - `TestFetchFailureShown`: a fetch that fails sets `last_fetch_error` to git's first line, which the settings page shows with its time. Reads keep working.
  - `TestFetchWritesNoEvent`: the event count is unchanged.
  - `TestFetchIsTheOnlyNetwork`: across the whole test suite's fake runner, `fetch` was recorded only from `Fetch`.
  - The help article states that fetching is off by default and is the only network use.
- [ ] **Step 2:** The tests fail.
- [ ] **Step 3:** Implement. The last fetch error is kept in memory per repo (`gitread.Repos`), not in the database.
- [ ] **Step 4:** The tests pass.
- [ ] **Step 5:** Commit, then the full gate (`make ci`, `make e2e`).

## Review Focus

1. **A hostile repo:** config, hooks, filters or file names that try to make Vaquero run code or reach the network. Nothing runs and nothing reaches out except an explicit fetch. Tests: Task 1, `TestHostileRepoConfig` and `TestGitreadCommandsAreReadOnly`.
2. **A pinned file changes, is renamed, or is deleted:** readiness says which, never silently passes, and judges by content. Tests: Task 5.
3. **A slow or missing remote, or a remote that wants a password:** nothing hangs and nothing prompts. The page says when the fetch was and why it failed. Tests: Task 1 `TestFetchNeverPrompts`, and Task 6.
4. **An agent pointing a project at another repo or branch** to make pins pass: refused (P2-R1). Test: Task 2 `TestRepoSettingsOwnerOnly`.
5. **A board with many todo cards in a workflow project:** git runs once per project per render, not once per card. Test: Task 5 `TestReadinessGitBatched`.
