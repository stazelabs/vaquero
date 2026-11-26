-- Decisions (questions, rulings, approvals), the tickets each names, and
-- the approvals a plan approval records. Only internal/core writes them.

-- name: CreateDecision :exec
INSERT INTO decisions (id, project_id, run_id, kind, subject, question, options, recommendation, why,
    state, answer, answer_note, answered_by, answered_at, relayed_by, relayed_words, asked_by, asked_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: AddDecisionTicket :exec
INSERT INTO decision_tickets (decision_id, ticket_id, requirement_hash, plan_revision, parent_id, children)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListDecisionTicketPins :many
-- What a plan approval asked about for each of its tickets, by number.
SELECT dt.ticket_id, dt.requirement_hash, dt.plan_revision, dt.parent_id, dt.children,
       t.title, t.body, coalesce(t.parent_id, '') AS current_parent_id, p.key AS project_key, t.number,
       t.project_id
FROM decision_tickets dt
JOIN tickets t ON t.id = dt.ticket_id
JOIN projects p ON p.id = t.project_id
WHERE dt.decision_id = ?
ORDER BY t.number;

-- name: GetDecision :one
SELECT * FROM decisions WHERE id = ?;

-- name: AnswerDecision :exec
UPDATE decisions SET state = 'answered', answer = ?, answer_note = ?, answered_by = ?, answered_at = ?,
    relayed_by = ?, relayed_words = ?
WHERE id = ?;

-- name: WithdrawDecision :exec
UPDATE decisions SET state = 'withdrawn' WHERE id = ?;

-- name: ListDecisionTicketIDs :many
SELECT ticket_id FROM decision_tickets WHERE decision_id = ? ORDER BY ticket_id;

-- name: ListDecisionTicketKeys :many
-- The tickets a decision names, by key, in ticket number order.
SELECT p.key AS project_key, t.number
FROM decision_tickets dt
JOIN tickets t ON t.id = dt.ticket_id
JOIN projects p ON p.id = t.project_id
WHERE dt.decision_id = ?
ORDER BY t.number;

-- name: ListDecisions :many
-- Newest first, optionally narrowed to a project, a state, a kind or a
-- ticket.
SELECT d.* FROM decisions d
WHERE (sqlc.narg(project_id) IS NULL OR d.project_id = sqlc.narg(project_id))
  AND (sqlc.narg(state) IS NULL OR d.state = sqlc.narg(state))
  AND (sqlc.narg(kind) IS NULL OR d.kind = sqlc.narg(kind))
  AND (sqlc.narg(ticket_id) IS NULL OR EXISTS (
      SELECT 1 FROM decision_tickets dt WHERE dt.decision_id = d.id AND dt.ticket_id = sqlc.narg(ticket_id)))
ORDER BY d.asked_at DESC, d.id DESC
LIMIT sqlc.arg(lim);

-- name: AddApproval :exec
INSERT INTO approvals (id, ticket_id, decision_id, requirement_hash, plan_revision, pins, at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListDecisionApprovals :many
SELECT a.*, p.key AS project_key, t.number
FROM approvals a
JOIN tickets t ON t.id = a.ticket_id
JOIN projects p ON p.id = t.project_id
WHERE a.decision_id = ?
ORDER BY t.number;

-- name: DeleteProjectApprovals :exec
DELETE FROM approvals
WHERE ticket_id IN (SELECT tk.id FROM tickets tk WHERE tk.project_id = sqlc.arg(project))
   OR decision_id IN (SELECT dc.id FROM decisions dc WHERE dc.project_id = sqlc.arg(project));

-- name: DeleteProjectDecisionTickets :exec
DELETE FROM decision_tickets
WHERE ticket_id IN (SELECT tk.id FROM tickets tk WHERE tk.project_id = sqlc.arg(project))
   OR decision_id IN (SELECT dc.id FROM decisions dc WHERE dc.project_id = sqlc.arg(project));

-- name: DeleteProjectDecisions :exec
DELETE FROM decisions WHERE project_id = ?;

-- name: CountOpenDecisionsNaming :one
-- Open decisions naming any of the given tickets (a ticket that moves
-- leaves them in its old project).
SELECT count(DISTINCT d.id) FROM decisions d
JOIN decision_tickets dt ON dt.decision_id = d.id
WHERE d.state = 'open' AND dt.ticket_id IN (sqlc.slice('ids'));
