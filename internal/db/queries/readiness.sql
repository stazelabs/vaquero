-- What internal/readiness reads to decide whether tickets are ready: each
-- query takes a batch of ticket ids, so a list costs the same few reads
-- whatever its length.

-- name: ListReadinessTickets :many
-- The ticket fields the readiness rule reads, with the status category and
-- the ticket's key (the epic to ask on).
SELECT t.id, t.project_id, p.key AS project_key, t.number, t.title, t.body, t.parent_id, s.category AS status_category
FROM tickets t
JOIN statuses s ON s.id = t.status_id
JOIN projects p ON p.id = t.project_id
WHERE t.id IN (sqlc.slice('ids'));

-- name: ListPlanRevisionsForTickets :many
-- The current plan revision of each given ticket that has a plan.
SELECT ticket_id, revision FROM plans WHERE ticket_id IN (sqlc.slice('ids'));

-- name: ListPlanApprovalAnswers :many
-- Every answered plan approval naming one of the given tickets, latest
-- answer first for each ticket, with the approval row it wrote for that
-- ticket (none: it was answered otherwise than Approve, or the ticket has
-- moved to another project since, which deletes its approvals). Only
-- decisions of the ticket's own project count.
SELECT dt.ticket_id, d.id AS decision_id,
       CAST(a.id IS NOT NULL AS BOOLEAN) AS approved,
       CAST(coalesce(a.requirement_hash, '') AS TEXT) AS requirement_hash,
       CAST(coalesce(a.plan_revision, 0) AS INTEGER) AS plan_revision,
       CAST(coalesce(a.pins, '[]') AS TEXT) AS pins
FROM decision_tickets dt
JOIN decisions d ON d.id = dt.decision_id
LEFT JOIN approvals a ON a.decision_id = d.id AND a.ticket_id = dt.ticket_id
WHERE dt.ticket_id IN (sqlc.slice('ids'))
  AND d.kind = 'approval' AND d.subject = 'plan' AND d.state = 'answered'
  AND d.project_id = (SELECT tk.project_id FROM tickets tk WHERE tk.id = dt.ticket_id)
ORDER BY dt.ticket_id, d.answered_at DESC, d.asked_at DESC, d.id DESC;

-- name: ListReadinessRefs :many
-- The references of the given tickets, which a plan approval's file pins
-- are compared with (ruling P2-R4).
SELECT ticket_id, id, target FROM ticket_refs
WHERE ticket_id IN (sqlc.slice('ids'))
ORDER BY ticket_id, created_at, id;

-- name: ListReadinessProjects :many
-- The projects whose repos hold the given approvals' pinned files.
SELECT * FROM projects WHERE id IN (sqlc.slice('ids')) ORDER BY id;
