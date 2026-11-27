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

It talks to the Vaquero on this machine, starting it if it isn't running, with the token Vaquero keeps in its data directory. `--server NAME` points it at another server. Diagnostics go to standard error; standard output carries only the protocol.

For Claude Code, add it once:

    claude mcp add vaquero -- vaquero mcp --agent builder

Give each agent you run its own `--agent` name, so their claims and comments stay apart. `--agent` works only with the local owner's token; with any other token it is ignored, and `vaquero mcp` says so on standard error.

Each change an agent makes records where it came from, as the client reports it: that it arrived over MCP, the `vaquero mcp` version, the client's own name and version (from the session's first message), and the model, given with `--model ID` or the `VAQUERO_MODEL` environment variable. Whatever is missing is left out. These are recorded as reported, not checked; the agent's name is what says who made the change.

    vaquero mcp --agent builder --model my-model-id

**HTTP.** A client that connects over HTTP uses `http://127.0.0.1:7878/mcp` (Streamable HTTP, one request per message, plain JSON answers) with an API token in an `Authorization: Bearer vq_...` header, and the agent's name in a `Vaquero-Agent` header. [API tokens](/help/api-tokens) explains making a token, and [Agents and claims](/help/agents-and-claims#naming-an-agent) how names work. Such a client can report its name and version in a `Vaquero-Harness` header and its model in `Vaquero-Model`; see [the API](/help/api#where-a-change-came-from).

## Tools

