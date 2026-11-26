# Contributing to Vaquero

Thank you for helping. This page says how a change gets from your branch into `main`.

## Before you open a pull request

Run the full gate locally, on the branch you will push:

    make tools   # one time
    make ci      # generate, format, vet, lint, test, coverage floors

For a change to the pages or to `app.js`, also run the browser tests:

    make e2e     # keyboard shortcuts, board drags, dialogs, narrow screens, light and dark

They drive the real pages in a headless Google Chrome (or Chromium) with a profile of their own, on temporary test servers; without Chrome installed they skip. The ci workflow runs them too, with Chrome required.

`make ci` must pass, and `git status` must be clean afterwards: generated files (templ, sqlc, Tailwind, the third-party notices) are committed, so a change to their inputs includes the regenerated output. README.md has the rest of the setup, and CLAUDE.md the rules the code keeps.

## What happens to your pull request

CI does not run on its own. No workflow in `.github/workflows` has a `push` or `pull_request` trigger, so a pull request from a fork never runs code with this repository's secrets, and nothing spends Actions minutes on every commit (`TestWorkflowsHaveNoPushOrPullRequestTriggers` holds this). Instead, before merging, a maintainer reads the change, pushes it to a branch here when it comes from a fork, and runs the ci workflow (Actions, "ci", Run workflow) on that branch; it runs the same gate as `make ci`. A pull request is merged only when that run passes.

## Commits

Commit messages are `{Area}: {summary}`, like `CLI: add vaquero logs`. Keep each commit to one change, with its tests.
