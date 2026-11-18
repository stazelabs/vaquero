-- name: CreateMilestone :one
INSERT INTO milestones (id, project_id, name, description, due_on, created_at)
VALUES (?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetMilestone :one
SELECT * FROM milestones WHERE id = ?;

-- name: ListMilestones :many
-- Open milestones first (by due date, undated last), then closed ones.
SELECT * FROM milestones WHERE project_id = ?
ORDER BY closed_at IS NOT NULL, due_on IS NULL, due_on, name;

-- name: UpdateMilestone :exec
UPDATE milestones SET name = ?, description = ?, due_on = ? WHERE id = ?;

-- name: SetMilestoneClosed :exec
UPDATE milestones SET closed_at = ? WHERE id = ?;
