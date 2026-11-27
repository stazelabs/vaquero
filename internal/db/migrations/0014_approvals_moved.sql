-- +goose Up
-- Ruling R-Q13 (VAQ-160): a move to another project clears the approvals
-- of every ticket it re-keys, and takes those tickets out of the
-- descendants other approvals cover (core's clearApprovals). Before this
-- release a move kept them, so a ticket moved away and back was ready on
-- an approval asked before it left. This applies the rule to that
-- history: an approval asked before its ticket's latest move goes, and a
-- covered descendant moved after an approval was asked leaves its
-- children. Every other pin field, and every other row, stays as it is.

-- The statements below also run after an import (db.ClearMovedApprovals),
-- which reads them from this file between the two markers. A second run
-- changes nothing.
-- backfill:begin
DELETE FROM approvals
WHERE EXISTS (
    SELECT 1 FROM ticket_old_keys k JOIN decisions d ON d.id = approvals.decision_id
    WHERE k.ticket_id = approvals.ticket_id AND k.moved_at > d.asked_at);
UPDATE approvals SET pins = json_set(pins, '$.children', (
    SELECT json_group_array(json(kept.value)) FROM (
        SELECT c.value FROM json_each(approvals.pins, '$.children') c
        WHERE NOT EXISTS (
            SELECT 1 FROM ticket_old_keys k JOIN decisions d ON d.id = approvals.decision_id
            WHERE k.ticket_id = json_extract(c.value, '$.id') AND k.moved_at > d.asked_at)
        ORDER BY c.key) kept))
WHERE json_type(pins, '$.children') = 'array' AND EXISTS (
    SELECT 1 FROM json_each(approvals.pins, '$.children') c
    JOIN ticket_old_keys k ON k.ticket_id = json_extract(c.value, '$.id')
    JOIN decisions d ON d.id = approvals.decision_id
    WHERE k.moved_at > d.asked_at);
-- backfill:end

-- +goose Down
SELECT 1;
