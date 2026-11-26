-- A ticket's plan and its revisions. plan_revisions keeps every revision,
-- the current one included, so any revision reads the same way.

-- name: GetPlanByTicket :one
SELECT * FROM plans WHERE ticket_id = ?;

-- name: CreatePlan :exec
INSERT INTO plans (id, ticket_id, revision, body, created_at, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdatePlan :exec
UPDATE plans SET revision = ?, body = ?, updated_at = ?, updated_by = ? WHERE id = ?;

-- name: AddPlanRevision :exec
INSERT INTO plan_revisions (plan_id, revision, body, at, actor_id) VALUES (?, ?, ?, ?, ?);

-- name: GetTicketPlan :one
SELECT p.*, coalesce(nullif(a.display_name, ''), a.name) AS updated_by_name
FROM plans p JOIN actors a ON a.id = p.updated_by
WHERE p.ticket_id = ?;

-- name: GetPlanRevision :one
SELECT r.*, coalesce(nullif(a.display_name, ''), a.name) AS actor_name
FROM plan_revisions r JOIN actors a ON a.id = r.actor_id
WHERE r.plan_id = ? AND r.revision = ?;

-- name: DeleteProjectPlanRevisions :exec
DELETE FROM plan_revisions WHERE plan_id IN (
    SELECT p.id FROM plans p JOIN tickets t ON t.id = p.ticket_id WHERE t.project_id = ?);

-- name: DeleteProjectPlans :exec
DELETE FROM plans WHERE ticket_id IN (SELECT id FROM tickets WHERE project_id = ?);
