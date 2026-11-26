---
title: The API
section: api-and-agents
summary: The HTTP API at /api/v1 that the command line, scripts and agents use: endpoints, requests, answers and errors.
order: 1
---
Everything the pages and the command line do goes through this API. It speaks JSON over HTTP at `/api/v1` on the server, `http://127.0.0.1:7878/api/v1` on this machine by default.

## Signing requests

Send an API token in every request:

    curl -H "Authorization: Bearer vq_..." http://127.0.0.1:7878/api/v1/me

Make tokens on the Admin page or with `vaquero token create`; see [API tokens](/help/api-tokens). A missing, unknown or revoked token gets `401` with code `unauthorized`, the same answer for every reason.

On this machine, a request made with your token can act as an agent by naming it in a `Vaquero-Agent` header; the agent is created the first time its name is used, and its comments, edits and claims are recorded under that name. Agent names are 1 to 80 letters, digits, spaces, dots, dashes or underscores; any other name gets `400` with code `bad_request`. A token that belongs to an agent always acts as that agent, whatever the header says.

## Where a change came from

Every change records where it came from, as the caller reports it, in the event's `meta`: `surface` (`web`, `cli`, `api` or `mcp`), and, when sent, `client` (the `Vaquero-Client` header; the command line sends `vaquero-cli/VERSION`), `harness` (`Vaquero-Harness`, the coding agent's name and version), `model` (`Vaquero-Model`) and `run` (`Vaquero-Run`). Each value is cut to 80 characters, and one not sent is left out. They are recorded as reported, never checked, and never decide who made the change: the token and `Vaquero-Agent` do. Changes Vaquero makes on its own, such as a claim lapsing or the local token minted at boot, carry no `meta`, and neither do older changes.

## Conventions

