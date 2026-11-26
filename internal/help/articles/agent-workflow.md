---
title: Agent workflow
section: api-and-agents
summary: The project setting for agent work: execution tiers and the tier map, plans, the decisions the owner answers, and when a ticket is ready.
order: 5
---
The agent workflow is a project setting for teams where agents do much of the building. It is off in a new project, and a project with it off works as it always has. Turn it on in the project's settings, under Workflow, or with `vaquero project edit KEY --workflow on`.

Only you, the owner, turn the workflow on or off and change the [tier map](#the-tier-map), acting yourself: a request made as an agent (with `--agent`, a `Vaquero-Agent` header, or through MCP) is refused with "Only the owner turns the agent workflow on or off." For the same reason an agent can't move a ticket out of a project with the workflow on into one with it off; only you can.

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

Asking for a plan approval notes, for each ticket, its requirement, its plan revision, its epic, and its sub-tickets with their requirements, as they stand. If any of that changes before the answer (a sub-ticket added, removed or edited, or the ticket moved to another epic), Approve is refused ("The plan or requirement for VAQ-12 changed since this was asked; ask again."), so an approval always approves what you were shown; Request changes is still taken. `vaquero decision show` and the API's decision list, under "Approving covers" and `asked`, what an Approve covers: each ticket's plan revision, its epic and its sub-tickets. Approving a plan, with Approve, records for each of the decision's tickets the requirement as approved, a hash of the ticket's title and description, the revision of the ticket's own plan (none, for a sub-ticket without one: it relies on its epic's own approval), and the ticket's sub-tickets as you were asked about them. One approval can cover several tickets. Any other answer records no approval.

A plan approval also pins the ticket's files: every [reference](/help/repositories) to a repo path, at the main branch's current commit, with the file's content then. URL references aren't pinned. Asking is refused when a referenced file isn't on main ("docs/x.md isn't on main; merge it before asking for approval."), or when the repository can't be read. If a pinned file's content changes on main before the answer, or the ticket gains or loses a repo-path reference, Approve is refused ("A file VAQ-12 references changed since this was asked; ask again."); a commit that leaves the file's content as it was changes nothing.

An answered approval is final. To take one back, ask for a new plan approval and answer it Request changes: the latest answered plan approval of a ticket is the one that counts. Editing the ticket's requirement or plan also leaves the approval behind.

## Readiness

With the workflow on, a ticket is ready to build only when it is clear enough and approved. On top of the board's rule (a todo, unassigned and unclaimed, nothing open blocking it, no open sub-tickets; see [the ready queue](/help/agents-and-claims#the-ready-queue)), it needs:

- a tier, any of them: a design ticket can be ready, for planning work;
- a current plan approval: the requirement, the plan and the pinned files as approved are still the ticket's.

Which approval counts:

- **A ticket with its own plan** needs its own approval of that plan. Its epic's plan doesn't cover it, and editing the epic leaves it alone.
- **A sub-ticket with no plan and no approval of its own** relies on its epic's approval, which covers the sub-tickets you were asked about with the epic, each as it was then. A sub-ticket added later, or an existing ticket moved under the epic, is not covered until the epic is approved again; nor is one whose title or description changed since ("Requirement changed since approval."). Editing the epic's title, description or plan makes every such sub-ticket not ready until the epic is approved again.
- **A sub-ticket with an approval of its own but no plan** needs its own title and description unchanged, and its epic's approval to be current, though the epic's approval need not list it. Without an approved epic, the reason is "Epic's plan not approved."
- **A ticket with no plan and no epic** can be approved as it is, for a small change.
- **An approval holds under the epic it was asked under.** A ticket moved to another epic, out of its epic, or under an epic when it had none is not ready ("Epic's plan not approved.") until it is approved again where it now is.
- **Leaning goes up the tree.** When a ticket's epic has no plan of its own, the epic relies on its own epic in turn, up to the first one with a plan, and every approval on the way must be current. Revising the plan at the top makes the grandchildren not ready too, with the reason for the link that broke: "Epic's requirement changed since approval.", "Epic's plan changed since approval." or "Epic's plan not approved."
- **Pinned files must be as approved.** Files are judged by their content on main, with renames followed: a pinned file whose content changed, that left main, or a repo-path reference added or removed since the approval makes the ticket not ready ("Files changed since approval."); a rename alone doesn't. A ticket leaning on its epic is not ready when the epic's pinned files changed ("Epic's files changed since approval."). A sub-ticket's own repo-path references aren't covered by its epic's approval: with any, it needs an approval of its own. When the files can't be checked (no repository set, git not installed, or the repository can't be read) the ticket is never ready ("Files can't be checked.").
- **A move to another project voids it.** An approval counts only if it was asked after the ticket last arrived in its project; and when a ticket leans on an epic's approval, any ticket in the chain from it up to that epic moving to another project after the approval was asked voids it ("Plan not approved."), even if the ticket came back or was detached and attached again.

The ready queue (`vaquero ready`, `GET /api/v1/ready`, the `ready_tickets` tool) and the board's ready mark leave out a ticket that fails this. Every ticket in the API answer, and the summary of a todo ticket in a list, then carries `readiness`: `ready`, and the `reasons` it is not, in this order:

- Not a todo.
- No tier.
- Plan not approved.
- Epic's plan not approved. (The sub-ticket's own approval holds, but its epic has no current approval.)
- Requirement changed since approval.
- Plan changed since approval.
- Epic's requirement changed since approval.
- Epic's plan changed since approval.
- Files changed since approval.
- Epic's files changed since approval.
- Files can't be checked.

When the ticket's readiness rests on repo files, `readiness` also has `files`: each file's `path` as pinned and its `state`: `unchanged`, `changed`, `renamed` (with `now`, its path on main), `gone` (no longer on main, or no longer referenced), `added` (referenced, but not pinned by the approval) or `unchecked`. `vaquero ticket show` lists them under Files. Opened from the ticket's reference, the file view says when a file changed since the approval, with Show changes, or was renamed or removed.

With the workflow off, tickets carry no `readiness`, and ready means what the board's rule says.
