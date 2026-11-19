# Vaquero design

Date: 2026-11-18. Status: awaiting approval.

## Intent

Vaquero (Spanish for cowboy) herds the chaos. It is an agent-native, local-first, lightweight ticket tracker with the coordination primitives agents need to share a backlog without colliding.

Why it exists:

1. **Control and autonomy.** The tracker, its data and its upgrade schedule belong to the person running it.
2. **Cost.** A solo developer pays nothing. A shared instance costs only what the cloud platform charges.

It ships as a single Go binary. One person runs it on a laptop ("army of one"). A small group deploys the same binary to cheap cloud infrastructure, with Fly.io as the reference target.

## Goals

- Track work with projects, tickets, sub-tickets, statuses, tags, running comments, links between tickets and optional milestones.
- Serve three kinds of user equally: a person in the web UI, a person or script in the CLI, and an agent over MCP.
- Let several agents work one backlog safely: a ready queue, atomic claims with expiring leases, dependencies, per-agent identity and a full activity log.
- Understand git: branches, commits, pull requests and tags link to tickets and can move them.
- Upgrade seamlessly. A user who installed an old release picks up a new one without losing data or reading migration notes.
- Stay simple. The feel is a board with cards, closer to a kanban tool than an enterprise tracker.
- Offer optional LLM assists that ride along when the user supplies a key and vanish when they do not.

## Non-goals

- Syncing data between a local instance and a hosted one. Data moves by export and import.
- Launching or supervising agents. Vaquero coordinates; the user's own harness runs them.
- Teams, sprints, cycles, time tracking, custom fields, workflow designers, reporting dashboards.
- More than one workspace per instance.
- Forges other than GitHub (the local git features work with any remote).
- Telemetry of any kind.

## Anti-goals

Vaquero has failed, even if it works, when:

- an upgrade loses or corrupts data, or needs manual steps;
- any feature requires an LLM key, an email provider or another outside account to stand up a working instance;
- an agent has to guess at vocabulary (core nouns stay plain: project, ticket, status, tag, comment);
- the web UI, CLI and MCP disagree about what an operation does;
- the documentation names competing products.

## Constraints

- Follow the stack of the sibling projects `healthcheck-email` and `startcase`: Go 1.27 with `CGO_ENABLED=0`; chi v5; templ; htmx plus one hand-written `app.js`; Tailwind v4 CLI with token colours; `modernc.org/sqlite`; sqlc; goose migrations embedded and run on boot; stdlib `testing`; generated output committed; no JS build step in the Docker build.
- Port code from `healthcheck-email` using `startcase/docs/superpowers/plans/2026-11-17-porting-map.md` as the guide.
- Module `github.com/stazelabs/vaquero`, command `cmd/vaquero`, env prefix `VQ_`.
- Public open source under Apache 2.0.
- The gate is `make gen && make fmt && make vet && make lint && make test && make cover-check`.

## Approaches

Three shapes were considered for how clients reach the data.

1. **Everything through one HTTP API (chosen).** Web UI, CLI and MCP are all clients of one server. Locally the server binds loopback and the CLI starts it on demand. One code path, live UI updates for free, identical behaviour local and hosted. Cost: a background process on the laptop.
2. **Direct database access locally.** CLI and stdio MCP open the SQLite file when no remote is configured. Nothing to keep running, but two client backends must stay identical and the UI must poll for outside changes.
3. **Server required, started by hand.** Same single path as the first, without auto-start. Simplest, but agents fail whenever the server is down.

The user picked the first: uniform behaviour matters more than avoiding a background process.

Related choices the user made, each against named alternatives:

| Topic | Chosen | Rejected |
|---|---|---|
| Local vs hosted | Two modes of one binary, no sync | Offline-first sync; local only for now |
| Orchestration | Coordination primitives | Tracker only; active dispatcher |
| Git | GitHub plus local git | Local git only; multi-forge adapters |
| Hierarchy | Projects, tickets, sub-tickets | Flat; teams and cycles |
| Hosted sign-in | Password plus optional passkeys | GitHub sign-in; magic links; both |
| LLM assists | Draft/decompose, triage, summaries, natural-language search | (all four selected) |
| Licence | Apache 2.0 | MIT; AGPL 3.0 |

## Design

### Shape

```
web UI (templ+htmx) ─┐
REST /api/v1 ────────┼─> internal/core ─> sqlc ─> SQLite (WAL)
MCP  /mcp ───────────┘        │
                              └─> events (seq) ─> SSE, webhooks, activity feed
vaquero CLI ──HTTP──> /api/v1        vaquero mcp (stdio) ──HTTP──> /mcp
```

