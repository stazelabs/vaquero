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

Asking for a plan approval notes, for each ticket, its requirement, its plan revision, its epic, and the sub-tickets it covers (its sub-tickets, theirs and so on, except any with a plan of its own and what is under it) with their requirements, as they stand. If any of that changes before the answer (a sub-ticket added, removed or edited, or the ticket moved to another epic), Approve is refused ("The plan or requirement for VAQ-12 changed since this was asked; ask again."), so an approval always approves what you were shown; Request changes is still taken. `vaquero decision show` and the API's decision list, under "Approving covers" and `asked`, what an Approve covers: each ticket's plan revision, its epic and its sub-tickets. Approving a plan, with Approve, records for each of the decision's tickets the requirement as approved, a hash of the ticket's title and description, the revision of the ticket's own plan (none, for a sub-ticket without one: it relies on its epic's own approval), and the sub-tickets it covers as you were asked about them. One approval can cover several tickets. Any other answer records no approval.

A plan approval also pins the ticket's files: every [reference](/help/repositories) to a repo path, at the main branch's current commit, with the file's content then. URL references aren't pinned. Asking is refused when a referenced file isn't on main ("docs/x.md isn't on main; merge it before asking for approval."), or when the repository can't be read. If a pinned file's content changes on main before the answer, or the ticket gains or loses a repo-path reference, Approve is refused ("A file VAQ-12 references changed since this was asked; ask again."); a commit that leaves the file's content as it was changes nothing.

An answered approval is final. To take one back, ask for a new plan approval and answer it Request changes: the latest answered plan approval of a ticket is the one that counts. Editing the ticket's requirement or plan also leaves the approval behind.

## Readiness

With the workflow on, a ticket is ready when it is a todo the board calls ready (unassigned and unclaimed, nothing open blocking it, no open sub-tickets; see [the ready queue](/help/agents-and-claims#the-ready-queue)) and its plan is approved. The tier doesn't gate it: a ticket with none counts as standard.

A ticket's approval is its own when it has a plan, or when a plan approval naming it was answered. Otherwise it is the approval of its nearest epic that has a plan or an answered plan approval of its own, which covers the sub-tickets, and theirs, that the epic had when it was asked, each as it was then; a sub-ticket with a plan of its own isn't covered and needs its own approval. So approving an epic with no plan covers its sub-tickets, and theirs, just as approving one with a plan does. A cancelled epic covers nothing; a done one still does. A sub-ticket with repo-path references of its own needs its own approval too.

An approval goes stale when anything it covered changes: the title or description, the plan, a referenced file on main, the ticket's epic, or a covered sub-ticket. Moving a ticket to another project clears its approvals, so it needs approving again there, and again if it comes back.

A ticket that isn't ready says why, as what to do:

- Ask for plan approval.
- Changed since approval: {what}. Ask again.
- Its epic changed since approval: {what}. Ask again.
- Can't check its files: {why}.

{what} is the description, the plan, its parent, or a file's path; {why} is that git isn't installed, git 2.31 or later is needed, git's version couldn't be read, no repository is set, the repository settings changed since approval, or the repository can't be read. A ticket whose files can't be checked is never ready; when that is because the repository settings changed, asking again pins the files from the repository as it is set now.

The ticket page shows a todo's readiness in its side panel: Ready, or the one reason. Where the reason is to ask for approval, or to ask again, an Ask for approval button asks a plan approval of the ticket (or of its epic, when that is where to ask); the open approval then shows on the page with a button for each option and a note, and the owner answers it there. A ticket's own open plan approval shows whatever its status. When the ticket or anything the approval covers changes while it waits, Approve is refused; Ask again withdraws the waiting approval and asks a new one about the ticket as it is now. The inbox will take this over.

The ready queue (`vaquero ready`, `GET /api/v1/ready`, the `ready_tickets` tool) and the board's ready mark leave out a ticket that isn't ready. Every ticket in the API answer, and the summary of a todo ticket in a list, carries `readiness`: `ready`, the `reasons` (one at most, none for a ticket that isn't a todo), `action` (`ask` or `ask_again`) and `epic` (the key of the epic to ask on, when the approval to ask for is the epic's). When the ticket has repo files, `readiness` also has `files`: each file's `path` as pinned and its `state`: `unchanged`, `changed`, `renamed` (with `now`, its path on main), `gone` (no longer on main, or no longer referenced), `added` (referenced, but not pinned by the approval) or `unchecked`. `vaquero ticket show` lists them under Files. Opened from the ticket's reference, the file view says when a file changed since the approval, with Show changes, was renamed or removed, or can't be checked.

With the workflow off, tickets carry no `readiness`, and ready means what the board's rule says.
