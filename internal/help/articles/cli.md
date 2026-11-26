---
title: The command line
section: command-line
summary: Every vaquero command, its exit codes, profiles, and how scripts and agents should use it.
order: 1
---
The `vaquero` program is also the command line. Commands other than `serve`, `version` and `docs` talk to a Vaquero server over [the API](/help/api). With the built-in `local` profile they talk to the server on this machine, and start it in the background if it isn't running.

## Commands

Every command that talks to a server takes `--profile NAME`, `--agent NAME` and `--json`. On this machine, `--agent` (or `VAQUERO_AGENT`) makes the command act as that agent instead of you; the agent is created the first time its name is used. Flags may come before or after the other arguments. `-` as a comment's text (or `--body -`, `--description -`) reads standard input.

| Command | What it does |
|---|---|
| `vaquero serve` | Run the server in the foreground |
| `vaquero version` | Print the version |
| `vaquero stop` | Stop the server running in the background on this machine |
| `vaquero backup list` | The database snapshots in the data directory, newest first; needs no server |
| `vaquero backup restore NAME` | Put a snapshot in place of the database, saving the current one as a snapshot first; stop the server first. See [Your data and backups](/help/data-and-backups#restoring-a-snapshot) |
| `vaquero backup compact` | Shrink the database to what it holds (deleted attachments leave space behind), saving it as a snapshot first; all or nothing; stop the server first. See [Your data and backups](/help/data-and-backups#compacting-the-database) |
| `vaquero export FILE\|-` | Write all your data to a `.tar.gz`, or with `-` to standard output; the owner only. See [Your data and backups](/help/data-and-backups#exporting-everything) |
| `vaquero import FILE` | Restore an export into an empty instance, all or nothing; stop the server first. See [Your data and backups](/help/data-and-backups#importing-an-export) |
| `vaquero logs [-n 100] [-f] [--path]` | The last lines of the server's log (100 unless `-n`); `-f` keeps printing new lines, across the log's move to `.1` at 10 MB, until Ctrl-C; `--path` prints only where the log is. Reads `vaquero.log` in the data directory, or the service's log under `brew services`; needs no server. See [Installing and upgrading](/help/installing-and-upgrading#the-servers-log) |
| `vaquero docs [TOPIC]` | List these help articles, or print one as markdown; needs no server |
| `vaquero projects` | List projects with their open ticket counts |
| `vaquero project create KEY NAME [--description TEXT]` | Create a project |
| `vaquero project show KEY` | Show a project |
| `vaquero project edit KEY [--name NAME] [--description TEXT] [--workflow on\|off]` | Change its name or description, or turn the [agent workflow](/help/agent-workflow) on or off |
| `vaquero project key KEY NEWKEY` | Change its key (links that use the old key stop working) |
| `vaquero project icon KEY --monogram\|--glyph NAME\|--image FILE` | Set its [icon](/help/projects-and-tickets#project-icons): its monogram, a glyph from the list, or a PNG, JPEG or WebP image of up to 256 KB |
| `vaquero statuses KEY` | A project's statuses in board order, with each one's category and ticket count |
| `vaquero status add KEY NAME --category C [--before S \| --after S]` | Add a status; without `--before` or `--after` it goes after the last status of its category. Categories are `backlog`, `todo`, `in progress`, `in review`, `done` and `cancelled` |
| `vaquero status rename KEY NAME NEW` | Rename a status |
| `vaquero status move KEY NAME --before S \| --after S` | Move a status next to another, in one step |
| `vaquero status delete KEY NAME` | Delete a status no ticket uses; each category keeps at least one |
| `vaquero milestones KEY` | A project's milestones with their due dates, done and total tickets, and whether each is open |
| `vaquero milestone add KEY NAME [--description TEXT] [--due DATE]` | Add a milestone; dates look like 2026-12-31, and `-` as the description reads it from standard input |
| `vaquero milestone edit KEY NAME [--name NEW] [--description TEXT] [--due DATE]` | Change only what is given; `--due ""` clears the date, and `-` as the description reads it from standard input |
| `vaquero milestone close KEY NAME` / `reopen KEY NAME` | Close a milestone when it ships, or open it again |
| `vaquero tickets KEY [--status S] [--tag T] [--priority P] [--milestone NAME] [--mine] [--agents] [--all] [--limit N]` | List tickets; done and cancelled are left out unless `--status` or `--all` |
| `vaquero ticket show TICKET` | A ticket with its sub-tickets, links and comments |
| `vaquero ticket new KEY TITLE [--body TEXT] [--status S] [--priority P] [--tag T]... [--parent TICKET] [--milestone NAME] [--assignee NAME] [--tier T] [--model M]` | Create a ticket; prints its key. `--tier` is `design`, `light`, `standard` or `heavy` (see [tiers](/help/agent-workflow#execution-tiers)), and `--model` an exact model for it |
| `vaquero ticket edit TICKET [--title TEXT] [same flags] [--add-tag T]... [--remove-tag T]... [--version N]` | Change only the fields given; `--tier ""` and `--model ""` clear them |
| `vaquero ticket move TICKET STATUS` | Move a ticket to a status |
| `vaquero comment TICKET TEXT` | Add a comment |
| `vaquero plan show TICKET [--revision N]` | A ticket's [plan](/help/agent-workflow#plans): its revision, who wrote it and when, then its text; `--revision` shows an earlier one |
| `vaquero plan save TICKET FILE\|- [--revision N]` | Write the plan from a file, or `-` for standard input. A ticket's first plan needs no `--revision`; after that, pass the revision you read. If someone saved since, nothing is written, the plan as it is now is printed and the exit code is 3 |
| `vaquero link TICKET blocks\|relates\|duplicates OTHER` | Link two tickets |
| `vaquero unlink TICKET OTHER` | Remove the links between two tickets |
| `vaquero attach TICKET FILE... [--name NAME]` | Attach files (up to 10 MB each) and print a Markdown reference for each, to paste into a description or comment; `-` reads standard input and needs `--name`. See [Attachments](/help/attachments) |
| `vaquero attachments TICKET` | A ticket's files: id, name, size, who attached it and when |
| `vaquero detach ID` | Remove an attachment; only the person who attached it, or the owner, can |
| `vaquero search WORDS...` | Search titles, descriptions and comments |
| `vaquero activity [--after SEQ] [--limit N]` | The newest changes (oldest of them first), or those after SEQ; reorders within a column are left out |
| `vaquero ready [KEY] [--limit N]` | Tickets ready to be worked on, most urgent first; every project unless KEY |
| `vaquero claim TICKET [--lease 30m]` | Claim a ticket for 1 minute to 2 hours (default 15 minutes); see [Agents and claims](/help/agents-and-claims) |
| `vaquero heartbeat TICKET [--lease 30m]` | Extend your claim from now |
| `vaquero release TICKET` | Give up your claim; the status and assignee stay |
| `vaquero decisions [KEY] [--state open\|answered\|withdrawn] [--kind question\|ruling\|approval] [--ticket T] [--limit N]` | [Decisions](/help/agent-workflow#decisions), newest first; every project unless KEY |
| `vaquero decision show ID` | A decision: the question, its options, and the answer with who gave it, directly or relayed; for a plan approval, what approving covers (each ticket's plan revision, epic and sub-tickets) |
| `vaquero decision ask KEY QUESTION [--ticket T]... [--option "LABEL: TRADE-OFF"]... [--approval plan\|merge\|release] [--recommend TEXT] [--why TEXT] [--run R]` | Ask a question, or with `--approval` ask for an approval; prints its id. Each `--option` is a label, then optionally a colon and its trade-off; an approval's options must include `Approve` |
| `vaquero decision rule KEY QUESTION ANSWER [same flags] [--note TEXT]` | Record a ruling you made, answered with ANSWER (an option's label, or words) |
| `vaquero decision answer ID [OPTION] [--text TEXT] [--note TEXT] [--words TEXT]` | Answer as the owner: an option by label, or `--text`. With `--agent`, the answer is relayed by that agent and needs `--words`, the owner's answer quoted. On a ruling, it overrules |
| `vaquero decision withdraw ID` | Withdraw an open question or approval |
| `vaquero mcp [--agent NAME] [--model ID]` | An MCP server on standard input and output, for agent clients that launch one; `--model` (or `VAQUERO_MODEL`) names the model the session runs, recorded with each change; see [MCP](/help/mcp) |
| `vaquero token create NAME` / `list` / `revoke PREFIX` | API tokens; a new token is printed once |
| `vaquero profile add NAME URL TOKEN` / `list` / `use NAME` / `remove NAME` | Servers to talk to; `-` as the token reads it from standard input, keeping it out of your shell history |

Priorities are `none`, `urgent`, `high`, `medium` and `low`. Statuses, assignees and milestones are given by name, in any case.

## Text that starts with a dash

Text that starts with `-`, such as a markdown list, would read as a flag. Put `--` before it: everything after `--` is taken as written.

    vaquero comment VAQ-12 -- "- checked the logs
    - restarted the job"

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Done |
| 1 | Failed; the reason is on standard error |
| 2 | Used wrongly; the usage line is on standard error |
| 3 | The ticket changed since you read it (`--version` was stale), or its plan did (`plan save --revision` was stale). The ticket or plan as it is now is printed; read it, then retry |

## Profiles

`local` is built in: the server on this machine at `VQ_ADDR` (default `127.0.0.1:7878`), with its data in `VQ_DATA_DIR`, using the token Vaquero keeps there. Commands read only `VQ_MODE`, `VQ_ADDR` and `VQ_DATA_DIR` (and `vaquero import` the attachment storage settings), so a setting only the server uses, such as `VQ_LOG_LEVEL`, is checked by `vaquero serve` alone. Other profiles are saved in your user config directory (`vaquero/config.json`, readable only by you) with `vaquero profile add`. The profile used is the `--profile` flag, else `VAQUERO_PROFILE`, else the one chosen with `vaquero profile use`, else `local`.

## Scripts and agents

Use `--json` and check the exit code. To edit safely when someone else might change the same ticket, read it (`vaquero ticket show TICKET --json`), then edit with `--version` set to the version you read; on exit 3, read again and decide. [Editing and conflicts](/help/editing-and-conflicts) explains why.
