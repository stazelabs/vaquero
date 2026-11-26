package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// SearchHit is one ticket matching a search.
type SearchHit struct {
	// The ticket's project: its key, and its id, name and icon for the
	// icon drawn beside the key.
	ProjectID, ProjectName, IconKind, IconGlyph string
	ProjectKey                                  string
	Number                                      int64
	Title                                       string
	StatusName                                  string
	StatusCategory                              string
	Snippet                                     string // \x02 and \x03 mark the hits (content can't hold control characters); "" for none
	Total                                       int64  // how many tickets matched in all, the same on every row
}

// Where a term was found, as weights. A ticket's score is, for each term,
// the weight of the best place it was found, summed over the terms: a
// title hit beats a description hit beats a comment hit, and more terms in
// better places beat fewer. Ties go to the most recently updated ticket.
// This explicit score replaces comparing bm25 across the two FTS tables,
// whose scales differ (each table's bm25 depends on its own row count and
// lengths), and ignores how often a term repeats, so a long comment
// can't outrank a title.
const (
	weightTitle   = 4
	weightBody    = 2
	weightComment = 1
)

// snippetLen is the description's opening shown when a ticket matched on
// its title alone (in characters).
const snippetLen = 160

// termCTE finds the tickets one term matches, with the best place's weight:
// the title or body column of tickets_fts (FTS5 column filters), or a live
// comment. It takes three parameters: "title : term", "body : term", term.
func termCTE(i int) string {
	return fmt.Sprintf(`
m%[1]d AS (
    SELECT ticket_id, max(w) AS w FROM (
        SELECT t.id AS ticket_id, %[2]d AS w FROM tickets_fts JOIN tickets t ON t.rowid = tickets_fts.rowid WHERE tickets_fts MATCH ?
        UNION ALL
        SELECT t.id, %[3]d FROM tickets_fts JOIN tickets t ON t.rowid = tickets_fts.rowid WHERE tickets_fts MATCH ?
        UNION ALL
        SELECT c.ticket_id, %[4]d FROM comments_fts JOIN comments c ON c.rowid = comments_fts.rowid
        WHERE comments_fts MATCH ? AND c.deleted_at IS NULL
    ) GROUP BY ticket_id
)`, i, weightTitle, weightBody, weightComment)
}

// searchSQL is written by hand, because sqlc can't read FTS5 virtual
// tables, and built for the number of terms: one CTE per term, joined so a
// ticket matches when every term is found somewhere in it (its title, its
// description or any live comment). Only "?" placeholders are added; the
// terms themselves are always bound parameters.
func searchSQL(n int) string {
	var b strings.Builder
	b.WriteString("WITH")
	for i := range n {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(termCTE(i))
	}
	b.WriteString(",\nscored AS (SELECT m0.ticket_id, m0.w")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&b, " + m%d.w", i)
	}
	b.WriteString(" AS score FROM m0")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&b, " JOIN m%d USING (ticket_id)", i)
	}
	b.WriteString(")")
	// top ranks, counts and cuts; the snippets are made only for the rows
	// it keeps. The snippet comes from the description when it matches,
	// else the best matching live comment, else the description's opening
	// (a title hit never repeats the title). Comment snippets are made in
	// one pass over comments_fts for the kept tickets, the best per ticket
	// by bm25 (VAQ-133), not by a MATCH run again for each row.
	fmt.Fprintf(&b, `,
top AS MATERIALIZED (
    SELECT t.rowid, t.id, t.body, p.id AS project_id, p.name AS project_name, p.icon_kind, p.icon_glyph, p.key AS project_key, t.number, t.title, s.name AS status_name,
           s.category AS status_category, x.score, t.updated_at, count(*) OVER () AS total
    FROM scored x
    JOIN tickets t ON t.id = x.ticket_id
    JOIN projects p ON p.id = t.project_id
    JOIN statuses s ON s.id = t.status_id
    ORDER BY x.score DESC, t.updated_at DESC, t.number
    LIMIT ?
),
hits AS MATERIALIZED (
    SELECT c.ticket_id, c.rowid AS crow, snippet(comments_fts, 0, char(2), char(3), '…', 12) AS snip, bm25(comments_fts) AS rank
    FROM comments_fts JOIN comments c ON c.rowid = comments_fts.rowid
    WHERE comments_fts MATCH ? AND c.deleted_at IS NULL AND c.ticket_id IN (SELECT id FROM top)
),
csnip AS (
    SELECT ticket_id, snip FROM (
        SELECT ticket_id, snip, row_number() OVER (PARTITION BY ticket_id ORDER BY rank, crow) AS rn FROM hits
    ) WHERE rn = 1
)
SELECT top.project_id, top.project_name, top.icon_kind, top.icon_glyph, top.project_key, top.number, top.title, top.status_name, top.status_category,
       CAST(coalesce(
           (SELECT snippet(tickets_fts, 1, char(2), char(3), '…', 12) FROM tickets_fts
            WHERE tickets_fts MATCH ? AND tickets_fts.rowid = top.rowid),
           cs.snip,
           CASE WHEN length(top.body) > %[1]d THEN substr(top.body, 1, %[1]d) || '…' ELSE top.body END
       ) AS TEXT) AS snippet,
       top.total
FROM top
LEFT JOIN csnip cs ON cs.ticket_id = top.id
ORDER BY top.score DESC, top.updated_at DESC, top.number;`, snippetLen)
	return b.String()
}

// SearchTickets finds the tickets in which every term matches somewhere in
// the title, description or a live comment, best first (see the weights),
// at most limit of them; each hit also carries the total. Run it on the
// reader. Each term must already be a safe FTS5 term, a quoted prefix term
// like "colum"* (internal/search builds them).
func SearchTickets(ctx context.Context, db *sql.DB, terms []string, limit int64) ([]SearchHit, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	args := make([]any, 0, 3*len(terms)+3)
	for _, t := range terms {
		args = append(args, "title : "+t, "body : "+t, t)
	}
	// The snippets mark every term: any of them, in the body or a comment.
	either := "(" + strings.Join(terms, " OR ") + ")"
	args = append(args, limit, either, "body : "+either) // in the order they appear in the text
	rows, err := db.QueryContext(ctx, searchSQL(len(terms)), args...)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()
	var out []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ProjectID, &h.ProjectName, &h.IconKind, &h.IconGlyph, &h.ProjectKey, &h.Number, &h.Title, &h.StatusName, &h.StatusCategory, &h.Snippet, &h.Total); err != nil {
			return nil, fmt.Errorf("search: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
