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

-- name: SetActorDisplayName :exec
-- Only internal/core writes actors (VAQ-149: the owner's profile).
UPDATE actors SET display_name = ? WHERE id = ?;

-- name: GetActorAvatar :one
SELECT * FROM actor_avatars WHERE actor_id = ?;

-- name: GetActorAvatarEtag :one
SELECT etag FROM actor_avatars WHERE actor_id = ?;

-- name: SaveActorAvatar :exec
INSERT INTO actor_avatars (actor_id, content_type, data, etag, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (actor_id) DO UPDATE SET content_type = excluded.content_type, data = excluded.data,
    etag = excluded.etag, updated_at = excluded.updated_at;

-- name: DeleteActorAvatar :exec
DELETE FROM actor_avatars WHERE actor_id = ?;

-- name: ListActorFaces :many
-- Every actor's shown name and avatar etag ('' for none), for drawing
-- avatars beside names; the table is small (the owner and its agents).
SELECT a.id, a.kind, a.name, a.display_name, coalesce(v.etag, '') AS avatar_etag
FROM actors a LEFT JOIN actor_avatars v ON v.actor_id = a.id;
