-- +goose Up
-- VAQ-172, ruling T-R4: a decision's recommended option, the label an agent
-- recommends when it asks ('' for none); recommendation stays the free-text
-- why. Older decisions have none.
ALTER TABLE decisions ADD COLUMN recommended TEXT NOT NULL DEFAULT '' CHECK (length(recommended) <= 200);

-- The inbox's Check lane reads lapsed claims of the last day (VAQ-171).
CREATE INDEX events_claim_lapsed ON events (at) WHERE kind = 'ticket.claim_lapsed';

-- +goose Down
SELECT 1;