- Projects are named by key (`VAQ`), tickets by key (`VAQ-12`), in any case.
- Statuses, assignees and milestones are sent and returned by name; names match in any case within the ticket's project. Names are returned as shown: the owner's [display name](/help/your-profile) when one is set. An assignee may be sent as either Owner or the display name.
- Priorities are numbers: 0 none, 1 urgent, 2 high, 3 medium, 4 low.
- Times are text in UTC, such as `2026-11-19T14:05:00.000Z`.
- A request body is one JSON object; unknown fields are refused, and bodies over 1 MiB get `413` (an upload may be 10 MB).
- Every answer carries `Vaquero-Version` (the server's version) and `Vaquero-Min-Client` (the oldest command line it works with).
- Lists take `limit` (most endpoints allow up to 500).

## Reading

| Request | Answer |
|---|---|
| `GET /api/v1/me` | The actor the token belongs to: `{"actor": {"name", "kind", "display_name", "shown_name"}, "version"}`. `name` is the account's name (Owner, or the agent's), `display_name` the owner's [display name](/help/your-profile) (`""` for none) and `shown_name` what answers and pages show |
| `GET /api/v1/projects` | Every project, with `open_count` and `icon` |
| `GET /api/v1/projects/{key}` | One project, with `workflow` (whether the [agent workflow](/help/agent-workflow) is on), `tier_map`, the [tier map](/help/agent-workflow#the-tier-map) in use: the project's own, else the default, and `links`, its [project links](/help/projects-and-tickets#project-links) as `[{"id", "kind", "direction", "key", "name"}]`: `kind` is `depends on` or `relates`, `direction` is `out` (this project depends on or relates to `key`) or `in` (`key` depends on or relates to this one). `GET /api/v1/projects` carries `links` too. A project with a [repository](/help/repositories) also has `repo`: `{"path", "main_branch", "remote", "remote_url", "fetch_minutes", "last_fetch_at"?, "available"}`, `remote_url` without any credentials and `available` false when git is missing or the repo can't be opened; a project without one has no `repo` |
| `GET /api/v1/projects/{key}/statuses` | Its statuses in board order, each with its `category`, `position` and `ticket_count` |
| `GET /api/v1/projects/{key}/milestones` | Its milestones, with `done` and `total` counts |
| `GET /api/v1/projects/{key}/tags` | The tags its tickets carry, by name, each with its `ticket_count` (this project's tickets only) |
| `GET /api/v1/projects/{key}/tickets` | Its tickets: filter with `status`, `open=1`, `tag`, `priority`, `mine=1`, `agents=1`, `milestone` (a name, or `none`), `assignee` (a name), `category` (`backlog`, `todo`, `in_progress`, `in_review`, `done` or `cancelled`); an unknown name or category is `422` |
| `GET /api/v1/tickets/{ticket}` | One ticket with `body`, `tier` and `model` (left out when unset), `sub_tickets` (each the same summary a ticket list gives), `links`, `comments`, `attachments` and `references` (the ticket's [references](/help/repositories#references), left out when it has none) |
| `GET /api/v1/tickets/{ticket}/attachments` | The ticket's files, oldest first |
| `GET /api/v1/tickets/{ticket}/commits` | The commits on the main branch that name the ticket (see [Commits](/help/repositories#reading-files-and-commits)): `{"commits": [{"hash", "short", "subject", "author", "at"}], "diffstat": {"added", "removed", "files"}, "released"?}`, newest first, at most 50, including those made under a key the ticket had before it moved; `released` is the first version tag that contains the newest. `409` with `no_repo` when the project has no repository, `no_git` when git isn't installed, `repo_unreadable` when the repository can't be read |
| `GET /api/v1/projects/{key}/files/{path}` | A file in the project's repository at the main branch, or at `at` (a branch, tag or commit hash): `{"path", "at", "blob", "size", "text"?, "too_large", "binary"?, "markdown"}`. `text` is left out when the file is over 512 KiB (`too_large`) or isn't text (`binary`); `markdown` says the page renders it as Markdown. A path or revision Vaquero won't pass to git (`..`, a leading `-`, `HEAD@{1}` and the like) is `422`; a missing file or revision `404`; no repository or git the same `409`s as commits |
| `GET /api/v1/projects/{key}/repo/status` | The repository as of its last fetch, read without any network: `{"main": {"ahead", "behind", "tracked"}, "unpushed_branches": [{"name", "ahead", "behind", "tracked"}], "last_fetch_at"?}`. `main` compares the main branch with the remote's (`tracked` false when the remote has none); `unpushed_branches` lists local branches the remote doesn't have or that are ahead of it, at most 50 (of the first 100 branches by name). The same `409`s as commits `last_fetch_error` (with `last_fetch_error_at`) says why this server's last fetch failed, until one succeeds. |
| `POST /api/v1/projects/{key}/repo/fetch` | Fetch the remote now, the one request that contacts a remote. Answers 200 with `{"last_fetch_at", "error"?}` when the fetch ran, even if it failed (`error` is git's first line); 409 `no_repo`, `no_git` or `no_remote` when there was nothing to fetch. Two requests at once share one fetch, and a fetch writes no event. |
| `GET /api/v1/tickets/{ticket}/plan` | The ticket's [plan](/help/agent-workflow#plans): `ticket`, `revision`, `body`, `updated_by` and `updated_at`; `404` when it has none |
| `GET /api/v1/tickets/{ticket}/plan/revisions/{n}` | Revision `n` of the plan, the current one included, in the same shape: `updated_by` and `updated_at` are who wrote that revision and when |
| `GET /api/v1/attachments/{id}` | A file's bytes, served the way [the page serves them](/help/attachments#how-files-are-served) |
| `GET /api/v1/search` | Search with `q`; each hit has a `snippet` with `[` and `]` around the matched words; a ticket found by its title alone has the start of its description, unmarked, or an empty snippet. At most `limit` hits (50 unless you ask, up to 500), best first; the answer's `Vaquero-Total` header is how many tickets matched in all |
| `GET /api/v1/export` | Everything, as a `.tar.gz` download (`application/gzip`); the owner's token only, `403` for anyone else, including an agent acting through it. `HEAD` makes the same checks and sends no body. See [Your data and backups](/help/data-and-backups#exporting-everything) |
| `GET /api/v1/events` | Changes, oldest first: `after=SEQ` for those after a sequence number (poll with the last `seq` you saw), or `newest=N` for the newest N. Reordering a card within its column is left out, though it still advances `seq`. A description edit's text is not included; its payload has `"body_diff": true` when the ticket page can show the diff. A plan's events (`plan.created`, `plan.revised`) carry only its `revision`, never its text. Each event's `meta` says where it came from (see [Where a change came from](#where-a-change-came-from)), left out when it has none. Each event's `text` is its sentence, as the activity page has it, naming a link's target by its current key, or "a deleted ticket". The answer's `Vaquero-Seq` header is the newest sequence number: when an `after=` answer is empty (it held only reorders, or nothing), continue from `Vaquero-Seq` |

A project's `icon` is `{"kind"}`: `monogram` (its two letters), `glyph` with `glyph` naming it, or `image` with `url`, the address of the image on the server (`/p/{key}/icon`). See [Project icons](/help/projects-and-tickets#project-icons).

An attachment has `id`, `name`, `content_type`, `size`, `uploaded_by`, `created_at` and `url`, the address of its bytes on the server (put it after the server's address, or use `GET /api/v1/attachments/{id}`).

A ticket in a list has `key`, `title`, `status`, `category`, `priority`, `assignee`, `tags`, `ready` and `version`. `ready` means the same as on [the board](/help/the-board#ready-tickets). In a project with the [agent workflow](/help/agent-workflow) on, it also has `tier`, and a todo ticket's has `readiness`, `{"ready", "reasons"}`, with `files` when it rests on pinned repo files; a ticket's own answer has `readiness` whatever its status. `ready` then needs both the board's rule and `readiness` (see [Readiness](/help/agent-workflow#readiness)).

## Writing

| Request | Body |
|---|---|
| `POST /api/v1/projects` | `{"key", "name", "description"}` |
| `PATCH /api/v1/projects/{key}` | Any of `{"name", "description", "workflow", "tier_map", "icon", "repo_path", "main_branch", "remote", "fetch_minutes"}`: the [repository](/help/repositories) fields `repo_path` (absolute; `""` clears it), `main_branch` and `remote` are the owner's alone, and `fetch_minutes` is 0 or 5 to 1440; `workflow` is `true` or `false`; `tier_map` is an object, harness to tier to model, that replaces the [tier map](/help/agent-workflow#the-tier-map), or `null` for the default; `icon` is `{"kind": "monogram"}`, `{"kind": "glyph", "glyph": "rocket"}` (a name from [the list](/help/projects-and-tickets#project-icons)), or `{"kind": "image"}` for the image already uploaded |
| `PUT /api/v1/projects/{key}/icon` | A multipart form with a PNG, JPEG or WebP image in a part named `file`; it becomes the project's icon, and the answer is the project. Up to 256 KB: more is `413`, and another type (SVG included) is `422` |
| `DELETE /api/v1/projects/{key}/icon` | Remove the uploaded icon image. If the project was showing it, it shows its monogram instead. The answer is the project; with no image stored nothing changes |
| `POST /api/v1/projects/{key}/key` | `{"key"}`, the new key |
| `POST /api/v1/projects/{key}/links` | `{"kind", "project"}`: `depends on` (or `depends-on`) or `relates`, and the other project's key. Answers `201` with the link as this project sees it |
| `DELETE /api/v1/projects/{key}/links/{id}` | Nothing; removes a link of the project's, from either end. Answers `204` |
| `POST /api/v1/projects/{key}/statuses` | `{"name", "category", "before", "after"}`: `before` or `after` names a status to put it next to; without one it goes after the last status of its category. Answers `201` with the status |
| `PATCH /api/v1/projects/{key}/statuses/{name}` | `{"name"}`, the new name; a status's category never changes. Answers with the status |
| `POST /api/v1/projects/{key}/statuses/{name}/move` | `{"before"}` or `{"after"}`: another status's name; the status moves there in one step. Answers with the status and its new `position` |
| `DELETE /api/v1/projects/{key}/statuses/{name}` | Nothing; a status no ticket uses, and not the last of its category. Answers `204` |
| `PATCH /api/v1/projects/{key}/tags/{name}` | `{"name", "merge"}`: renames the tag on this project's tickets (other projects keep it). A name another of the project's tags has is a merge, refused with `409` naming that tag in `message` and in `current` unless you send `"merge": true`. Answers with the tag the tickets now carry |
| `DELETE /api/v1/projects/{key}/tags/{name}` | Nothing; takes the tag off every ticket in the project. Answers `204` |
| `POST /api/v1/projects/{key}/milestones` | `{"name", "description", "due_on"}`; only `name` is required, and `due_on` looks like `2026-12-31`. Answers `201` with the milestone |
| `PATCH /api/v1/projects/{key}/milestones/{name}` | Any of `{"name", "description", "due_on"}`; `"due_on": ""` clears the date. Answers with the milestone |
| `POST /api/v1/projects/{key}/milestones/{name}/close` | Nothing; closes the milestone and answers with it |
| `POST /api/v1/projects/{key}/milestones/{name}/reopen` | Nothing; opens it again and answers with it |
| `POST /api/v1/projects/{key}/tickets` | `{"title", "body", "status", "priority", "assignee", "parent", "milestone", "tags", "tier", "model"}`; only `title` is required. `tier` is `design`, `light`, `standard` or `heavy`, `model` an exact model id (at most 80 characters); `""` clears either. `number` keeps a number from another tracker, as `vaquero import linear` does. Only the owner, acting directly, sends it (an agent's is `422`), and it is at most 1,000,000,000 and above every number the project has given (else `422`). Numbering goes on after it, and the project's `next_number` says where numbering is |
| `PATCH /api/v1/tickets/{ticket}` | Any of the ticket fields, plus `add_tags`, `remove_tags` and `version` |
| `POST /api/v1/tickets/bulk` | `{"items": [...]}`: move to another project, claim, renew (heartbeat), update, comment on, move and release up to 100 tickets in one call; see [Bulk changes](#bulk-changes) |
| `POST /api/v1/tickets/{ticket}/move` | `{"status", "before", "after"}`: a status and, optionally, the keys of the cards to land between (a key that names no ticket is refused) |
| `POST /api/v1/tickets/{ticket}/move-to` | `{"project", "version"}`: move the ticket, with its sub-tickets, to the project of that key; `version` is optional. Answers with the ticket under its new key; see [Moving a ticket to another project](/help/projects-and-tickets#moving-a-ticket-to-another-project) |
| `POST /api/v1/tickets/{ticket}/comments` | `{"body"}` |
| `PUT /api/v1/tickets/{ticket}/plan` | `{"body", "revision"}`: the whole plan, in Markdown (at most 200,000 characters). Leave out `revision` to write a ticket's first plan (`0` is `422`); to revise it, send the revision you read. The body limit is 1 MiB, as for every request. A stale or missing revision on a ticket that has a plan is `409` with the plan as it is now in `current`. Answers with the plan; saving the same text changes nothing |
| `PATCH /api/v1/comments/{id}` | `{"body"}`; your own comments only |
| `DELETE /api/v1/comments/{id}` | Nothing; your own comments only |
| `POST /api/v1/tickets/{ticket}/attachments` | A multipart form with the file in a part named `file`; answers `201` with the attachment. Up to 10 MB: more is `413` |
| `DELETE /api/v1/attachments/{id}` | Nothing; the person who attached it, or the owner |
| `POST /api/v1/tickets/{ticket}/refs` | `{"kind", "target", "anchor"?, "label"?}`: a [reference](/help/repositories#references). `kind` is `requirement`, `design`, `plan`, `spec` or `other`; `target` is a repo path (the project needs a repository) or an http or https URL. Answers `201` with `{"id", "kind", "target", "anchor"?, "label"?}` |
| `DELETE /api/v1/tickets/{ticket}/refs/{id}` | Nothing; removes one of the ticket's references. Answers `204` |
| `POST /api/v1/tickets/{ticket}/links` | `{"kind", "to"}`: `blocks`, `relates` or `duplicates`, and the other ticket's key |
| `DELETE /api/v1/links/{id}` | Nothing |
| `GET /api/v1/tokens` | Every token, without its secret |
| `POST /api/v1/tokens` | `{"name"}`; the answer holds the token, shown this once |
| `POST /api/v1/tokens/{prefix}/revoke` | Nothing; revokes the token with that prefix |

Statuses, milestones and tags are named in the path, and statuses in `before` and `after`, in any case. A name in the path that matches nothing is `404`, like any other missing resource; a name in `before` or `after` that matches nothing is `422` naming the field. A status's `category` is one of `backlog`, `todo`, `in_progress`, `in_review`, `done` and `cancelled` (`in progress` works too). Each change is recorded in the history as yours, or the agent's when you act as one.

A `PATCH` changes only the fields it sends. Send `"assignee": ""` (or `"milestone"`, `"parent"`) to clear one; `"tags"` replaces the whole list, while `add_tags` and `remove_tags` change it one tag at a time.

## Your profile

The owner's [profile](/help/your-profile): the display name every answer shows, and the avatar. Only the owner, not an agent acting through the `Vaquero-Agent` header or MCP, changes it; an agent gets `422`.

| Request | Body |
|---|---|
| `GET /api/v1/profile` | No body. The answer is `{"name", "display_name", "shown_name", "avatar"}`: `name` is the account's name, Owner; `display_name` what was chosen (`""` for none); `shown_name` what answers and pages show; `avatar` is `{"url", "etag"}`, or `null` while the initials show |
| `PATCH /api/v1/profile` | `{"display_name"}`: up to 80 characters, no line breaks or control characters, and not a name an agent has (in any case); `""` shows Owner again. The answer is the profile |
| `PUT /api/v1/profile/avatar` | A multipart form with a PNG, JPEG or WebP image in a part named `file`; it becomes the avatar, and the answer is the profile. Up to 256 KB: more is `413`, and another type (SVG and GIF included) is `422` |
| `DELETE /api/v1/profile/avatar` | No body. Remove the avatar, so the initials show again; the answer is the profile. With none stored nothing changes |

## Short answers

A ticket update, a move, a new comment, a claim, a heartbeat and a release each answer with the whole ticket (a new comment answers with the comment). Add `?view=summary` to get only the ticket's state instead: `key`, `status`, `category`, `version`, and `assignee`, `claimed_by` and `claim_expires_at` when they are set, plus the new comment's `id` for a comment. `?view=full` is the default; any other view is `422` and nothing is written. When someone other than you holds a claim on the ticket, each of these answers (and each item of a bulk update, a new link, an edited comment and an uploaded attachment) also carries `warning`, such as `"Claimed by builder until 12:15 UTC."`; the write still went through ([claims are advisory](/help/agents-and-claims#claims-are-advisory)). A new or edited comment's answer also names the ticket's `claimed_by` and `claim_expires_at` while a claim is held.

    POST /api/v1/tickets/VAQ-12/move?view=summary
    {"key": "VAQ-12", "status": "In review", "category": "in_review", "version": 7, "assignee": "builder"}

## Claiming work

Agents take work from the ready queue and hold it with a claim that expires unless renewed; [Agents and claims](/help/agents-and-claims) explains the rules. Each answer is the ticket, with `claimed_by` and `claim_expires_at` while a claim is held. A claim that can't be made or kept is `422` with a message naming why, such as who holds it and until when.

| Request | Body or query |
|---|---|
| `GET /api/v1/ready` | Ready tickets, urgent first, then high, medium, low and no priority, each in board order: `project=KEY` for one project, `limit=N` (default 50). In a project with the agent workflow on, only tickets that pass [readiness](/help/agent-workflow#readiness) |
| `POST /api/v1/tickets/{ticket}/claim` | `{"lease_minutes"}`, optional: 1 to 120, default 15. Assigns the ticket to you and moves a backlog or todo ticket to the first in-progress status; claiming your own claim again renews it |
| `POST /api/v1/tickets/{ticket}/heartbeat` | `{"lease_minutes"}`, optional: extends your claim from now |
| `POST /api/v1/tickets/{ticket}/release` | Nothing; gives up your claim, keeping the status and assignee |

## Decisions

A [decision](/help/agent-workflow#decisions) is a question for the owner, a ruling an agent made, or an approval of a plan, a merge or a release. Only the owner answers one. A request acting as an agent (your token with `Vaquero-Agent`) that answers is relaying the owner's answer, given in conversation: it must quote the owner in `words`, or it is `422` on `words`. Without the header, the owner answers directly, and `words` is refused; a direct answer comes from the command line or the API, never through MCP, where an answer is always relayed by the agent `vaquero mcp --agent NAME` names.

| Request | Body or query |
|---|---|
| `POST /api/v1/decisions` | `{"project", "kind", "question", "tickets", "subject", "options", "recommendation", "why", "run", "answer"}`: `kind` is `question`, `approval` (with `subject` `plan`, `merge` or `release`) or `ruling`. `tickets` are keys in the project (a plan approval names at least one; leave them out for a project-level decision). `options` are `{"label", "tradeoff"}`, up to 10; an approval's must include one labelled `Approve` (any case), the only option that approves, or it is `422`, and an approval with none offers `Approve` and `Request changes`. A ruling is recorded already answered by you: send `answer` as `{"option"}` or `{"text"}`, with an optional `"note"`; other kinds take no `answer`. Answers `201` with the decision |
| `GET /api/v1/decisions` | Newest first: `project=KEY`, `state` (`open`, `answered` or `withdrawn`), `kind`, `ticket=KEY`, `limit=N` (default 100, at most 500); an unknown one is `422` |
| `GET /api/v1/decisions/{id}` | One decision |
| `POST /api/v1/decisions/{id}/answer` | `{"option"}` (a label, any case) or `{"text"}`, with optional `"note"` and, when relaying, `"words"`. Answers an open question or approval; on a ruling it overrules the answer, which the decision's history keeps. An answered question or approval, or a withdrawn decision, is `422` |
| `POST /api/v1/decisions/{id}/withdraw` | Nothing; withdraws an open question or approval. Only who asked, or the owner |

A decision has `id`, `project`, `tickets`, `kind`, `subject` (approvals), `question`, `options`, `recommendation`, `why`, `run`, `state`, `asked_by` and `asked_at`, and once answered `answer`, `note`, `answered_by` and `answered_at`. `answered` is `direct` or `relayed` when the owner answered, with `relayed_by` (the agent) and `words` (the owner's, quoted) for a relayed one; a ruling an agent made has none until the owner overrules it. An approval's `approves` names its approving option. An approval of a plan answered `Approve` records, for each ticket, an entry in `approvals`: `ticket`, `requirement_hash` (a SHA-256 of the ticket's title and description as approved), `plan_revision` (the ticket's own plan's revision, or `0` when it has none: a sub-ticket then relies on its epic's own approval) and `at`; the approval also pins each repo file the ticket references, at main's commit (see [Readiness](/help/agent-workflow#readiness)). Any other answer records none. A plan approval also has `asked`, what an `Approve` covers, as asked: for each ticket its `ticket`, `plan_revision`, `parent` (its epic's key, left out for none) and `children` (its sub-tickets, each `key` and `title`). If any of that changes before the answer, `Approve` is `422`; ask again. An answered approval is final: to take one back, ask for a new plan approval and answer it `Request changes` (the latest answered plan approval of a ticket is the one that counts), or edit the ticket's requirement or plan, which makes the approval stale. The events (`decision.asked`, `decision.ruled`, `decision.answered`, `decision.withdrawn`) carry `decision_id`, `kind`, `subject`, `ticket_ids`, and the question and answer cut to 200 characters; never the owner's quoted words.

## Bulk changes

`POST /api/v1/tickets/bulk` works many tickets in one request, so an agent booking a batch makes one call instead of one per action:

    {"items": [
      {"key": "VAQ-9", "claim": {"lease_minutes": 120}, "comment": "Taking this."},
      {"key": "VAQ-10", "version": 3, "update": {"priority": 2}, "move": "In review", "release": true}
    ]}

- An item names a ticket by `key` and any of `project` (another project's key: the ticket moves there with its sub-tickets, as `move-to` moves it, and the rest of the item names that project's statuses and milestones; the project it is already in changes nothing), `claim` and `heartbeat` (`{}`, or `{"lease_minutes"}`), `update` (the fields `PATCH` takes, without `version`), `comment` (the text), `move` (a status name; the ticket goes to the end of that column, and a move to the status it already has changes nothing, so a retried batch doesn't reorder the column) and `release` (`true`).
- An item's actions run in that order: project, claim, heartbeat, update, comment, move, release. The first one that fails skips the rest of that item, so a refused claim posts no comment and makes no move.
- An item's `version` is checked before any of its actions run, and again by its update, as a `PATCH` checks it. If the ticket changed since, the item is a `conflict` and nothing in it runs. A claim or a move to another project in the same item changes the version, which is allowed for: send the version you read. A result names the ticket by its key after the item, its new key when it moved.
- `heartbeat` renews your claim from now without changing the version, as its endpoint does. A `release` after the same item's update or move ended your claim (a move to a done or cancelled status, a reassignment) has nothing left to do and succeeds; a claim that ended any other way (it lapsed, or was released elsewhere) gets the normal release, which refuses.
- Items stand alone and run in request order. One refused item never stops the others, and the same key may appear in more than one item.
- Each action is the same change its own endpoint makes, recorded in the ticket's history the same way.
- At most 100 items, within the 1 MiB body limit.

A call that can't run as sent is `422` and changes nothing, with `fields` naming each problem (`items`, `items[0].key` and so on): no items, more than 100, an item with no `key`, an item with nothing to do, an item with both `claim` and `heartbeat` (it can claim or renew, not both), a `version` with no `update`, a `version` inside `update` (put it on the item), or a blank `move` or `project`.

The answer is `200` with one result per item, in request order:

    {"items": [
      {"key": "VAQ-9", "ok": true, "status": "In progress", "version": 4},
      {"key": "VAQ-10", "ok": false, "status": "Todo", "version": 5,
       "error": {"code": "conflict", "message": "VAQ-10 changed since you read it."},
       "skipped": ["move", "release"]}
    ]}

`status` and `version` are the ticket as the item left it. `error` has the same `code`, `message` and `fields` as a refusal from the item's own endpoint, and `skipped` lists the actions that didn't run. If the request ends before every item has run (the client went away, or the server is stopping), an item whose actions all ran is `ok` and carries the ticket's `status` and `version`, even when the request ends right after it. Only the item the request was cut off in, and each item after it, have the code `cancelled`, with the actions that didn't run in `skipped`; the actions before those ran.

## Errors

Every refusal has the same shape:

    {"error": {"code": "invalid", "message": "Enter a title.", "fields": {"title": "Enter a title."}}}

| Status | Code | Meaning |
|---|---|---|
| — (in a bulk result only) | `cancelled` | The request ended before that item finished |
| 400 | `bad_request` | The body isn't one JSON object of known fields |
| 401 | `unauthorized` | No valid token |
| 403 | `forbidden` | Only the owner may do this (an export) |
| 404 | `not_found` | No such project, ticket, comment, link or token, or no such endpoint |
| 405 | `method_not_allowed` | The endpoint exists but not for that method (say, `DELETE` on a ticket) |
| 409 | `conflict` | The ticket changed since the `version` you sent (an edit, or a tag change in project settings), or its plan since the `revision` you sent; `current` holds the ticket, or the plan, as it is now |
| 413 | `too_large` | The body is over 1 MiB, or an uploaded file is over 10 MB |
| 422 | `invalid` | The change was refused; `message` says why and `fields` names the fields. Deleting an attachment you didn't upload (and aren't the owner) is a 422 with `fields.attachment`, not a 403 |
| 500 | `server_error` | Something broke on the server; its log has the details |
| 500 | `missing_file` | An export found an attachment whose file is missing from storage; `message` names it and its ticket |
| 500 | `damaged_file` | An export found an attachment whose file in storage isn't the size its record says; `message` names it and its ticket |
| 422 | `export_too_large` | An export's `export.json` would be larger than 256 MB, more than `vaquero import` reads; nothing is sent |

On a `409`, read `current`, decide, and send again with its `version`. See [Editing and conflicts](/help/editing-and-conflicts).