| Tool | What it does |
|---|---|
| `list_projects` | Every project, with its open ticket count |
| `list_tickets` | A project's tickets: `project`, and optionally `status`, `tag`, `mine`, `open`, `limit` |
| `get_ticket` | One ticket with its description, tags, sub-tickets, links, comments, attachments, claim, version and tier, and with the [agent workflow](/help/agent-workflow#readiness) on its readiness: `key` |
| `ready_tickets` | Tickets ready to be worked on, most urgent first, applying [readiness](/help/agent-workflow#readiness) where the agent workflow is on: optionally `project`, `limit` |
| `search` | Find tickets by words: `query`. At most 50, best first; the REST answer's `Vaquero-Total` header counts every match |
| `recent_activity` | Changes, oldest first: `after` a sequence number, or the `newest` few; reorders within a column are left out |
| `create_ticket` | `project`, `title`, and optionally `body`, `status`, `priority`, `assignee`, `milestone`, `parent`, `tags`, `tier`, `model` |
| `update_ticket` | `key` and any fields to change (`tier` and `model` among them), plus `add_tags`, `remove_tags` and `version` |
| `move_ticket` | `key`, `status` |
| `create_comment` | `key`, `body` |
| `get_plan` | A ticket's [plan](/help/agent-workflow#plans): `key`, optionally `revision` for an earlier one |
| `read_file` | A file in the project's [repository](/help/repositories#reading-files-and-commits): `project`, `path`, optionally `at` (a branch, tag or commit hash; the main branch when left out). Answers the same as `GET /api/v1/projects/{key}/files/{path}`; use it to read a ticket's references |
| `save_plan` | Write or revise a plan: `key`, `body` (the whole text) and `revision`, the one you read (leave it out only for a ticket's first plan); a stale revision is refused |
| `ask_decision` | Park a [decision](/help/agent-workflow#decisions): `project`, `kind` (`question`, `approval` or `ruling`), `question`, and optionally `tickets`, `subject` (an approval's `plan`, `merge` or `release`), `options` (each a `label` and `tradeoff`), `recommendation`, `why` and `run`. A ruling is your own call, recorded answered: give `answer` (an option's label, or words) and `note` |
| `answer_decision` | Relay the owner's answer: `id`, `option` or `text`, optionally `note`, and `words`, the owner's own, quoted. The answer is recorded as relayed by the agent `vaquero mcp --agent NAME` (or `Vaquero-Agent`) names; without an agent, or without `words`, it is refused. An agent never answers as itself |
| `list_decisions` | Decisions, newest first: optionally `project`, `state`, `kind`, `ticket` and `limit` |
| `link_tickets` | `key`, `kind` (`blocks`, `relates` or `duplicates`), `to` |
| `unlink_tickets` | Remove a link: `link_id`, the id `get_ticket` lists on each link; answers `{"ok": true}` |
| `list_tags` | A project's tags with their ticket counts: `project` |
| `list_statuses` | A project's statuses in order, with category and ticket count: `project` |
| `get_me` | The actor the server sees you as: no arguments |
| `list_commits` | The main-branch commits that mention a ticket, with the diffstat and release: `ticket`; needs the project's [repository](/help/repositories#reading-files-and-commits) |
| `create_reference` | Point a ticket at a document ([references](/help/repositories#references)): `key`, `kind` (`requirement`, `design`, `plan`, `spec` or `other`), `target` (a repo path or an http or https URL), optionally `anchor` and `label`; answers with the reference and its `id` |
| `delete_reference` | `key`, `id`; answers `{"ok": true}` |
| `claim_ticket` | `key`, optionally `lease_minutes` (1 to 120, default 15) |
| `heartbeat` | Renew your claim: `key`, optionally `lease_minutes` |
| `unclaim_ticket` | Give up your claim: `key` |
| `bulk_update` | Work up to 100 tickets in one call: `items`, each a `key` with any of `project` (move it to another project, by key), `claim`, `heartbeat`, `update`, `comment`, `move` and `unclaim` (run in that order; `claim` or `heartbeat`, not both) and a `version`, which needs an `update` and is checked before any of them, as the API's [bulk changes](/help/api#bulk-changes) run; one short result per item |

`update_ticket`, `move_ticket`, `create_comment`, `claim_ticket`, `heartbeat` and `unclaim_ticket` also take `view`: `summary` answers with only the ticket's key, status, category, version, assignee and claim (and the comment's id), the API's [short answer](/help/api#short-answers), instead of the whole ticket.

A tool's answer is the same JSON the API gives. Its structured content, for clients that read it, is that JSON too, except that a list arrives as `{"items": [...]}`, because structured content is always an object. A refusal (an unknown ticket, a claim someone else holds, a stale `version`) is a tool error whose text says what went wrong, the same message the API gives.

The server tells clients the working loop: take a ticket from `ready_tickets`, `claim_ticket` it, `heartbeat` while working, record progress with `create_comment` and `update_ticket`, `move_ticket` it when done, and `unclaim_ticket` when stopping. Where the agent workflow is on, it also says to write plans with `save_plan`, to park questions and approvals with `ask_decision`, and to record the owner's answers only as relayed, with their words. Its last sentence names the workflow prompts and their arguments.

## Prompts

The server also offers the [workflow prompts](/help/agent-workflow#workflow-prompts): instructions for a longer job, filled in with your arguments, which a client lists and starts as it does any MCP prompt. They are text shipped in Vaquero, not tools; starting one reads and changes nothing.

| Prompt | Arguments |
|---|---|
| `plan` | `ticket`, a ticket's key |
| `audit` | `project`, a project's key, and `kind`: `qa`, `hygiene`, `continuity` or `docs` |

A missing argument, or a `kind` that isn't one of those, is refused with an invalid-params error that names the argument and what it takes, such as "Give kind: qa, hygiene, continuity or docs." `vaquero workflow` prints the same text without a server.

## The activity log

Every tool call, and every prompt started (as `prompt:plan`, `prompt:audit`), is recorded: when, which agent, which token (by its prefix), the tool, a short summary of its arguments, how it ended (`ok`, or the error such as `not_found`, `invalid`, `conflict`, `unknown_tool`, `unknown_prompt` or `bad_arguments`) and how long it took. Long text in arguments, like a comment's body, is cut short. Read it on the Admin page under MCP activity, filtered by agent or tool; it updates as calls arrive. Agent names are case-sensitive and an agent is never the person using Vaquero: an agent called `Owner` is listed apart from you, and its chip says `(agent)`. Calls are kept for 30 days.
