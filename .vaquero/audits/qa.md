# Audit pack: qa

Vaquero's own qa pack, read by the audit workflow prompt (`vaquero workflow audit --project VAQ --kind qa`). Two Vaquero servers are in play on this machine: the owner's tracker (the Homebrew install on port 7878, where findings are filed through the MCP tools) and the scratch server this audit boots from the branch under test. They run the same program, so a slip of one environment variable points a command at the owner's data. Every rule below exists to keep the two apart.

## Standards

Read in full before walking:

- [CLAUDE.md](../../CLAUDE.md): "Hard invariants" (local mode, the Host check, the CSP, theme tokens, copy, the agent rules, readiness and the workflow-off rule) and the package map entries for `internal/web`, `internal/web/handlers`, `internal/web/templates`, `internal/web/static` and `cmd/vaquero`.
- [.claude/rules/ui-theming.md](../../.claude/rules/ui-theming.md), [.claude/rules/copy-voice.md](../../.claude/rules/copy-voice.md), [.claude/rules/handlers.md](../../.claude/rules/handlers.md) (feedback: 303 after a POST, success as a toast, a missing entity redirects with a toast, the themed 413 page) and [.claude/rules/security.md](../../.claude/rules/security.md).
- The continuity pack's Known non-findings ([continuity.md](continuity.md)); they apply here too.
- The help articles under `internal/help/articles/` describe intended behaviour; a page that does something other than its article says is a finding against one of them (settle which against the code).

## Never targets

A command whose output names any of these is a stop.

