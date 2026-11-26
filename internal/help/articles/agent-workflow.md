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

Each ticket can carry a tier, which says how much work it is for whoever builds it:

- **design** is spec and plan work, done before anything is built;
- **light** is small and clear;
- **standard** is ordinary work;
- **heavy** is large or tricky.

Set it on the ticket's edit form, with `vaquero ticket edit TICKET --tier standard`, through the API (`"tier"` on a ticket), or with the `update_ticket` tool. A ticket may also name an exact model (`--model`, `"model"`), which overrides the tier map for that ticket.

## The tier map

The tier map names the model that works each tier, for each harness (the agent program doing the work). It is JSON: harness, then tier, then model.

```json
{
  "claude": {"design": "…", "light": "…", "standard": "…", "heavy": "…"}
}
```

Vaquero ships a default map, and settings shows the map in use. Edit it there and save, or send `"tier_map"` in `PATCH /api/v1/projects/{key}`. Empty the box (or send `null`) to go back to the default. The design tier normally takes the heavy model.
