package archive_test

import (
	"database/sql"
	"slices"
	"strings"
	"testing"
)

// TestExportImportKeepsDecisionsAndApprovals: the seeded decisions, the
// tickets they name and the approvals answering them survive an export and
// an import unchanged.
func TestExportImportKeepsDecisionsAndApprovals(t *testing.T) {
	d, store := seeded(t)
	queries := []string{
		`SELECT id, project_id, ifnull(run_id, '-'), kind, subject, question, options, recommendation, why, state, answer,
			answer_note, ifnull(answered_by, '-'), ifnull(answered_at, '-'), ifnull(relayed_by, '-'), relayed_words, asked_by, asked_at
			FROM decisions ORDER BY id`,
		`SELECT decision_id, ticket_id FROM decision_tickets ORDER BY decision_id, ticket_id`,
		`SELECT id, ticket_id, decision_id, requirement_hash, plan_revision, pins, at FROM approvals ORDER BY id`,
	}
	scan := func(db *sql.DB) []string {
		t.Helper()
		var out []string
		for _, q := range queries {
			rows, err := db.Query(q)
			if err != nil {
				t.Fatal(err)
			}
			cols, _ := rows.Columns()
			for rows.Next() {
				vals := make([]string, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					t.Fatal(err)
				}
				out = append(out, strings.Join(vals, "|"))
			}
			rows.Close()
			out = append(out, "--")
		}
		return out
	}
	want := scan(d.R)
	var approvals, relayed int
	_ = d.R.QueryRow(`SELECT count(*) FROM approvals`).Scan(&approvals)
	_ = d.R.QueryRow(`SELECT count(*) FROM decisions WHERE relayed_by IS NOT NULL`).Scan(&relayed)
	if approvals == 0 || relayed == 0 {
		t.Fatalf("the seed has %d approvals and %d relayed answers", approvals, relayed)
	}
	loaded, err := load(t, export(t, d, store))
	if err != nil {
		t.Fatal(err)
	}
	if got := scan(loaded.R); !slices.Equal(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}
