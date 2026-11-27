# Audit pack: hygiene

Read by the audit workflow prompt (`vaquero workflow audit --project KEY --kind hygiene`). Save it in the project's repository as `.vaquero/audits/hygiene.md` and fill in each section. Keep the headings as they are: the prompt finds each section by its heading.

## Standards

<The instruction file and rule files to read in full before the sweep, and which of their sections matter most.>

## Pre-pass

<The commands to run before any subagent: build, vet, lint, tests, coverage. Optional manual tools, and the dimension each is for. Their output is context, never a ticket.>

## Dimensions

| # | Dimension | What it hunts |
|---|---|---|
| 1 | Dead code and unused surface | <in this project's terms> |
| 2 | Duplication | <...> |
| 3 | Architecture and layering | <...> |
| 4 | Data and query correctness | <...> |
| 5 | Security | <...> |
| 6 | Performance and resource use | <...> |
| 7 | Error handling and observability | <...> |
| 8 | Test coverage and quality | <...> |

<The cross-cutting invariants, and which dimension owns each.>

## Search surface

<The paths the subagents search, and what they skip, such as generated files.>

## Known non-findings

- <A deliberate choice never to file, and why.>

## Verification notes

<Optional. How to confirm each dimension's claims in this codebase: the ways a symbol can be referenced, where middleware is mounted, and so on.>
