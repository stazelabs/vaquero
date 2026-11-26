-- What internal/readiness reads to decide whether tickets are ready: each
-- query takes a batch of ticket ids, so a list costs the same few reads
-- whatever its length.

-- name: ListReadinessTickets :many
-- The ticket fields the readiness rule reads, with the status category.
SELECT t.id, t.title, t.body, t.tier, t.parent_id, s.category AS status_category
FROM tickets t
JOIN statuses s ON s.id = t.status_id
WHERE t.id IN (sqlc.slice('ids'));

-- name: ListPlanRevisionsForTickets :many
-- The current plan revision of each given ticket that has a plan.
SELECT ticket_id, revision FROM plans WHERE ticket_id IN (sqlc.slice('ids'));

-- name: ListPlanApprovalAnswers :many
-- Every answered plan approval naming one of the given tickets, latest
-- answer first for each ticket, with the approval row it wrote for that
-- ticket (none: it was answered otherwise than Approve).
SELECT dt.ticket_id, d.id AS decision_id,
       CAST(a.id IS NOT NULL AS BOOLEAN) AS approved,
       CAST(coalesce(a.requirement_hash, '') AS TEXT) AS requirement_hash,
       CAST(coalesce(a.plan_revision, 0) AS INTEGER) AS plan_revision
FROM decision_tickets dt
JOIN decisions d ON d.id = dt.decision_id
LEFT JOIN approvals a ON a.decision_id = d.id AND a.ticket_id = dt.ticket_id
WHERE dt.ticket_id IN (sqlc.slice('ids'))
  AND d.kind = 'approval' AND d.subject = 'plan' AND d.state = 'answered'
ORDER BY dt.ticket_id, d.answered_at DESC, d.asked_at DESC, d.id DESC;
