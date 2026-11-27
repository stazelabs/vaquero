# Audit pack: docs

Vaquero's own docs pack, read by the audit workflow prompt (`vaquero workflow audit --project VAQ --kind docs`). Vaquero's documentation for people is the help articles, which ship in the binary (`/help`, `vaquero docs`) and are held by tests; the repository adds a README, a contributing guide and the release steps. CLAUDE.md and `.claude/` are the instructions for agents: read as the contract here, never edited by this audit.

## Standards

[CLAUDE.md](../../CLAUDE.md) in full, and [.claude/rules/copy-voice.md](../../.claude/rules/copy-voice.md). CLAUDE.md's package map, "Commands" and "Hard invariants" are the ground the documents are checked against, and copy-voice.md's Help articles section is the articles' contract.

Both are frozen for this audit (owner ruling for Run R: they are the controller's to change). Drift found in them, a package missing from the map, a route or command named wrongly, is filed as a ticket naming the line and the fix, never edited.

## Ground truth

Run from the repository root; none needs a server:

```
go list ./...                                             # packages
grep -nE 'r\.(Get|Post|Put|Patch|Delete|Handle)\(' internal/web/router.go    # pages and API routes
ls internal/db/migrations/                                # migrations
ls .github/workflows/                                     # workflows
make                                                      # every make target, with its description
ls internal/help/articles/                                # help articles
grep -n -A40 'var toolNames' internal/mcpserver/tools.go  # the MCP tools
go run ./cmd/vaquero help                                 # the CLI's commands and usage
cat coverage-floors.txt go.mod                            # floors, the Go version and dependencies
git tag --list 'v*'                                       # releases
```

`go run ./cmd/vaquero docs SLUG` prints an article as the binary ships it.

## Surfaces

| Surface | Treatment |
|---|---|
| `internal/help/articles/*.md` | In scope, under Extra rules: commands, flags, routes, tools, settings, limits, paths and flows, against the code |
| [README.md](../../README.md) | In scope: the status line, install and build commands, the default data directory, links |
| [CONTRIBUTING.md](../../CONTRIBUTING.md) | In scope: the gate commands, the e2e note, the workflow and commit rules |
| [docs/RELEASING.md](../../docs/RELEASING.md) | In scope: the release steps against `.github/workflows/release.yml`, `formula.yml`, `.goreleaser.yaml`, `scripts/publish-formula.sh` and the Makefile's `fixture` and `formula` targets |

## Frozen

- `CLAUDE.md` and everything under `.claude/`: drift there is a ticket.
- `docs/superpowers/specs/` and `docs/superpowers/plans/`: dated specs and plans, history.
- `docs/BACKLOG.md`: the record up to Phase 3b.
- `.vaquero/audits/`: the packs, edited only when the owner asks.
- `internal/workflow/prompts/` and `internal/workflow/packs/`: product text held by tests; drift is a ticket.
- `testdata/` (the upgrade fixtures in `testdata/upgrade/` above all) and every `testdata/` golden.
- Generated files: `*_templ.go`, `internal/db/gen/`, `internal/web/static/css/tailwind.css`, `internal/web/icons/ui_gen.go`, `internal/legal/third_party.json`.
- `LICENSE`, `NOTICE`, `internal/legal/vendor/`.
- `packaging/homebrew/vaquero.rb.tmpl` and its golden: code, held by `scripts/formula`'s test.

## Partitions

1. **Getting started**: `welcome.md`, `installing-and-upgrading.md`, `data-and-backups.md`, with `README.md`. Check against `cmd/vaquero` (`serve.go`, `local_cmds.go`, `brew.go`, `logs.go`, `compact.go`, `attachments_move.go`, `cli_import.go`, `cli_export.go`), `internal/config` and `internal/db` (`backup.go`).
2. **Using**: `projects-and-tickets.md`, `the-board.md`, `search.md`, `keyboard-shortcuts.md`, `markdown.md`, `editing-and-conflicts.md`, `attachments.md`, `repositories.md`, `your-profile.md`. Check against `internal/web/templates`, `internal/web/handlers`, `internal/board`, `internal/core`, `internal/search`, `internal/markdown`, `internal/gitread` and `internal/web/static/js/app.js`.
3. **Command line**: `cli.md`. Check every row against `cmd/vaquero` (`cli.go`'s `cliCommands`, each command's usage line and flags, exit codes 0, 1, 2 and 3).
4. **API and agents**: `api.md`, `api-tokens.md`, `mcp.md`, `agents-and-claims.md`, `agent-workflow.md`. Check against `internal/web/router.go` (`mountAPI`), `internal/web/handlers/api*.go`, `internal/api`, `internal/mcpserver/tools.go`, `internal/core` (claims, plans, decisions, readiness rules) and `internal/readiness`.
5. **Repository docs**: `CONTRIBUTING.md` and `docs/RELEASING.md`, against the Makefile, `.github/workflows/`, `.goreleaser.yaml` and `scripts/`.

## Voice

copy-voice.md applies to every surface: sentence case in titles and headings; no exclamation points, no emoji, no "Oops", "let's" or "you're all set"; few em dashes; the product is "Vaquero" in prose and `vaquero` as a command; plain nouns (project, ticket, status, tag, comment, milestone); never name a competing product (the one exception: the `vaquero import linear` lines in the cli, api and data-and-backups articles) and never name a model provider; real counts, never "ticket(s)"; no vendor-speak ("powerful", "seamless"). Write for someone using Vaquero, not someone reading the code: say what happens, then how.

## Extra rules

Help articles are what a person using Vaquero reads, and they ship in the binary, so the bar is higher.

- **Shape.** `internal/help/articles/{slug}.md`: front matter between `---` lines with `title`, `section` (`getting-started`, `using`, `command-line` or `api-and-agents`), `summary` (one sentence) and `order`, then a Markdown body with `##` headings. The file name is the slug and the URL (`/help/{slug}`, `vaquero docs {slug}`): never rename one. Keep `order` ascending within a section.
- **Anchors.** Headings get automatic ids, and pages and other articles link to them (`/help/slug#anchor`; `TestHelpLinksResolve`, `TestContextualHelpLinks`). Changing a heading's words changes its anchor: grep for the old anchor first, and leave the heading alone when anything links to it.
- **Tests that pin the articles**, run as soon as you touch one: `TestArticlesComplete`, `TestHeadings`, `TestHelpLinksResolve`, `TestMCPToolsDocumented`, `TestAPIErrorCodesDocumented`, `TestLocalModeTrustIsDocumented`, `TestMarkdownArticleExamples`, `TestWelcomeNamesEveryTopBarLink`, `TestInstallArticleLeadsWithFirstRun`, `TestAgentTokensDocumented` (`internal/help`); `TestCLICommandsDocumented`, `TestCLISubcommandsDocumented`, `TestCLIFlagsDocumented` (`cmd/vaquero`: every command, sub-verb and flag has its line in `cli.md`); `TestAPIRoutesDocumented` (`internal/web`: every `/api/v1` route as `` `METHOD /path` `` in `api.md`); `TestShortcutsDocumented`, `TestEveryHandledKeyIsListed`, `TestProjectGlyphsDocumented` (`internal/web/templates`). A test that fails on an article's fact means the code moved: correct the article, never the test.
- **Rendering.** goldmark with tables only: no typographer and no autolinks, so `--` typed as a dash reaches the reader as two hyphens (write a comma or a period) and a bare URL isn't a link. Raw HTML is shown escaped.
- **Limits and numbers** an article states (10 MB attachments, 256 KB icons, lease lengths, 7 snapshots of each kind, 30 days of MCP log, 512 KiB file view, fetch minutes 5 to 1440) are checked against the constant in code, by name.
- **Agents read the articles too** (`/help/{slug}.md`, `vaquero docs`): an example command must run as written.

## Verify

- **A help article changed:** `go test ./internal/help/... ./internal/web/... ./cmd/vaquero/`, which runs every test that reads the articles. Help articles don't feed the stylesheet (`input.css` scans only templates and `app.js`), so no generated file moves.
- **Only README.md, CONTRIBUTING.md or docs/RELEASING.md changed:** check every relative link and anchor you touched resolves, and that each command you changed runs as written (a make target with `make` listing it, a CLI command with `go run ./cmd/vaquero help`). No test reads them.
- **Anything else moved** (this audit should never touch code, but if a step ran a generator): the full gate, `make gen && make fmt && make vet && make lint && VQ_GIT_REQUIRE=1 make test`, and `git status` must show only the documents you meant to change.
