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

## When git is missing

Without git installed, the section says "Git isn't installed, so repository features are off." and its fields are disabled. Nothing else in Vaquero depends on git.

## What stays local

Vaquero runs git only to read. The one exception is fetching, which is the only thing that contacts a remote.
