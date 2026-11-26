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

## Conventions

- Projects are named by key (`VAQ`), tickets by key (`VAQ-12`), in any case.
- Statuses, assignees and milestones are sent and returned by name; names match in any case within the ticket's project.
- Priorities are numbers: 0 none, 1 urgent, 2 high, 3 medium, 4 low.
- Times are text in UTC, such as `2026-11-19T14:05:00.000Z`.
- A request body is one JSON object; unknown fields are refused, and bodies over 1 MiB get `413` (an upload may be 10 MB).
- Every answer carries `Vaquero-Version` (the server's version) and `Vaquero-Min-Client` (the oldest command line it works with).
- Lists take `limit` (most endpoints allow up to 500).

## Reading

| Request | Answer |
|---|---|
| `GET /api/v1/me` | The actor the token belongs to |
| `GET /api/v1/projects` | Every project, with `open_count` |
| `GET /api/v1/projects/{key}` | One project |
| `GET /api/v1/projects/{key}/statuses` | Its statuses in board order, each with its `category`, `position` and `ticket_count` |
| `GET /api/v1/projects/{key}/milestones` | Its milestones, with `done` and `total` counts |
| `GET /api/v1/projects/{key}/tickets` | Its tickets: filter with `status`, `open=1`, `tag`, `priority`, `mine=1`, `agents=1`, `milestone` (a name, or `none`), `assignee` (a name), `category` (`backlog`, `todo`, `in_progress`, `in_review`, `done` or `cancelled`); an unknown name or category is `422` |
| `GET /api/v1/tickets/{ticket}` | One ticket with `body`, `sub_tickets` (each the same summary a ticket list gives), `links`, `comments` and `attachments` |
| `GET /api/v1/tickets/{ticket}/attachments` | The ticket's files, oldest first |
| `GET /api/v1/attachments/{id}` | A file's bytes, served the way [the page serves them](/help/attachments#how-files-are-served) |
| `GET /api/v1/search` | Search with `q`; each hit has a `snippet` with `[` and `]` around the matched words |
| `GET /api/v1/export` | Everything, as a `.tar.gz` download (`application/gzip`); the owner's token only, `403` for anyone else, including an agent acting through it. `HEAD` makes the same checks and sends no body. See [Your data and backups](/help/data-and-backups#exporting-everything) |
| `GET /api/v1/events` | Changes, oldest first: `after=SEQ` for those after a sequence number (poll with the last `seq` you saw), or `newest=N` for the newest N |

An attachment has `id`, `name`, `content_type`, `size`, `uploaded_by`, `created_at` and `url`, the address of its bytes on the server (put it after the server's address, or use `GET /api/v1/attachments/{id}`).

A ticket in a list has `key`, `title`, `status`, `category`, `priority`, `assignee`, `tags`, `ready` and `version`. `ready` means the same as on [the board](/help/the-board#ready-tickets).

## Writing

| Request | Body |
|---|---|
| `POST /api/v1/projects` | `{"key", "name", "description"}` |
| `PATCH /api/v1/projects/{key}` | `{"name"}` and/or `{"description"}` |
| `POST /api/v1/projects/{key}/key` | `{"key"}`, the new key |
| `POST /api/v1/projects/{key}/statuses` | `{"name", "category", "before", "after"}`: `before` or `after` names a status to put it next to; without one it goes after the last status of its category. Answers `201` with the status |
| `PATCH /api/v1/projects/{key}/statuses/{name}` | `{"name"}`, the new name; a status's category never changes |
| `POST /api/v1/projects/{key}/statuses/{name}/move` | `{"before"}` or `{"after"}`: another status's name; the status moves there in one step |
| `DELETE /api/v1/projects/{key}/statuses/{name}` | Nothing; a status no ticket uses, and not the last of its category |
| `POST /api/v1/projects/{key}/tickets` | `{"title", "body", "status", "priority", "assignee", "parent", "milestone", "tags"}`; only `title` is required |
| `PATCH /api/v1/tickets/{ticket}` | Any of the ticket fields, plus `add_tags`, `remove_tags` and `version` |
| `POST /api/v1/tickets/bulk` | `{"items": [...]}`: claim, renew (heartbeat), update, comment on, move and release up to 100 tickets in one call; see [Bulk changes](#bulk-changes) |
| `POST /api/v1/tickets/{ticket}/move` | `{"status", "before", "after"}`: a status and, optionally, the keys of the cards to land between (a key that names no ticket is refused) |
| `POST /api/v1/tickets/{ticket}/comments` | `{"body"}` |
| `PATCH /api/v1/comments/{id}` | `{"body"}`; your own comments only |
| `DELETE /api/v1/comments/{id}` | Nothing; your own comments only |
| `POST /api/v1/tickets/{ticket}/attachments` | A multipart form with the file in a part named `file`; answers `201` with the attachment. Up to 10 MB: more is `413` |
| `DELETE /api/v1/attachments/{id}` | Nothing; the person who attached it, or the owner |
| `POST /api/v1/tickets/{ticket}/links` | `{"kind", "to"}`: `blocks`, `relates` or `duplicates`, and the other ticket's key |
| `DELETE /api/v1/links/{id}` | Nothing |
| `GET /api/v1/tokens` | Every token, without its secret |
| `POST /api/v1/tokens` | `{"name"}`; the answer holds the token, shown this once |
| `POST /api/v1/tokens/{prefix}/revoke` | Nothing; revokes the token with that prefix |

Statuses are named in the path and in `before` and `after` in any case; a name that matches no status is `422` naming the field (`status` for the one in the path). A status's `category` is one of `backlog`, `todo`, `in_progress`, `in_review`, `done` and `cancelled` (`in progress` works too). Each change is recorded in the history as yours, or the agent's when you act as one.

A `PATCH` changes only the fields it sends. Send `"assignee": ""` (or `"milestone"`, `"parent"`) to clear one; `"tags"` replaces the whole list, while `add_tags` and `remove_tags` change it one tag at a time.

## Short answers

A ticket update, a move, a new comment, a claim, a heartbeat and a release each answer with the whole ticket (a new comment answers with the comment). Add `?view=summary` to get only the ticket's state instead: `key`, `status`, `category`, `version`, and `assignee`, `claimed_by` and `claim_expires_at` when they are set, plus the new comment's `id` for a comment. `?view=full` is the default; any other view is `422` and nothing is written.

    POST /api/v1/tickets/VAQ-12/move?view=summary
    {"key": "VAQ-12", "status": "In review", "category": "in_review", "version": 7, "assignee": "builder"}

## Claiming work

Agents take work from the ready queue and hold it with a claim that expires unless renewed; [Agents and claims](/help/agents-and-claims) explains the rules. Each answer is the ticket, with `claimed_by` and `claim_expires_at` while a claim is held. A claim that can't be made or kept is `422` with a message naming why, such as who holds it and until when.

| Request | Body or query |
|---|---|
| `GET /api/v1/ready` | Ready tickets, urgent first, then high, medium, low and no priority, each in board order: `project=KEY` for one project, `limit=N` (default 50) |
| `POST /api/v1/tickets/{ticket}/claim` | `{"lease_minutes"}`, optional: 1 to 120, default 15. Assigns the ticket to you and moves a backlog or todo ticket to the first in-progress status; claiming your own claim again renews it |
| `POST /api/v1/tickets/{ticket}/heartbeat` | `{"lease_minutes"}`, optional: extends your claim from now |
| `POST /api/v1/tickets/{ticket}/release` | Nothing; gives up your claim, keeping the status and assignee |

## Bulk changes

`POST /api/v1/tickets/bulk` works many tickets in one request, so an agent booking a batch makes one call instead of one per action:

    {"items": [
      {"key": "VAQ-9", "claim": {"lease_minutes": 120}, "comment": "Taking this."},
      {"key": "VAQ-10", "version": 3, "update": {"priority": 2}, "move": "In review", "release": true}
    ]}

- An item names a ticket by `key` and any of `claim` and `heartbeat` (`{}`, or `{"lease_minutes"}`), `update` (the fields `PATCH` takes, without `version`), `comment` (the text), `move` (a status name; the ticket goes to the end of that column, and a move to the status it already has changes nothing, so a retried batch doesn't reorder the column) and `release` (`true`).
- An item's actions run in that order: claim, heartbeat, update, comment, move, release. The first one that fails skips the rest of that item, so a refused claim posts no comment and makes no move.
- An item's `version` is checked before any of its actions run, and again by its update, as a `PATCH` checks it. If the ticket changed since, the item is a `conflict` and nothing in it runs. A claim in the same item changes the version, which is allowed for: send the version you read.
- `heartbeat` renews your claim from now without changing the version, as its endpoint does. A `release` after the same item's update or move ended your claim (a move to a done or cancelled status, a reassignment) has nothing left to do and succeeds; a claim that ended any other way (it lapsed, or was released elsewhere) gets the normal release, which refuses.
- Items stand alone and run in request order. One refused item never stops the others, and the same key may appear in more than one item.
- Each action is the same change its own endpoint makes, recorded in the ticket's history the same way.
- At most 100 items, within the 1 MiB body limit.

A call that can't run as sent is `422` and changes nothing, with `fields` naming each problem (`items`, `items[0].key` and so on): no items, more than 100, an item with no `key`, an item with nothing to do, an item with both `claim` and `heartbeat` (it can claim or renew, not both), a `version` with no `update`, a `version` inside `update` (put it on the item), or a blank `move`.

The answer is `200` with one result per item, in request order:

    {"items": [
      {"key": "VAQ-9", "ok": true, "status": "In progress", "version": 4},
      {"key": "VAQ-10", "ok": false, "status": "Todo", "version": 5,
       "error": {"code": "conflict", "message": "VAQ-10 changed since you read it."},
       "skipped": ["move", "release"]}
    ]}

`status` and `version` are the ticket as the item left it. `error` has the same `code`, `message` and `fields` as a refusal from the item's own endpoint, and `skipped` lists the actions that didn't run. If the request ends before every item has run (the client went away, or the server is stopping), the item it ended in and each item after it have the code `cancelled`, with the actions that didn't run in `skipped`; actions before those ran.

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
| 409 | `conflict` | The ticket changed since the `version` you sent; `current` holds it as it is now |
| 413 | `too_large` | The body is over 1 MiB, or an uploaded file is over 10 MB |
| 422 | `invalid` | The change was refused; `message` says why and `fields` names the fields. Deleting an attachment you didn't upload (and aren't the owner) is a 422 with `fields.attachment`, not a 403 |
| 500 | `server_error` | Something broke on the server; its log has the details |
| 500 | `missing_file` | An export found an attachment whose file is missing from storage; `message` names it and its ticket |
| 500 | `damaged_file` | An export found an attachment whose file in storage isn't the size its record says; `message` names it and its ticket |

On a `409`, read `current`, decide, and send again with its `version`. See [Editing and conflicts](/help/editing-and-conflicts).
