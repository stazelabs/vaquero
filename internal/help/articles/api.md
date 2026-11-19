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
- A request body is one JSON object; unknown fields are refused, and bodies over 1 MiB get `413`.
- Every answer carries `Vaquero-Version` (the server's version) and `Vaquero-Min-Client` (the oldest command line it works with).
- Lists take `limit` (most endpoints allow up to 500).

## Reading

| Request | Answer |
|---|---|
| `GET /api/v1/me` | The actor the token belongs to |
| `GET /api/v1/projects` | Every project, with `open_count` |
| `GET /api/v1/projects/{key}` | One project |
| `GET /api/v1/projects/{key}/statuses` | Its statuses in board order, each with its `category` |
| `GET /api/v1/projects/{key}/milestones` | Its milestones, with `done` and `total` counts |
| `GET /api/v1/projects/{key}/tickets` | Its tickets: filter with `status`, `open=1`, `tag`, `priority`, `mine=1`, `agents=1`, `milestone` (a name, or `none`), `assignee` (a name); an unknown name is `422` |
| `GET /api/v1/tickets/{ticket}` | One ticket with `body`, `sub_tickets`, `links` and `comments` |
| `GET /api/v1/search` | Search with `q`; each hit has a `snippet` with `[` and `]` around the matched words |
| `GET /api/v1/events` | Changes, oldest first: `after=SEQ` for those after a sequence number (poll with the last `seq` you saw), or `newest=N` for the newest N |

A ticket in a list has `key`, `title`, `status`, `category`, `priority`, `assignee`, `tags`, `ready` and `version`. `ready` means the same as on [the board](/help/the-board#ready-tickets).

## Writing

| Request | Body |
|---|---|
| `POST /api/v1/projects` | `{"key", "name", "description"}` |
| `PATCH /api/v1/projects/{key}` | `{"name"}` and/or `{"description"}` |
| `POST /api/v1/projects/{key}/key` | `{"key"}`, the new key |
| `POST /api/v1/projects/{key}/tickets` | `{"title", "body", "status", "priority", "assignee", "parent", "milestone", "tags"}`; only `title` is required |
| `PATCH /api/v1/tickets/{ticket}` | Any of the ticket fields, plus `add_tags`, `remove_tags` and `version` |
| `POST /api/v1/tickets/{ticket}/move` | `{"status", "before", "after"}`: a status and, optionally, the keys of the cards to land between |
| `POST /api/v1/tickets/{ticket}/comments` | `{"body"}` |
| `PATCH /api/v1/comments/{id}` | `{"body"}`; your own comments only |
| `DELETE /api/v1/comments/{id}` | Nothing; your own comments only |
| `POST /api/v1/tickets/{ticket}/links` | `{"kind", "to"}`: `blocks`, `relates` or `duplicates`, and the other ticket's key |
| `DELETE /api/v1/links/{id}` | Nothing |
| `GET /api/v1/tokens` | Every token, without its secret |
| `POST /api/v1/tokens` | `{"name"}`; the answer holds the token, shown this once |
| `POST /api/v1/tokens/{prefix}/revoke` | Nothing; revokes the token with that prefix |

A `PATCH` changes only the fields it sends. Send `"assignee": ""` (or `"milestone"`, `"parent"`) to clear one; `"tags"` replaces the whole list, while `add_tags` and `remove_tags` change it one tag at a time.

## Errors

Every refusal has the same shape:

    {"error": {"code": "invalid", "message": "Enter a title.", "fields": {"title": "Enter a title."}}}

| Status | Code | Meaning |
|---|---|---|
| 400 | `bad_request` | The body isn't one JSON object of known fields |
| 401 | `unauthorized` | No valid token |
| 404 | `not_found` | No such project, ticket, comment, link or token, or no such endpoint |
| 405 | `method_not_allowed` | The endpoint exists but not for that method (say, `DELETE` on a ticket) |
| 409 | `conflict` | The ticket changed since the `version` you sent; `current` holds it as it is now |
| 413 | `too_large` | The body is over 1 MiB |
| 422 | `invalid` | The change was refused; `message` says why and `fields` names the fields |
| 500 | `server_error` | Something broke on the server; its log has the details |

On a `409`, read `current`, decide, and send again with its `version`. See [Editing and conflicts](/help/editing-and-conflicts).