`internal/core` owns every mutation and writes the matching `events` row in the same transaction. Web handlers, REST handlers and MCP tools call `core` and nothing else for writes. This departs from the siblings, where handlers call sqlc directly; with four surfaces that would let them drift. (Builder's call, proposed here: reads may use sqlc reader queries directly where no rule applies.)

### Modes

`VQ_MODE` selects the mode.

- **local** (default). Binds `127.0.0.1`. Data lives in the platform data directory, overridable with `VQ_DATA_DIR`. One implicit owner; no sign-in screen. Because any website can try to reach loopback, the server checks the Host header against an allowlist, applies stdlib cross-origin protection, and requires a token on `/api/v1` and `/mcp`. The token is generated on first boot into a file with mode 0600 in the data directory; the CLI and the stdio bridge read it automatically.
- **hosted**. Sign-in required. Users, roles (owner, member, viewer), invite links, secure cookies, rate limits. The server refuses to boot without real secrets.

### Data model (first cut)

- `actors`: the one identity table. Kind is human or agent; it has a display name. `tickets.assignee`, `tickets.claimed_by`, `comments.author` and `events.actor` all reference it, so an agent can be an assignee. It exists from Phase 1, seeded with the local owner.
- `users` (Phase 4): sign-in details for a human actor. `sessions`, `credentials` (passkeys).
- `api_tokens`: the actor it acts as, display name, scope (read, write, admin). Only an HMAC and a prefix are stored.
- `projects`: name, key (such as `VAQ`), ticket counter, archived flag.
- `statuses`: per project. Each has a fixed category (backlog, todo, in progress, in review, done, cancelled); name and order are editable.
- `tickets`: project, number, title, markdown body, status, priority, assignee, parent ticket, milestone, board position, `claimed_by`, `claim_expires_at`, timestamps.
- `tags`, `ticket_tags`, `comments`, `milestones`.
- `ticket_links`: blocks, relates, duplicates.
- `repos` and `git_links` (branch, commit, pull request, tag; with state). These arrive in a Phase 5 migration, not before (builder's call).
- `events`: monotonically increasing sequence, actor, subject, kind, JSON payload.
- `attachments`: metadata only; bytes go through the `blobs` package (directory or S3-compatible).
- `settings`.
- An FTS5 index over tickets and comments.

IDs are ULIDs. Instants are stored as text and formatted through `timefmt`.

### Coordination

**Agent identity** (decided by the user):

- In local mode the machine is trusted. An agent declares its own name (a `--as` flag on the CLI, a setting on the stdio bridge, a request header underneath) and vaquero creates that agent actor on first use. All local clients share the one local token. With no name given, the caller is the owner.
- In hosted mode each agent has its own minted token, and the token fixes the actor. A self-declared name is ignored.

**Claims** (decided by the user):

- **Ready queue:** tickets in a todo-category status, with no open blockers and no live claim. A ticket with open sub-tickets is a container and is not itself ready (builder's call).
- **Claim:** atomic. It sets the assignee to the claiming actor, moves the ticket to the project's first in-progress status, records the status it came from, and returns a lease with an expiry. A second claimant loses cleanly.
- **Heartbeat** extends the lease. **Release** gives it up and leaves status and assignee as they are.
- **Expiry:** a background job returns the ticket to the status it came from, clears the assignee, and writes an event saying the lease lapsed.
- People assign themselves in the ordinary way, with no lease. A ticket assigned to a person is not in the ready queue.
- `vaquero start` on a ticket with a live claim by another actor fails with a clear message unless forced (builder's call).
- Every action is attributed to its actor in `events`, so a person can see which agent did what.

### Surfaces

- **Web UI:** project list, board with drag to move and reorder, list view with filters, ticket page with markdown and comments, activity feed, search, keyboard shortcuts, live updates over SSE fed from `events`.
- **REST `/api/v1`:** additive only once released.
- **CLI:** stdlib dispatch with `run(args, stdout, stderr) int`. An HTTP client with profiles (the local instance and named remotes). Starts the local server on demand. Every command accepts `--json`.
- **MCP:** Streamable HTTP at `/mcp` using the official Go SDK, with read and write tools that share the REST readers. `vaquero mcp` is a thin stdio bridge to it. The tool list is frozen by a test. Authentication is a bearer API token in both modes; no OAuth authorisation server is planned (builder's call).

### Git

- A project maps to one or more repositories.
- Branch naming `vaq-12-slug`. `vaquero start VAQ-12` creates the branch and claims the ticket. The current ticket is inferred from the branch name.
- References such as `Fixes VAQ-12` in commits and pull requests create links.
- Hosted instances receive GitHub webhooks. Local instances poll through `gh`.
- Who observes the repository, and with which GitHub credential, is settled in the Phase 5 plan. Starting position (builder's call): the CLI reports local git state from the working directory, so the server never needs a checkout path; hosted instances use a per-repository webhook with a shared secret and make no outbound GitHub calls.
- Per-project rules move a ticket when its pull request opens or merges. Release tags attach to milestones.

### Upgrades

- Semver releases built by goreleaser: binaries, checksums and a Homebrew tap. The container image moves to Phase 4, when hosted mode makes one useful.
- Upgrades go through Homebrew: `brew upgrade vaquero` (decided by the user on 2026-11-20, replacing a `vaquero upgrade` command, so Vaquero itself never calls out). The next CLI command restarts a local server older than itself; `brew services` keeps Vaquero running at login, in place of a `vaquero service install` command. Linux users without Homebrew install from the release archive and `checksums.txt`.
- Before applying pending migrations on boot, the server takes a `VACUUM INTO` snapshot and keeps the last few.
- The server refuses to open a database newer than itself.
- Migrations are append-only and forward-only. A destructive change ships as an additive step in one release and the removal in a later one.
- The CLI sends its version; the server answers with its own and a minimum client version. A mismatch prints a plain instruction to upgrade.
- CI keeps a fixture database from every release and proves each migrates to head.
- No update-available notice: `brew outdated` covers it (2026-11-20).
- Full export and import (builder's calls): an archive of JSON plus attachment bytes. It includes projects, tickets, comments, tags, links, milestones, actors, events and settings; it leaves out sessions, token hashes and live claims. Import restores into an empty instance only, bypasses `core`, and keeps IDs, project keys and ticket numbers. Actors arrive without sign-in details; an owner attaches users to them afterwards.
- The importer from the current tracker also keeps project keys and ticket numbers, so existing branch names and commit references still resolve.
- Supported platforms for the local binary are macOS and Linux (decided by the user). Windows users run it under WSL or use a hosted instance; native Windows may follow.

### Hosted sign-in

Passwords hashed with argon2id; optional passkeys through `go-webauthn`; sessions through scs with the SQLite store ported from the siblings. With no email provider configured, an owner resets a password from the admin page or with `vaquero admin`. This departs from the siblings, which use magic links only.

### LLM assists

`internal/llm` is ported from the siblings and is the only path to a model. The key is instance-wide in local mode and per user, sealed with `secretbox`, in hosted mode. With no key, no assist appears in any surface. Every assist proposes; the user confirms.

- Draft and decompose: rough note to ticket; large ticket to sub-tickets with dependencies.
- Triage: suggested tags, priority, project; likely duplicates.
- Summaries: comment threads, project digests, release notes.
- Natural-language search, compiled to the same filter the UI uses.

## Phases

Each phase ends with the gate green and something usable, and gets its own plan when it starts.

0. **Foundation.** Licence, README, CLAUDE.md and rules, ported scaffold, config with modes, database open and migrate, `vaquero serve`, `/healthz`, `vaquero version`, Makefile, CI workflows.
1. **Core tracker.** Schema, `internal/core`, events, and the web UI described above, in local mode.
2. **API, CLI, MCP, coordination.**
3. **Releases and upgrades.** First public release. Also export and import, an importer from the tracker currently in use, and an adapter so the `stazelabs` agent-workflow skills can drive vaquero. Vaquero's own tickets move into vaquero.
4. **Hosted mode.** Users, sign-in, roles, Dockerfile, `fly.toml`, ops docs, scheduled snapshot upload.
5. **Git integration.**
6. **Notifications and attachments.** In-app inbox, outgoing webhooks, optional email, files.
7. **LLM assists.**

## Left to the builder

- Package boundaries inside `internal/` beyond `core`, following the siblings' rule of one package per concern and no empty packages.
- Exact REST paths, CLI command names and MCP tool names, provided the three stay parallel.
- Board ordering scheme, markdown renderer and sanitiser, SSE fan-out.
- Local service management details per operating system.
- Default statuses seeded for a new project.
- Lease duration and snapshot retention defaults.

## Open items

- The siblings never trigger CI on push or pull request. A public repository may need that once outside contributors arrive.
- Check the name `vaquero` for collisions on GitHub, Homebrew and the container registry before the first release.
- Whether local mode should offer a LAN bind with sign-in. Current answer: use hosted mode.
