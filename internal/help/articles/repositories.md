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

## When git is missing

Without git installed, the section says "Git isn't installed, so repository features are off." and its fields are disabled. Nothing else in Vaquero depends on git.

## What stays local

Vaquero runs git only to read. The one exception is fetching, which is the only thing that contacts a remote.
