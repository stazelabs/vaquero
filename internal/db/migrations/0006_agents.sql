-- +goose Up
-- Claims: an agent holds a ticket until claim_expires_at, and the status it
-- was taken from is kept so a lapsed claim can put it back. mcp_calls logs
-- every MCP tool call for the admin page.

ALTER TABLE tickets ADD COLUMN claimed_by TEXT REFERENCES actors (id);
ALTER TABLE tickets ADD COLUMN claim_expires_at TEXT;
-- A deleted status forgets it; a lapse then returns the ticket to the first todo status.
ALTER TABLE tickets ADD COLUMN claim_prev_status_id TEXT REFERENCES statuses (id) ON DELETE SET NULL;
CREATE INDEX tickets_claim_expiry ON tickets (claim_expires_at) WHERE claimed_by IS NOT NULL;

-- A ticket that reaches done or cancelled, by any path, ends its claim:
-- the work is over, so the claim can no longer lapse and move it back.
-- +goose StatementBegin
CREATE TRIGGER tickets_claim_ends AFTER UPDATE OF status_id ON tickets
WHEN NEW.claimed_by IS NOT NULL
 AND (SELECT category FROM statuses WHERE id = NEW.status_id) IN ('done', 'cancelled')
BEGIN
    UPDATE tickets SET claimed_by = NULL, claim_expires_at = NULL, claim_prev_status_id = NULL WHERE id = NEW.id;
END;
-- +goose StatementEnd

CREATE TABLE mcp_calls (
    id           TEXT PRIMARY KEY,
    at           TEXT NOT NULL,
    actor_id     TEXT REFERENCES actors (id),
    token_prefix TEXT NOT NULL DEFAULT '',
    tool         TEXT NOT NULL,
    args         TEXT NOT NULL DEFAULT '',
    outcome      TEXT NOT NULL,
    message      TEXT NOT NULL DEFAULT '',
    duration_ms  INTEGER NOT NULL
);
CREATE INDEX mcp_calls_at ON mcp_calls (at);

-- +goose Down
SELECT 1;
