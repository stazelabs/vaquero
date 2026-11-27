# Audit pack: docs

Read by the audit workflow prompt (`vaquero workflow audit --project KEY --kind docs`). Save it in the project's repository as `.vaquero/audits/docs.md` and fill in each section. Keep the headings as they are: the prompt finds each section by its heading.

## Standards

<The document that is the conventions contract. A docs audit corrects its statements of fact; it never invents, relaxes or deletes a rule.>

## Ground truth

<The commands that list the structural facts the documents claim: packages, routes, migrations, workflows, make targets.>

## Surfaces

| Surface | Treatment |
|---|---|
| README.md | In scope: status claims, run commands, links |

## Frozen

- <The paths never edited: dated specs and plans, generated files.>

## Partitions

1. <Area one: its files>
2. <Area two: its files>

## Voice

<The copy rules to fix in passing.>

## Extra rules

<Optional. Rules with a higher bar for particular surfaces, such as help articles: their shape, required sections, link rules, and the tests that pin them. Say how a command a document shows is checked without a server (its help, for example), since the audit never runs one against a live server.>

## Verify

<The check to run for a change to documentation only, and for a change under a source tree; the generated output a docs edit may move.>
