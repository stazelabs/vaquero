package db

import (
	"context"
	"database/sql"
	"fmt"
)

// SearchHit is one ticket matching a search.
type SearchHit struct {
	ProjectKey     string
	Number         int64
	Title          string
	StatusName     string
	StatusCategory string
	Snippet        string // the best-matching text; \x02 and \x03 mark the hits (content can't hold control characters)
}

// searchSQL is written by hand: sqlc can't read FTS5 virtual tables.
const searchSQL = `
WITH hits AS (
    SELECT t.id AS ticket_id, bm25(tickets_fts) AS score,
           snippet(tickets_fts, -1, char(2), char(3), '…', 12) AS snip
    FROM tickets_fts JOIN tickets t ON t.rowid = tickets_fts.rowid
    WHERE tickets_fts MATCH ?1
    UNION ALL
    SELECT c.ticket_id, bm25(comments_fts), snippet(comments_fts, 0, char(2), char(3), '…', 12)
    FROM comments_fts JOIN comments c ON c.rowid = comments_fts.rowid
    WHERE comments_fts MATCH ?1 AND c.deleted_at IS NULL
),
best AS (SELECT ticket_id, min(score) AS score FROM hits GROUP BY ticket_id)
SELECT p.key AS project_key, t.number, t.title, s.name AS status_name, s.category AS status_category,
       CAST((SELECT h.snip FROM hits h WHERE h.ticket_id = b.ticket_id ORDER BY h.score LIMIT 1) AS TEXT) AS snippet
FROM best b
JOIN tickets t ON t.id = b.ticket_id
JOIN projects p ON p.id = t.project_id
JOIN statuses s ON s.id = t.status_id
ORDER BY b.score
LIMIT ?2;`

// SearchTickets finds tickets whose title, description or a live comment
// match the FTS5 query, once each, best match first. Run it on the reader.
// query must already be a safe FTS5 expression (internal/search builds it).
func SearchTickets(ctx context.Context, db *sql.DB, query string, limit int64) ([]SearchHit, error) {
	rows, err := db.QueryContext(ctx, searchSQL, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()
	var out []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ProjectKey, &h.Number, &h.Title, &h.StatusName, &h.StatusCategory, &h.Snippet); err != nil {
			return nil, fmt.Errorf("search: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
