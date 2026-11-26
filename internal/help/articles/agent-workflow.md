---
title: Agent workflow
section: api-and-agents
summary: The project setting that has agents work tickets with tiers and a map from each tier to a model.
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

With the workflow on, set it on the ticket's edit form, and see it on the ticket's page. In any project, the command line (`vaquero ticket edit TICKET --tier standard`), the API (`"tier"` on a ticket) and the `create_ticket` and `update_ticket` tools set it too. A ticket may also name an exact model (`--model`, `"model"`), which overrides the tier map for that ticket.

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
