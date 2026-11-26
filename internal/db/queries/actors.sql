-- name: CreateActor :one
INSERT INTO actors (id, kind, name, created_at) VALUES (?, ?, ?, ?) RETURNING *;

-- name: GetActor :one
SELECT * FROM actors WHERE id = ?;

-- name: GetActorByKindName :one
SELECT * FROM actors WHERE kind = ? AND name = ?;

-- name: ListActors :many
SELECT * FROM actors ORDER BY kind, name;

-- name: ListActorsByIDs :many
-- The given actors, in one read (a page of decisions' people).
SELECT * FROM actors WHERE id IN (sqlc.slice('ids'));
