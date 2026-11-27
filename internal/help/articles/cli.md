---
title: The command line
section: command-line
summary: Every vaquero command, its exit codes, servers, and how scripts and agents should use it.
order: 1
---
The `vaquero` program is also the command line. Commands other than `serve`, `version` and `docs` talk to a Vaquero server over [the API](/help/api). With the built-in `local` server they talk to the server on this machine, and start it in the background if it isn't running, saying once on standard error where it is (`Vaquero is running at http://127.0.0.1:7878.`).

## Commands

Every command that talks to a server takes `--server NAME`, `--agent NAME` and `--json`. On this machine, `--agent` (or `VAQUERO_AGENT`) makes the command act as that agent instead of you; the agent is created the first time its name is used. Flags may come before or after the other arguments. `-` as a comment's text (or `--body -`, `--description -`) reads standard input.

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
| `vaquero import linear FILE.csv [--project KEY]` | Make tickets from Linear's issue export, keeping team keys and issue numbers, into a new or empty project. See [Importing from Linear](/help/data-and-backups#importing-from-linear) |
| `vaquero logs [-n 100] [-f] [--path]` | The last lines of the server's log (100 unless `-n`); `-f` keeps printing new lines, across the log's move to `.1` at 10 MB, until Ctrl-C; `--path` prints only where the log is. Reads `vaquero.log` in the data directory, or the service's log under `brew services`; needs no server. See [Installing and upgrading](/help/installing-and-upgrading#the-servers-log) |
| `vaquero open [KEY\|TICKET]` | Open the web pages in your browser: the projects page, a project's Focus page (`vaquero open VAQ`) or a ticket's page (`vaquero open VAQ-12`); starts the server like any command; prints the address when no browser can be opened. With `--server NAME` it opens that server's address |
| `vaquero docs [TOPIC]` | List these help articles, or print one as markdown; needs no server |
| `vaquero me [--name NAME] [--avatar FILE \| --remove-avatar]` | Show your [profile](/help/your-profile), or change it: `--name` sets the display name (empty shows Owner again), `--avatar` uploads a PNG, JPEG or WebP image of up to 256 KB, `--remove-avatar` goes back to your initials. Only you, not an agent, change it |
| `vaquero projects` | List projects with their open ticket counts |
| `vaquero project new KEY NAME [--description TEXT]` | Create a project |
| `vaquero project show KEY` | Show a project |
| `vaquero project edit KEY [--name NAME] [--description TEXT] [--workflow on\|off] [--repo PATH] [--main-branch NAME] [--remote NAME] [--fetch-minutes N]` | Change its name or description, turn the [agent workflow](/help/agent-workflow) on or off, or set its [repository](/help/repositories): `--repo` is an absolute path (empty clears it), `--remote` empty means origin, `--fetch-minutes` is 0 or 5 to 1440. Only the owner, not an agent, changes `--repo`, `--main-branch` and `--remote` |
| `vaquero project key KEY NEWKEY` | Change its key (links that use the old key stop working) |
| `vaquero project icon KEY --monogram\|--glyph NAME\|--image FILE\|--remove-image` | Set its [icon](/help/projects-and-tickets#project-icons): its monogram, a glyph from the list, or a PNG, JPEG or WebP image of up to 256 KB. `--remove-image` deletes the uploaded image; if the project was showing it, it shows its monogram instead |
| `vaquero project link KEY depends-on\|relates OTHER` | Say the project depends on or relates to another; see [Project links](/help/projects-and-tickets#project-links). `vaquero project show` lists its links with their ids |
| `vaquero project unlink KEY ID\|OTHER` | Remove one of its links by id, or every link between it and another project |
| `vaquero statuses KEY` | A project's statuses in board order, with each one's category and ticket count |
| `vaquero status new KEY NAME --category C [--before S \| --after S]` | Create a status; without `--before` or `--after` it goes after the last status of its category. Categories are `backlog`, `todo`, `in progress`, `in review`, `done` and `cancelled` |
| `vaquero status edit KEY NAME --name NEW` | Rename a status |
| `vaquero status move KEY NAME --before S \| --after S` | Move a status next to another, in one step |
| `vaquero status delete KEY NAME` | Delete a status no ticket uses; each category keeps at least one |
| `vaquero tags KEY` | The tags a project's tickets carry, with how many of its tickets carry each |
| `vaquero tag edit KEY NAME --name NEW [--merge]` | Rename a tag on the project's tickets; a name another of its tags has is refused (exit 3) unless `--merge`, which merges the two |
| `vaquero tag delete KEY NAME` | Take a tag off every ticket in the project |
| `vaquero milestones KEY` | A project's milestones with their due dates, done and total tickets, and whether each is open |
| `vaquero milestone new KEY NAME [--description TEXT] [--due DATE]` | Create a milestone; dates look like 2026-12-31, and `-` as the description reads it from standard input |
| `vaquero milestone edit KEY NAME [--name NEW] [--description TEXT] [--due DATE]` | Change only what is given; `--due ""` clears the date, and `-` as the description reads it from standard input |
| `vaquero milestone close KEY NAME` / `reopen KEY NAME` | Close a milestone when it ships, or open it again |
| `vaquero tickets KEY [--status S] [--tag T] [--priority P] [--milestone NAME] [--mine] [--agents] [--all] [--limit N]` | List tickets; done and cancelled are left out unless `--status` or `--all` |
| `vaquero ticket show TICKET` | A ticket with its sub-tickets, links, references and comments |
| `vaquero ticket new KEY TITLE [--body TEXT] [--status S] [--priority P] [--tag T]... [--parent TICKET] [--milestone NAME] [--assignee NAME] [--tier T] [--model M]` | Create a ticket; prints its key. `--tier` is `design`, `light`, `standard` or `heavy` (see [tiers](/help/agent-workflow#execution-tiers)), and `--model` an exact model for it |
| `vaquero ticket edit TICKET [--title TEXT] [same flags] [--add-tag T]... [--remove-tag T]... [--version N]` | Change only the fields given; `--tier ""` and `--model ""` clear them |
| `vaquero ticket move TICKET STATUS` | Move a ticket to a status |
| `vaquero ticket transfer TICKET KEY [--version N]` | Move a ticket, with its sub-tickets, to the project KEY; prints its new key. Its old key keeps working. See [Moving a ticket to another project](/help/projects-and-tickets#moving-a-ticket-to-another-project) |
| `vaquero comment new TICKET TEXT` | Add a comment |
| `vaquero plan show TICKET [--revision N]` | A ticket's [plan](/help/agent-workflow#plans): its revision, who wrote it and when, then its text; `--revision` shows an earlier one |
| `vaquero plan save TICKET FILE\|- [--revision N]` | Write the plan from a file, or `-` for standard input. A ticket's first plan needs no `--revision`; after that, pass the revision you read. If someone saved since, nothing is written, the plan as it is now is printed and the exit code is 3 |
| `vaquero ticket link TICKET blocks\|relates\|duplicates OTHER` | Link two tickets |
| `vaquero ref new TICKET KIND TARGET [--anchor A] [--label L]` | Add a [reference](/help/repositories#references): `KIND` is `requirement`, `design`, `plan`, `spec` or `other`, `TARGET` a repo path or an http or https URL |
| `vaquero ref delete TICKET ID` | Remove one of a ticket's references; `ticket show` lists them with their ids |
| `vaquero file KEY PATH [--at REV]` | Print a file from the project's [repository](/help/repositories#reading-files-and-commits) as it is on the main branch, or at a branch, tag or commit hash; a file too large to show (over 512 KiB) or not text is an error |
| `vaquero commits TICKET` | The commits on the main branch whose message names the ticket, as `(VAQ-12)`, newest first (at most 50), with the lines added and removed and the first release tag that contains them |
| `vaquero fetch KEY` | Fetch the project's [repository](/help/repositories#fetching) remote now. Prints "Fetched KEY."; a failed fetch exits 1 with git's first line. |
| `vaquero ticket unlink TICKET OTHER` | Remove the links between two tickets |
| `vaquero attachment new TICKET FILE... [--name NAME]` | Attach files (up to 10 MB each) and print a Markdown reference for each, to paste into a description or comment; `-` reads standard input and needs `--name`. See [Attachments](/help/attachments) |
| `vaquero attachment list TICKET` | A ticket's files: id, name, size, who attached it and when |
| `vaquero attachment move --to sqlite\|dir\|s3 [--delete-old]` | Move every attachment's file to other storage, checking each copy, then switch to it; `--delete-old` deletes the old copies once the switch is made; resumes if interrupted; stop the server first. See [Your data and backups](/help/data-and-backups#moving-attachments-to-other-storage) |
| `vaquero attachment delete ID` | Remove an attachment; only the person who attached it, or the owner, can |
| `vaquero search WORDS...` | Search titles, descriptions and comments |
| `vaquero activity [--after SEQ] [--limit N]` | The newest changes (oldest of them first), or those after SEQ; reorders within a column are left out |
| `vaquero ready [KEY] [--limit N]` | Tickets ready to be worked on, most urgent first; every project unless KEY |
| `vaquero claim TICKET [--lease 30m]` | Claim a ticket for 1 minute to 2 hours (default 15 minutes); see [Agents and claims](/help/agents-and-claims) |
| `vaquero heartbeat TICKET [--lease 30m]` | Extend your claim from now |
| `vaquero unclaim TICKET` | Give up your claim; the status and assignee stay |
| `vaquero decisions [KEY] [--state open\|answered\|withdrawn] [--kind question\|ruling\|approval] [--ticket T] [--limit N]` | [Decisions](/help/agent-workflow#decisions), newest first; every project unless KEY |
| `vaquero decision show ID` | A decision: the question, its options, and the answer with who gave it, directly or relayed; for a plan approval, what approving covers (each ticket's plan revision, epic and sub-tickets) |
| `vaquero decision ask KEY QUESTION [--ticket T]... [--option "LABEL: TRADE-OFF"]... [--approval plan\|merge\|release] [--recommend TEXT] [--why TEXT] [--run R]` | Ask a question, or with `--approval` ask for an approval; prints its id. Each `--option` is a label, then optionally a colon and its trade-off; an approval's options must include `Approve` |
| `vaquero decision rule KEY QUESTION ANSWER [same flags] [--note TEXT]` | Record a ruling you made, answered with ANSWER (an option's label, or words) |
| `vaquero decision answer ID [OPTION] [--text TEXT] [--note TEXT] [--words TEXT]` | Answer as the owner: an option by label, or `--text`. With `--agent`, the answer is relayed by that agent and needs `--words`, the owner's answer quoted. On a ruling, it overrules |
| `vaquero decision withdraw ID` | Withdraw an open question or approval |
| `vaquero mcp [--agent NAME] [--model ID]` | An MCP server on standard input and output, for agent clients that launch one; `--model` (or `VAQUERO_MODEL`) names the model the session runs, recorded with each change; see [MCP](/help/mcp) |
| `vaquero token new NAME [--agent AGENT]` / `list` / `delete PREFIX` | API tokens; a new token is printed once. With `--agent` the token is made for that agent and acts as it (see [API tokens](/help/api-tokens#making-a-token)) |
| `vaquero server new NAME URL TOKEN` / `list` / `use NAME` / `delete NAME` | Servers to talk to; `-` as the token reads it from standard input, keeping it out of your shell history |

Priorities are `none`, `urgent`, `high`, `medium` and `low`. Statuses, assignees and milestones are given by name, in any case.

## Text that starts with a dash

Text that starts with `-`, such as a markdown list, would read as a flag. Put `--` before it: everything after `--` is taken as written.

    vaquero comment new VAQ-12 -- "- checked the logs
    - restarted the job"

## Renamed spellings

Every command now has the same verbs: `new`, `edit`, `delete`, `list` and `show`. The old spellings (`project create`, `status add`, `status rename`, `milestone add`, `tag rename`, `ref add`, `ref remove`, `token create`, `token revoke`, `attach`, `attachments`, `detach`, `comment TICKET TEXT`, `link`, `unlink`, `ticket move-to`, `release`, `profile` and `--profile`) stop with exit code 2 and name the new one, for one release. `VAQUERO_PROFILE` is still read when `VAQUERO_SERVER` isn't set. Plural commands such as `projects` and `tickets` stay as shortcuts for listing.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Done |
| 1 | Failed; the reason is on standard error |
| 2 | Used wrongly; the usage line is on standard error |
| 3 | The ticket changed since you read it (`--version` was stale), or its plan did (`plan save --revision` was stale). The ticket or plan as it is now is printed; read it, then retry |

## Servers

`local` is built in: the server on this machine at `VQ_ADDR` (default `127.0.0.1:7878`), with its data in `VQ_DATA_DIR`, using the token Vaquero keeps there. Commands read only `VQ_MODE`, `VQ_ADDR` and `VQ_DATA_DIR` (and `vaquero import` and `vaquero attachment move` the attachment storage settings), so a setting only the server uses, such as `VQ_LOG_LEVEL`, is checked by `vaquero serve` alone. Like the server, they read `vaquero.env` in the data directory after the environment, so a `VQ_ADDR` set there is where they look for the server, and a file other users can read stops them (see [Installing and upgrading](/help/installing-and-upgrading#settings-for-the-server)). Other servers are saved in your user config directory (`vaquero/config.json`, readable only by you) with `vaquero server new`. The server used is the `--server` flag, else `VAQUERO_SERVER`, else the one chosen with `vaquero server use`, else `local`.

## Scripts and agents

Use `--json` and check the exit code. To edit safely when someone else might change the same ticket, read it (`vaquero ticket show TICKET --json`), then edit with `--version` set to the version you read; on exit 3, read again and decide. [Editing and conflicts](/help/editing-and-conflicts) explains why.
