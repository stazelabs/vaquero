-- name: ClaimTicket :exec
UPDATE tickets SET claimed_by = ?, claim_expires_at = ?, claim_prev_status_id = ?, assignee_id = ?,
       status_id = ?, position = ?, status_changed_at = ?, updated_at = ?, version = version + 1
WHERE id = ?;

-- name: ExtendClaim :exec
-- A heartbeat: the lease only, so the ticket's version stays.
UPDATE tickets SET claim_expires_at = ? WHERE id = ?;

-- name: ReleaseClaim :exec
UPDATE tickets SET claimed_by = NULL, claim_expires_at = NULL, claim_prev_status_id = NULL,
       updated_at = ?, version = version + 1
WHERE id = ?;

-- name: ListLapsedClaims :many
SELECT * FROM tickets WHERE claimed_by IS NOT NULL AND claim_expires_at <= ? ORDER BY claim_expires_at, id;

-- name: LapseClaim :exec
UPDATE tickets SET claimed_by = NULL, claim_expires_at = NULL, claim_prev_status_id = NULL,
       status_id = ?, assignee_id = ?, position = ?, status_changed_at = ?, updated_at = ?, version = version + 1
WHERE id = ?;

-- name: FirstStatusInCategory :one
SELECT * FROM statuses WHERE project_id = ? AND category = ? ORDER BY position LIMIT 1;
