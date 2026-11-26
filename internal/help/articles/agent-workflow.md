---
title: Agent workflow
section: api-and-agents
summary: The project setting for agent work: execution tiers and the tier map, plans, the decisions the owner answers, and when a ticket is ready.
order: 5
---
The agent workflow is a project setting for teams where agents do much of the building. It is off in a new project, and a project with it off works as it always has. Turn it on in the project's settings, under Workflow, or with `vaquero project edit KEY --workflow on`.

## Turning it on

With the workflow on, the Workflow section of the settings page offers to add two backlog statuses, Needs decision and Needs design, if the project lacks them. The workflow parks tickets there: one waiting on an answer from you, one that needs a spec or a plan first. Each is added at the end of the backlog category, as any new status is.

## Execution tiers

Tickets carry an execution tier when the agent workflow is on; the [tier map](#the-tier-map) names the model each tier uses in each harness. The tier says how much work a ticket is for whoever builds it:

- **design** is spec and plan work, done before anything is built;
- **light** is small and clear;
- **standard** is ordinary work;
- **heavy** is large or tricky.

With the workflow on, set it on the ticket's edit form, and see it on the ticket's page and its card on the board. In any project, the command line (`vaquero ticket edit TICKET --tier standard`), the API (`"tier"` on a ticket) and the `create_ticket` and `update_ticket` tools set it too. A ticket may also name an exact model (`--model`, `"model"`), which overrides the tier map for that ticket.

## The tier map

The tier map names the model that works each tier, for each harness (the agent program doing the work). It is JSON: harness, then tier, then model.

```json
{
  "claude": {"design": "…", "light": "…", "standard": "…", "heavy": "…"}
}
```

Vaquero ships a default map, and settings shows the map in use. A map holds at most 20 harnesses. Edit it there and save, or send `"tier_map"` in `PATCH /api/v1/projects/{key}`. Empty the box (or send `null`) to go back to the default. The design tier normally takes the heavy model.

## Plans

A plan says how a ticket, or an epic and its sub-tickets, will be done. It lives in Vaquero beside the ticket, as a Markdown document, so agents can draft and revise it without touching the repository. Every revision is kept.

The ticket's page shows the current plan, with its revision, who wrote it and when; "Show changes" lists the lines changed since the revision before. Read and write plans with `vaquero plan show` and `vaquero plan save` (see [the command line](/help/cli)), the API's `/plan` routes (see [the API](/help/api#reading)) or the `get_plan` and `save_plan` tools.

A save sends the whole text and the revision it was based on, as ticket edits send their version. If someone saved in between, the save is refused with the plan as it is now, so no revision is lost: read it, merge, and save again. A ticket's history and the activity page record each new revision by number, never the text.

## Decisions

A decision is a question parked for the owner, a ruling an agent made, or an approval. Agents ask them as they work, with the options and their trade-offs, a recommendation, and why it matters; the owner answers in conversation with the agent, or with `vaquero decision answer` (see [the command line](/help/cli)). The API's [decisions](/help/api#decisions) and the `ask_decision`, `answer_decision` and `list_decisions` tools do the same.

- **Questions** wait for the owner. Only the owner answers one; an agent never answers a question, its own or another's.
- **Rulings** are calls an agent made and recorded as its own, already answered. The owner can overrule one; the history keeps the answer it replaced.
- **Approvals** are of a plan, a merge or a release. Merges and releases always need one. Only the option labelled Approve approves, so every approval has one; an approval with no options offers Approve and Request changes.

When the owner answers in conversation, the agent records the answer as relayed, quoting the owner's words; an answer given without an agent, in the command line or the API, is direct. Through MCP every answer is relayed, so run `vaquero mcp` with `--agent NAME`. Both count the same, and each decision shows which it was. Until Vaquero has sign-in, every agent on this machine uses your token, so this rests on trust.

Asking for a plan approval notes each ticket's requirement and plan revision as they stand. If either changes before the answer, Approve is refused ("The plan or requirement for VAQ-12 changed since this was asked; ask again."), so an approval always approves what you were shown; Request changes is still taken. Approving a plan, with Approve, records for each of the decision's tickets the requirement as approved, a hash of the ticket's title and description, the revision of the ticket's own plan (none, for a sub-ticket without one: it relies on its epic's own approval), and the ticket's sub-tickets at that moment. One approval can cover several tickets. Any other answer records no approval.

An answered approval is final. To take one back, ask for a new plan approval and answer it Request changes: the latest answered plan approval of a ticket is the one that counts. Editing the ticket's requirement or plan also leaves the approval behind.

## Readiness

With the workflow on, a ticket is ready to build only when it is clear enough and approved. On top of the board's rule (a todo, unassigned and unclaimed, nothing open blocking it, no open sub-tickets; see [the ready queue](/help/agents-and-claims#the-ready-queue)), it needs:

- a tier, any of them: a design ticket can be ready, for planning work;
- a current plan approval: the requirement and the plan as approved are still the ticket's.

Which approval counts:

- **A ticket with its own plan** needs its own approval of that plan. Its epic's plan doesn't cover it, and editing the epic leaves it alone.
- **A sub-ticket without a plan** relies on its epic's approval. Editing the epic's title, description or plan makes every such sub-ticket not ready until the epic is approved again. One approved together with its epic needs its own title and description unchanged too.
- **An epic's approval covers the sub-tickets it had when it was approved.** A sub-ticket added later, or an existing ticket moved under the epic, is not covered until the epic is approved again.
- **A ticket with no plan and no epic** can be approved as it is, for a small change.

The ready queue (`vaquero ready`, `GET /api/v1/ready`, the `ready_tickets` tool) and the board's ready mark leave out a ticket that fails this. Every ticket in the API answer, and the summary of a todo ticket in a list, then carries `readiness`: `ready`, and the `reasons` it is not, in this order:

- Not a todo.
- No tier.
- Plan not approved.
- Epic's plan not approved. (The sub-ticket's own approval holds, but its epic's doesn't, or doesn't cover it.)
- Requirement changed since approval.
- Plan changed since approval.
- Epic's requirement changed since approval.
- Epic's plan changed since approval.

With the workflow off, tickets carry no `readiness`, and ready means what the board's rule says.
