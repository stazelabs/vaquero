---
name: plan
title: Plan a ticket
description: Write a ticket's plan and ask the owner to approve it, without building anything.
args:
  - name: ticket
    description: The ticket to plan, by key, such as VAQ-12.
    required: true
---
# Plan {{ticket}}

Read {{ticket}} with `get_ticket`, and its current plan, if it has one, with `get_plan`. Write the plan with `save_plan`, then ask the owner to approve it with `ask_decision`: kind approval, subject plan, the ticket {{ticket}}. Don't build anything, and never answer the approval yourself; only the owner answers it.

From a shell, `vaquero ticket show {{ticket}}` and `vaquero plan save {{ticket}} -` do the same.
