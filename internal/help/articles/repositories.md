---
title: Repositories
section: using
summary: Point a project at a git repository: its path, main branch, remote and how often to fetch.
order: 8
---
A project can name the git repository its work lives in. Vaquero reads it to show files and history next to tickets, and never changes it.

## Settings

Open the project's settings and find Repository. It has four fields:

- Repository path: an absolute path to the repository's work tree. Vaquero saves the work tree's root, even when you name a folder inside it.
- Main branch: the branch Vaquero treats as the truth, `main` unless you change it. The repository must have it.
- Remote: one of the repository's remotes. Leave it on the default to use `origin` when the repository has it. Its URL is shown, without any user name or password.
- Fetch every N minutes: how often to fetch the remote, from 5 to 1440; 0 means never.

From the command line, `vaquero project edit KEY --repo PATH --main-branch NAME --remote NAME --fetch-minutes N` sets the same things. The [API](/help/api) takes `repo_path`, `main_branch`, `remote` and `fetch_minutes` on `PATCH /api/v1/projects/{key}`, and a project's answer has a `repo` object once a path is set.

Only the owner changes the path, main branch and remote, because the project's readiness will depend on them. An agent can change how often the repository is fetched.

## References

A ticket can point at the documents that govern it: a requirement, a design, a plan, a spec or something else. A reference is either a path in the project's repository, such as `docs/design.md`, or an http or https URL. A path needs the project's repository to be set first, and it doesn't have to exist on main yet, so you can reference a file you are about to write. Adding a reference never runs git.

Each reference has a kind and, if you like, a label (shown instead of the target, up to 120 characters) and an anchor, a heading in the target such as `acceptance-criteria` (lower-case letters, digits and hyphens), which only changes where the link lands. A ticket can reference a target once for each anchor.

The ticket page lists them under References: a repo path opens the file view, a URL opens in a new tab, and Remove asks before it deletes. References move with a ticket to another project; a repo path is then read against the new project's repository.

From the command line, `vaquero ref new TICKET KIND TARGET --anchor A --label L` and `vaquero ref delete TICKET ID`; `vaquero ticket show` lists a ticket's references with their ids. Agents use the `create_reference` and `delete_reference` [tools](/help/mcp). The [API](/help/api) has `POST /api/v1/tickets/{ticket}/refs` and `DELETE /api/v1/tickets/{ticket}/refs/{id}`, and a ticket's answer has `references`.

## Reading files and commits

A repo path reference opens the file view, `/p/{key}/files/{path}`: the file as it is on the main branch, or at a branch, tag or commit hash with `?at=`. A Markdown file (`.md`, `.markdown`) is rendered the same safe way as a ticket's description; anything else is shown as plain text. Files over 512 KiB, and files that aren't text, aren't shown. Opened from a reference on a ticket whose plan is approved, the view says whether the file changed on main since the approval pinned it, with Show changes to see how, or was renamed or removed (see [Readiness](/help/agent-workflow#readiness)). A path or revision that could make git do something other than read (`..`, a leading `-`, `HEAD@{1}` and the like) is refused before git runs.

A ticket's page lists its commits under Commits: the commits on the main branch whose message names the ticket in parentheses, `(VAQ-12)` but not `(VAQ-123)`, newest first, at most 50, with the lines they added and removed, the files they touched, and the first release tag that contains them. A ticket moved from another project keeps the commits made under its old key. The list is read after the page loads, so a slow repository never holds the page up.

From the command line, `vaquero file KEY PATH --at REV` prints a file and `vaquero commits TICKET` lists the commits. Agents read files with the `read_file` [tool](/help/mcp). The [API](/help/api) has `GET /api/v1/projects/{key}/files/{path}`, `GET /api/v1/tickets/{ticket}/commits` and `GET /api/v1/projects/{key}/repo/status`, which compares the main branch and the local branches with the remote's as of the last fetch, without contacting it.

## Fetching

Fetching is off by default (fetch every 0 minutes) and is the only thing in Vaquero that contacts a remote. Everything else reads the repository as it is on disk, so the status and the readiness checks are as fresh as the last fetch.

Set Fetch every N minutes (5 to 1440) and the server fetches the project's remote whenever its last fetch is that old, one repository at a time; projects that share a repository share one fetch. Fetch now on the settings page, `vaquero fetch KEY` and `POST /api/v1/projects/{key}/repo/fetch` fetch at once, whatever the interval, and anyone may use them. A fetch updates only Vaquero's view of the remote (its branches under `refs/remotes/` and its tags under `refs/vaquero/remote-tags/`): never your branches, your tags or your work tree, and it writes no event. It uses your own git credential helpers, never prompts and gives up after 60 seconds, so a remote that wants a password fails instead of waiting.

When a fetch fails, the settings page shows when and why (git's first line, such as "Could not resolve host"), and the API's `last_fetch_error` has it, until a fetch succeeds or the server restarts. Reading files, commits and the status keeps working.

## When git is missing

Without git installed, the section says "Git isn't installed, so repository features are off." and its fields are disabled, and the file view and a ticket's Commits say "Git isn't installed." Vaquero needs git 2.31 or later; with an older git they say "Git 2.31 or later is needed." instead, and "Couldn't read git's version." when git doesn't say which it is. Nothing else in Vaquero depends on git.

A partial clone (one cloned with `--filter`, which leaves some file contents on the remote) needs git 2.44 or later: Vaquero never downloads a missing file to read it, and only from 2.44 can git be told not to. With an older git the repository is refused with "Partial clones need git 2.44 or later." In a partial clone a file whose contents aren't on disk can't be shown, and a readiness check that needs one says "Can't check its files: the repository can't be read." until a fetch or checkout brings it.

## What stays local

Vaquero runs git only to read. The one exception is [fetching](#fetching), which is off by default and is the only thing that contacts a remote.
