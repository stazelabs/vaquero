# The vaquero CLI

The `vaquero` binary is also the command line. It talks to a vaquero server over the API (`/api/v1`). With the built-in `local` profile it talks to the server on this machine and starts it in the background when it isn't running.

## Commands

Every command takes `--profile NAME` and `--json`. Flags may come before or after the other arguments. `-` in place of a text argument (or `--body -`) reads standard input.

| Command | What it does |
|---|---|
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
| `vaquero activity [--after SEQ] [--limit N]` | Recent changes, oldest first |
| `vaquero token create NAME` / `list` / `revoke PREFIX` | API tokens; a new token is printed once |
| `vaquero profile add NAME URL TOKEN` / `list` / `use NAME` / `remove NAME` | Servers to talk to |

Priorities are `none`, `urgent`, `high`, `medium` and `low`. Statuses, assignees and milestones are given by name, in any case.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Done |
| 1 | Failed; the reason is on standard error |
| 2 | Used wrongly; the usage line is on standard error |
| 3 | The ticket changed since you read it (`--version` was stale). The ticket as it is now is printed; read it, then retry |

## Profiles

`local` is built in: the server on this machine, at `VQ_ADDR` (default `127.0.0.1:7878`) with its data in `VQ_DATA_DIR`, and the token vaquero keeps in that directory. Other profiles are saved in your user config directory (`vaquero/config.json`, readable only by you) with `vaquero profile add`. The profile used is the `--profile` flag, else `VAQUERO_PROFILE`, else the one chosen with `vaquero profile use`, else `local`.

## Scripts and agents

Use `--json` and check the exit code. To edit safely when someone else might change the same ticket, read it (`vaquero ticket show TICKET --json`), then edit with `--version` set to the version you read; on exit 3, read again and decide.
