---
title: MCP
section: api-and-agents
summary: Connecting an agent to Vaquero over the Model Context Protocol, the tools it gets, and the log of what agents did.
order: 4
---
Vaquero speaks the Model Context Protocol (MCP), so an agent client can read and work tickets with tools instead of calling the API by hand. The tools are the API: same names, same rules, same answers. An agent that uses them shows up under its own name in tickets, history and the activity log.

## Connecting a client

There are two ways in.

**Standard input and output.** Most clients launch an MCP server as a command. Give them `vaquero mcp`, with the name the agent should work under:

    vaquero mcp --agent builder

It talks to the Vaquero on this machine, starting it if it isn't running, with the token Vaquero keeps in its data directory. `--profile NAME` points it at another server. Diagnostics go to standard error; standard output carries only the protocol.

For Claude Code, add it once:

    claude mcp add vaquero -- vaquero mcp --agent builder

Give each agent you run its own `--agent` name, so their claims and comments stay apart. `--agent` works only with the local owner's token; with any other token it is ignored, and `vaquero mcp` says so on standard error.

**HTTP.** A client that connects over HTTP uses `http://127.0.0.1:7878/mcp` (Streamable HTTP, one request per message, plain JSON answers) with an API token in an `Authorization: Bearer vq_...` header, and the agent's name in a `Vaquero-Agent` header. [API tokens](/help/api-tokens) explains making a token, and [Agents and claims](/help/agents-and-claims#naming-an-agent) how names work.

## Tools

| Tool | What it does |
|---|---|
| `list_projects` | Every project, with its open ticket count |
| `list_tickets` | A project's tickets: `project`, and optionally `status`, `tag`, `mine`, `open`, `limit` |
| `get_ticket` | One ticket with its description, tags, sub-tickets, links, comments, attachments, claim and version: `key` |
| `ready_tickets` | Tickets ready to be worked on, most urgent first: optionally `project`, `limit` |
| `search` | Find tickets by words: `query` |
| `recent_activity` | Changes, oldest first: `after` a sequence number, or the `newest` few; reorders within a column are left out |
| `create_ticket` | `project`, `title`, and optionally `body`, `status`, `priority`, `assignee`, `milestone`, `parent`, `tags` |
| `update_ticket` | `key` and any fields to change, plus `add_tags`, `remove_tags` and `version` |
| `move_ticket` | `key`, `status` |
| `add_comment` | `key`, `body` |
| `link_tickets` | `key`, `kind` (`blocks`, `relates` or `duplicates`), `to` |
| `claim_ticket` | `key`, optionally `lease_minutes` (1 to 120, default 15) |
| `heartbeat` | Renew your claim: `key`, optionally `lease_minutes` |
| `release_ticket` | Give up your claim: `key` |
| `bulk_update` | Work up to 100 tickets in one call: `items`, each a `key` with any of `claim`, `heartbeat`, `update`, `comment`, `move` and `release` (run in that order; `claim` or `heartbeat`, not both) and a `version`, which needs an `update` and is checked before any of them, as the API's [bulk changes](/help/api#bulk-changes) run; one short result per item |

`update_ticket`, `move_ticket`, `add_comment`, `claim_ticket`, `heartbeat` and `release_ticket` also take `view`: `summary` answers with only the ticket's key, status, category, version, assignee and claim (and the comment's id), the API's [short answer](/help/api#short-answers), instead of the whole ticket.

A tool's answer is the same JSON the API gives. Its structured content, for clients that read it, is that JSON too, except that a list arrives as `{"items": [...]}`, because structured content is always an object. A refusal (an unknown ticket, a claim someone else holds, a stale `version`) is a tool error whose text says what went wrong, the same message the API gives.

The server tells clients the working loop: take a ticket from `ready_tickets`, `claim_ticket` it, `heartbeat` while working, record progress with `add_comment` and `update_ticket`, `move_ticket` it when done, and `release_ticket` when stopping.

## The activity log

Every tool call is recorded: when, which agent, which token (by its prefix), the tool, a short summary of its arguments, how it ended (`ok`, or the error such as `not_found`, `invalid`, `conflict`, `unknown_tool` or `bad_arguments`) and how long it took. Long text in arguments, like a comment's body, is cut short. Read it on the Admin page under MCP activity, filtered by agent or tool; it updates as calls arrive. Agent names are case-sensitive and an agent is never the person using Vaquero: an agent called `Owner` is listed apart from you, and its chip says `(agent)`. Calls are kept for 30 days.
