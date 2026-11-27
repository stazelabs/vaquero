---
name: audit
title: Audit a project
description: Run one kind of audit over a project and file what it finds as tickets, without fixing anything.
args:
  - name: project
    description: The project to audit, by key, such as VAQ.
    required: true
  - name: kind
    description: The kind of audit.
    required: true
    values: qa, hygiene, continuity, docs
---
# Audit {{project}}: {{kind}}

Print the {{kind}} pack template with `vaquero workflow pack {{kind}}` and follow the project's pack for this kind. Read the project's open tickets with `list_tickets` (the API's `GET /api/v1/projects/{key}/tickets`) so you don't file a finding twice, and file each new finding as a ticket with `create_ticket` in {{project}}. Audit only: never fix what you find.
