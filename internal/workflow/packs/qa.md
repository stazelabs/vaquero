# Audit pack: qa

Read by the audit workflow prompt (`vaquero workflow audit --project KEY --kind qa`). Save it in the project's repository as `.vaquero/audits/qa.md` and fill in each section. Keep the headings as they are: the prompt finds each section by its heading.

## Standards

<The documents that name the conventions a runtime defect can break: the instruction file, the UI and copy rules.>

## Never targets

- <The owner's own server, its port and its data directory; tunnels; production hosts. A command whose output names one is a stop.>

## Boot

<The exact commands to start a disposable server in the background: the environment (a free port, a new temporary data directory), the build, the start command, and how to record PORT and PID so cleanup can stop it by PID. The health URL. Where the log is and its format. How to sign in, and how to get an elevated or second account.>

## Areas

Flow slices:

| Slice | Flow | Walk |
|---|---|---|
| A | <flow> | <the pages and actions> |

Cross-cutting passes. The prompt knows N console and network, O dark mode, P narrow viewport, Q keyboard and focus, R validation and errors, S role gating and T recent fixes; add passes, or override one with a `### Pass X` subsection:

| Pass | What to exercise |
|---|---|
| S | <the roles, and how to get a second account> |

## Limits

- <Email, model calls, outbound network, destructive admin tools.>

## Triage

<Noise particular to this app's environment, how to inspect the scratch data directly, and the log greps, in the log's real format, with the pattern that proves each one fires.>

## Code map

<Where to trace a defect: routes, handlers, view sources, client scripts. The generated files that are never evidence.>

## Cleanup

<Anything to remove beyond stopping the server by PID and closing the tabs, such as the temporary data directory.>

## Known non-findings

- <Behaviour that looks wrong and is intended, and why. It may point at the continuity pack's list.>