- **Port 7878** on 127.0.0.1, localhost or ::1: the owner's tracker. Never open it in the browser, never curl it, never point a scratch command at it.
- **The owner's data directory**: `~/Library/Application Support/vaquero` on macOS, `${XDG_DATA_HOME:-~/.local/share}/vaquero` on Linux, and everything in it (`vaquero.db`, `token`, `backups/`, `vaquero.env`, `vaquero.log`, `attachments/`).
- **The owner's saved CLI servers**: `config.json` in the user config directory (`~/Library/Application Support/vaquero/config.json` on macOS, `~/.config/vaquero/config.json` on Linux). Never run `vaquero server new`, `vaquero server use` or `vaquero server delete`, and never pass `--server` with a saved server's name.
- **The Homebrew service** (`brew services`, labels `sh.brew.vaquero`, `homebrew.mxcl.vaquero`, `homebrew.vaquero`) and its log, `$(brew --prefix)/var/log/vaquero.log`. Never run `brew` at all.
- **The installed `vaquero`** (Homebrew's, on PATH as `/opt/homebrew/bin/vaquero` or similar) and any `vaquero` command run without the scratch environment: it resolves the saved profile, which is the tracker, and starts or restarts the owner's server.
- **The developer's own server**: `make dev` on port 7879, its data `./data`, `.env.local`, `tmp/dev.log`. Never run `make dev` or `make db-reset`.
- **Any git remote.** The scratch repository has none; never add one, never fetch.

## Boot

Each Bash call starts a fresh shell: nothing exported in one survives to the next, and a bare `vaquero` there is Homebrew's, talking to the tracker. So the scratch environment lives in a file, `tmp/qa/env` under the repository root (`tmp/` is ignored by git), and every command after Boot follows one pattern that fails closed:

- **Every fenced block opens with `. /ABSOLUTE/REPO/tmp/qa/env || exit 1`**, the repository root written out in full. The file exits when its scratch binary is missing, so a stale or wrong env stops the block.
- **The scratch binary is always called by its full path, `"${QA:?}/bin/vaquero"`**, never as a bare `vaquero`, and the scratch directory and port only as `${QA:?}` and `${PORT:?}`. Without the env sourced, `${QA:?}` aborts the command before anything runs. This holds for one-line commands in prose too. `TestQAPackRunsOnlyTheScratchBinary` holds the pattern.

The env file also sets `VAQUERO_SERVER=local` with the scratch `VQ_ADDR` and `VQ_DATA_DIR`, clears every other setting that could leak in, and changes to the scratch directory (so `serve` never reads the repository's `.env.local`).

Run once, from the repository root, as one command:

```bash
set -eu
repo=$(git rev-parse --show-toplevel); cd "$repo"
[ -z "$(git status --porcelain)" ] || { echo "The tree isn't clean; stop."; exit 1; }
make build                                  # gen, then bin/vaquero (CGO_ENABLED=0, a 0.0.0-dev build)
[ -z "$(git status --porcelain)" ] || { echo "make build changed generated files; stop and tell the owner."; exit 1; }
qa=$(mktemp -d "${TMPDIR:-/tmp}/vaquero-qa.XXXXXX")   # new and empty; the data directory is $qa/data
port=7880
while [ "$port" = 7878 ] || [ "$port" = 7879 ] || lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; do port=$((port+1)); done
mkdir -p "$qa/bin" tmp/qa && cp bin/vaquero "$qa/bin/vaquero"
cat > tmp/qa/env <<EOF
[ -x "$qa/bin/vaquero" ] || { echo "No scratch server set up; stop."; return 1 2>/dev/null || exit 1; }
unset VQ_MODE VQ_LOG_FORMAT VQ_LOG_LEVEL VQ_BLOB_STORE VQ_BLOB_DIR VQ_S3_ENDPOINT VQ_S3_BUCKET VQ_S3_ACCESS_KEY_ID VQ_S3_SECRET_ACCESS_KEY VQ_S3_REGION VAQUERO_AGENT VAQUERO_PROFILE VAQUERO_MODEL
export VQ_ADDR=127.0.0.1:$port VQ_DATA_DIR=$qa/data VAQUERO_SERVER=local PATH=$qa/bin:\$PATH
export QA=$qa PORT=$port
cd $qa
EOF
. tmp/qa/env
nohup "$qa/bin/vaquero" serve >>"$qa/serve.log" 2>&1 &
curl -fsS --retry 10 --retry-connrefused --retry-delay 1 "http://127.0.0.1:$port/healthz"; echo    # waits up to 10 s for ok
lsof -nP -tiTCP:"$port" -sTCP:LISTEN > "$repo/tmp/qa/pid"    # the listener's PID, for cleanup
echo "qa=$qa port=$port pid=$(cat "$repo/tmp/qa/pid")"
```

Then check:

- **Health:** `curl -fsS "http://127.0.0.1:${PORT:?}/healthz"` prints `ok` (`GET /healthz`, `handlers.Healthz`), and `tmp/qa/pid` holds one PID. If either fails, read `${QA:?}/serve.log` and stop.
- **Which server:** `"${QA:?}/bin/vaquero" version` prints `0.0.0-dev` and `"${QA:?}/bin/vaquero" logs --path` prints a path under the scratch directory. Anything naming port 7878 or the owner's data directory is a stop.
- **Log:** `${QA:?}/serve.log`, slog text: `time=… level=INFO msg=request method=GET path=/p/QA status=200 dur_ms=3 request_id=…`. Requests to `/healthz` and `/static/` log at debug and don't appear. A server the CLI starts by itself (when the first one died) logs to `${QA:?}/data/vaquero.log` instead; check both.
- **Sign in:** none. Local mode has no sign-in: the browser is the owner at `http://127.0.0.1:PORT/`, with the scratch port. The CLI reads the local token from the scratch data directory by itself; never print it.
- **A second actor:** agents, not accounts. A scratch command with `--agent qa-bot` acts as an agent called qa-bot (made on first use). There is no second person and no elevation.

Seed the scratch data through the scratch CLI, everything prefixed `QA`. `QAX` is the throwaway project for the flows that re-key tickets (a move to another project, a key change), so the tickets later slices name keep their keys:

```bash
. /ABSOLUTE/REPO/tmp/qa/env || exit 1
"${QA:?}/bin/vaquero" project new QA "QA project" --description "Scratch data for the qa audit"
"${QA:?}/bin/vaquero" ticket new QA "QA first ticket" --body "Some **Markdown** and a [link](/help/markdown)." --priority high --tag qa
"${QA:?}/bin/vaquero" ticket new QA "QA second ticket" --status Todo --tag qa
"${QA:?}/bin/vaquero" ticket new QA "QA sub-ticket" --parent QA-1
"${QA:?}/bin/vaquero" ticket link QA-2 blocks QA-3
"${QA:?}/bin/vaquero" comment new QA-1 "QA comment"
"${QA:?}/bin/vaquero" milestone new QA "QA milestone" --due 2026-12-31
"${QA:?}/bin/vaquero" claim QA-2 --agent qa-bot
"${QA:?}/bin/vaquero" project new QAW "QA workflow"
"${QA:?}/bin/vaquero" project edit QAW --workflow on
"${QA:?}/bin/vaquero" ticket new QAW "QA planned ticket" --status Todo
"${QA:?}/bin/vaquero" ticket new QAW "QA unplanned ticket" --status Todo
"${QA:?}/bin/vaquero" project new QAX "QA throwaway"
"${QA:?}/bin/vaquero" ticket new QAX "QA ticket to move"
"${QA:?}/bin/vaquero" ticket new QAX "QA sub-ticket to move" --parent QAX-1
```

For the repository slice, make a local repository with no remote and attach it:

```bash
. /ABSOLUTE/REPO/tmp/qa/env || exit 1
git init -q -b main "${QA:?}/repo" && mkdir -p "${QA:?}/repo/docs" && printf '# QA spec\n\n## Scope\n\nText.\n' > "${QA:?}/repo/docs/spec.md"
git -C "${QA:?}/repo" add . && git -C "${QA:?}/repo" -c user.name=QA -c user.email=qa@example.invalid commit -q -m "QA spec (QAW-1)"
"${QA:?}/bin/vaquero" project edit QAW --repo "${QA:?}/repo"
"${QA:?}/bin/vaquero" ref new QAW-1 spec docs/spec.md --anchor scope
printf 'Plan for QAW-1.\n' | "${QA:?}/bin/vaquero" plan save QAW-1 -
```

## Areas

Routes are all in [internal/web/router.go](../../internal/web/router.go). `QA`, `QAW` and `QAX` are the seeded projects.

Flow slices:

| Slice | Flow | Walk |
|---|---|---|
| A | Projects | `/` as cards and as list (`?view=cards`, `?view=list`, remembered), a card's quick settings (name, description, icon choices loaded when opened), New project (`/projects/new`) with a bad key and a good one, an archived project's row |
| B | Focus | `/p/QA/`: Next, Waiting (with its reasons: blocked, claimed, open sub-tickets), In flight, Finished; the Mine and Agents chips, the Priority, Milestone and Tags menus, a `?tag=` naming no tag ("No tag X in this project."); the first-run steps on an empty project |
| C | Board | `/p/QA/board`: drag a card between columns and within one (it persists after reload), the backlog and cancelled rails, the milestones panel, filters kept across the Focus, Board and List tabs; a change made by a scratch CLI command shows on the open board without a reload |
| D | List | `/p/QA/list`: groups open and closed (kept across reloads), the Status menu, done and cancelled groups paged by 50 (seed more done tickets through the CLI to test it) |
| E | Ticket | New ticket (`/p/QA/new`), the ticket page `/t/QA-1`: edit and save, a stale save (edit `QA-1` through the CLI between loading the form and saving: expect the conflict banner, not a lost edit), comments (add, edit, delete behind the confirm dialog), links (the folded Add link form, suggestions by number and by title words), sub-tickets, history with Show changes for a description edit, a claimed ticket's note, Move to another project behind the confirm dialog (move `QAX-1`, with its sub-ticket, into `QA`; never a ticket another slice names), attachments (upload through `"${QA:?}/bin/vaquero" attachment new QA-1 FILE`; the gallery and rows, open and delete) |
| F | Project settings | `/p/QA/settings`: name and description, statuses (add, rename, up, down, delete; deleting one in use is refused), milestones (edit, close, reopen), tags (rename, rename onto another to see the merge button, delete), icon (monogram, a glyph), project links (depends on, relates, a loop refused), archive and restore. The key change behind the confirm dialog only on `QAX`, after slice E's move. Delete only a `QA-` project made for it |
| G | Agent workflow | `QAW`: the workflow switch and tier map in settings (owner only), the triage statuses button, readiness on `/t/QAW-2` ("Ask for plan approval.") and Ask, the plan on `/t/QAW-1` with Show changes after a second `"${QA:?}/bin/vaquero" plan save`, the approval answer form and Ask again, and Focus's Waiting reasons with the workflow on |
| H | Repository | `QAW` settings' Repository section (path, main branch, fetch minutes; never set a remote and never press Fetch now), `/p/QAW/files/docs/spec.md` with its heading anchor, the ticket's References and Commits sections, "Changed since approval" after an approved plan's file changes in a new commit |
| I | Search and help | the header search box (`/` focuses it), `/search` with ticket, comment and help hits, `/help`, an article, its `.md` form, the Markdown help link under a description box |
| J | Activity and profile | `/activity` (newest first, reorders folded), `/profile`: the display name (a name an agent has is refused), the avatar (upload with `"${QA:?}/bin/vaquero" me --avatar FILE`, then remove behind the confirm dialog) |
| K | Admin | `/admin` (version, data directory, Storage section), `/admin/backups` (Snapshot now, then delete that snapshot), `/admin/tokens` (create a `QA-` token and revoke it), `/admin/mcp` |
| L | Errors and chrome | the themed 404 for an unknown address (`/nope`), an unknown project or ticket (`/p/NOPE`, `/t/QA-999`) redirecting home with "No project NOPE." or "No ticket QA-999.", `/about/licenses`, the footer's version and links, a key typed in lower case or with leading zeros redirecting to the canonical one |

Cross-cutting passes. App specifics:

| Pass | What to exercise |
|---|---|
| N | every page above; a request to any host but the scratch one is a finding (system fonts only, nothing is downloaded) |
| O | the theme follows the system (`prefers-color-scheme` in `internal/web/static/css/input.css`); there is no toggle |
| P | phone widths matter: the e2e suite holds no page wider than 375 px, and below `md` (768 px) a rail opens over the columns; check Focus, the board, the list, a ticket, settings and the projects page |
| Q | the shortcuts dialog (`?`), `g f`, `g b`, `g l`, `/` for search, and that Enter on a focused link or button still follows it; the confirm dialog closes on Escape and Cancel |
| R | forms empty, oversized (a title past its limit, a 2 MB pasted body: the themed 413 page) and malformed (a bad project key, a bad date) |

### Pass S: owner and agents

Local mode has one person, the owner, so the browser has no lower role to walk. Gating here is owner against agent, and it lives in core, so walk it through the scratch CLI with `--agent qa-bot` (each line with the env sourced first), each expecting a refusal in plain words and exit code 1, then confirm in the browser that nothing changed:

- `"${QA:?}/bin/vaquero" project edit QAW --workflow off --agent qa-bot`: "Only the owner turns the agent workflow on or off."
- `"${QA:?}/bin/vaquero" project edit QAW --main-branch trunk --agent qa-bot`: "Only the owner changes the repo settings." (A setting sent unchanged is a no-op and passes.)
- `"${QA:?}/bin/vaquero" me --name QA --agent qa-bot`: "Only the owner edits the owner's profile."
- `"${QA:?}/bin/vaquero" decision answer ID Approve --agent qa-bot` with no `--words`, on a plan approval (`"${QA:?}/bin/vaquero" decision ask QAW "Approve the plan?" --ticket QAW-1 --approval plan` prints its ID): "Quote the owner's answer when relaying it."

And the Host check: `curl -s -o /dev/null -w '%{http_code}' -H 'Host: qa.example' "http://127.0.0.1:${PORT:?}/"` answers 403.

## Limits

- **No network.** Never set a remote, press Fetch now, run `vaquero fetch` or set fetch minutes on a repository with a remote. Never set `VQ_BLOB_STORE=s3` or any `VQ_S3_` key.
- **No offline maintenance**, even on scratch data: `vaquero import`, `vaquero backup restore`, `vaquero backup compact`, `vaquero attachment move` and `vaquero stop` are out of bounds (`stop` and the brew logic are a stop risk if an environment slips). Report those flows as untested.
- **Tokens:** a token the Admin tokens page shows once is never read back from the page, copied or echoed; revoke it straight after.
- **Destructive actions** (delete a project, delete a status, delete a snapshot, revoke a token) only on `QA-` data you made on the scratch server.
- **No exports to the repository**: `vaquero export` writes only under the scratch directory.
- **File pickers** are the owner's: upload through the CLI instead.
- Give a live change 5 seconds to arrive (the stream polls once a second) before calling it missing.

## Triage

- Environment noise: the first page load while `serve` migrates a new database; a toast left over from the previous page (the flash cookie lives 60 seconds).
- Scratch data, read directly and read-only: `sqlite3 -readonly "${QA:?}/data/vaquero.db"`, or the scratch CLI (`"${QA:?}/bin/vaquero" ticket show QA-1 --json`, `"${QA:?}/bin/vaquero" activity`).
- Log greps, slog text, from your mark (`wc -l < "${QA:?}/serve.log"` before the walk). Prove the pattern fires on a request you know succeeded first:

```
. /ABSOLUTE/REPO/tmp/qa/env || exit 1
tail -n +MARK "${QA:?}/serve.log" > "${QA:?}/run.log"
grep -c 'msg=request' "${QA:?}/run.log"                               # requests seen: must be above zero
grep -oE 'status=[0-9]+' "${QA:?}/run.log" | sort | uniq -c           # by status
grep -E 'status=5[0-9]{2}' "${QA:?}/run.log"                          # 5xx
grep -E 'level=(ERROR|WARN)' "${QA:?}/run.log"                        # errors, warnings, refused hosts
grep -c 'msg=panic' "${QA:?}/run.log"                                 # recovered panics
```

- A 304 for an attachment or icon and a 302 from `/thumbs/{id}` to the original are normal.

## Code map

- Routes: `internal/web/router.go` (`NewRouter`, `mountAPI`). Handlers: `internal/web/handlers/` (the page handler by route, `board.go`, `tickets.go`, `projects.go`, `approvals.go`, `files.go`, `admin*.go`, `profile.go`, `stream.go`).
- Views: the `.templ` sources in `internal/web/templates/` (never the `*_templ.go` files, which are generated) and their helpers (`helpers.go`, `labels.go`, `activity.go`, `board.go`, `focus.go`, `filters.go`).
- Behaviour: `internal/web/static/js/app.js` (the confirm dialog, toasts, Sortable drags, the live stream, shortcuts, menus, uploads). Styles: `internal/web/static/css/input.css` (`tailwind.css` is generated).
- What a page shows comes from `internal/board` (Focus, board, list), `internal/readiness` (workflow-on readiness) and `internal/core` (every write and its refusal message).
- Never evidence: `*_templ.go`, `internal/db/gen/`, `internal/web/static/css/tailwind.css`, `internal/web/icons/ui_gen.go`, `internal/web/static/vendor/`.
- Name the CLAUDE.md "Hard invariants" rule or `.claude/rules/` line a defect breaks.

## Cleanup

```bash
. /ABSOLUTE/REPO/tmp/qa/env || exit 1
kill "$(cat /ABSOLUTE/REPO/tmp/qa/pid)"
lsof -nP -iTCP:"${PORT:?}" -sTCP:LISTEN || echo "Port free."    # must print only that
```

A server the CLI restarted on its own has another PID: stop it by the PID that lists, after checking the port is the scratch one. Then remove the scratch directory and the env file, in one command: `. /ABSOLUTE/REPO/tmp/qa/env && cd / && rm -rf "${QA:?}" /ABSOLUTE/REPO/tmp/qa`. Nothing was written to the tracker or the repository, so nothing else is removed.

## Known non-findings

- **No sign-in, no accounts and no roles** in local mode: the browser is always the owner. Hosted mode is refused until Phase 4.
- **The continuity pack's Known non-findings apply here too.**
- **A dev build's footer shows `0.0.0-dev`** and the commit.
- **A claim on someone else's ticket doesn't block an edit**: the save goes through with a note ("Claimed by X until 12:15 UTC"). Claims are advisory by design.
- **With the workflow off a project shows no tier and no readiness**, and the ticket form hides Tier and Model.
- **Readiness reads "Can't check its files: …"** for a ticket whose approval pinned repo files, when git is missing, the repository is unset or changed, or it can't be read: never a silent pass, by design. A ticket with nothing pinned runs no git and never reads it.
- **An old ticket key redirects (302) to the key now** after a move, and an old project key stops resolving after a key change (home, with "No project X.").
- **The Admin tokens page's POST answer shows the token** (the one answer that does), with the address replaced so a reload is a GET.
- **A non-image attachment downloads** instead of opening, and SVG is never shown inline: the attachment rules in security.md.
- **The shortcuts ignore keys typed into fields.**
