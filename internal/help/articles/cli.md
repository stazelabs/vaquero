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
| `vaquero docs [TOPIC]` | List these help articles, or print one as markdown; needs no server |
| `vaquero projects` | List projects with their open ticket counts |
| `vaquero project create KEY NAME [--description TEXT]` | Create a project |
| `vaquero project show KEY` | Show a project |
| `vaquero project edit KEY [--name NAME] [--description TEXT]` | Change its name or description |
| `vaquero project key KEY NEWKEY` | Change its key (links that use the old key stop working) |
| `vaquero tickets KEY [--status S] [--tag T] [--priority P] [--mine] [--agents] [--all] [--limit N]` | List tickets; done and cancelled are left out unless `--status` or `--all` |
| `vaquero ticket show TICKET` | A ticket with its sub-tickets, links and comments |
| `vaquero ticket new KEY TITLE [--body TEXT] [--status S] [--priority P] [--tag T]... [--parent TICKET] [--milestone NAME] [--assignee NAME]` | Create a ticket; prints its key |
| `vaquero ticket edit TICKET [same flags] [--add-tag T]... [--remove-tag T]... [--version N]` | Change only the fields given |
| `vaquero ticket move TICKET STATUS` | Move a ticket to a status |
| `vaquero comment TICKET TEXT` | Add a comment |
| `vaquero link TICKET blocks\|relates\|duplicates OTHER` | Link two tickets |
| `vaquero unlink TICKET OTHER` | Remove the links between two tickets |
| `vaquero search WORDS...` | Search titles, descriptions and comments |
| `vaquero activity [--after SEQ] [--limit N]` | The newest changes (oldest of them first), or those after SEQ |
| `vaquero ready [KEY] [--limit N]` | Tickets ready to be worked on, most urgent first; every project unless KEY |
| `vaquero claim TICKET [--lease 30m]` | Claim a ticket for 1 minute to 2 hours (default 15 minutes); see [Agents and claims](/help/agents-and-claims) |
| `vaquero heartbeat TICKET [--lease 30m]` | Extend your claim from now |
| `vaquero release TICKET` | Give up your claim; the status and assignee stay |
| `vaquero token create NAME` / `list` / `revoke PREFIX` | API tokens; a new token is printed once |
| `vaquero profile add NAME URL TOKEN` / `list` / `use NAME` / `remove NAME` | Servers to talk to |

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
| 3 | The ticket changed since you read it (`--version` was stale). The ticket as it is now is printed; read it, then retry |

## Profiles

`local` is built in: the server on this machine at `VQ_ADDR` (default `127.0.0.1:7878`), with its data in `VQ_DATA_DIR`, using the token Vaquero keeps there. Other profiles are saved in your user config directory (`vaquero/config.json`, readable only by you) with `vaquero profile add`. The profile used is the `--profile` flag, else `VAQUERO_PROFILE`, else the one chosen with `vaquero profile use`, else `local`.

## Scripts and agents

Use `--json` and check the exit code. To edit safely when someone else might change the same ticket, read it (`vaquero ticket show TICKET --json`), then edit with `--version` set to the version you read; on exit 3, read again and decide. [Editing and conflicts](/help/editing-and-conflicts) explains why.
