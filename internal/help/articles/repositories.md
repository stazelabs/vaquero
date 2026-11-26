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

From the command line, `vaquero ref add TICKET KIND TARGET --anchor A --label L` and `vaquero ref remove TICKET ID`; `vaquero ticket show` lists a ticket's references with their ids. Agents use the `add_reference` and `remove_reference` [tools](/help/mcp). The [API](/help/api) has `POST /api/v1/tickets/{ticket}/refs` and `DELETE /api/v1/tickets/{ticket}/refs/{id}`, and a ticket's answer has `references`.

## Reading files and commits

A repo path reference opens the file view, `/p/{key}/files/{path}`: the file as it is on the main branch, or at a branch, tag or commit hash with `?at=`. A Markdown file (`.md`, `.markdown`) is rendered the same safe way as a ticket's description; anything else is shown as plain text. Files over 512 KiB, and files that aren't text, aren't shown. Opened from a reference on a ticket whose plan is approved, the view says whether the file changed on main since the approval pinned it, with Show changes to see how, or was renamed or removed (see [Readiness](/help/agent-workflow#readiness)). A path or revision that could make git do something other than read (`..`, a leading `-`, `HEAD@{1}` and the like) is refused before git runs.

A ticket's page lists its commits under Commits: the commits on the main branch whose message names the ticket in parentheses, `(VAQ-12)` but not `(VAQ-123)`, newest first, at most 50, with the lines they added and removed, the files they touched, and the first release tag that contains them. A ticket moved from another project keeps the commits made under its old key. The list is read after the page loads, so a slow repository never holds the page up.

From the command line, `vaquero file KEY PATH --at REV` prints a file and `vaquero commits TICKET` lists the commits. Agents read files with the `read_file` [tool](/help/mcp). The [API](/help/api) has `GET /api/v1/projects/{key}/files/{path}`, `GET /api/v1/tickets/{ticket}/commits` and `GET /api/v1/projects/{key}/repo/status`, which compares the main branch and the local branches with the remote's as of the last fetch, without contacting it.

## When git is missing

Without git installed, the section says "Git isn't installed, so repository features are off." and its fields are disabled, and the file view and a ticket's Commits say "Git isn't installed." Nothing else in Vaquero depends on git.

## What stays local

Vaquero runs git only to read. The one exception is fetching, which is the only thing that contacts a remote.
