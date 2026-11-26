package archive_test

import (
	"database/sql"
	"slices"
	"testing"
)

// TestExportImportKeepsRefs: the seed's two references (a repo path with an
// anchor and label, and a URL) survive an export and an import.
func TestExportImportKeepsRefs(t *testing.T) {
	d, store := seeded(t)
	const q = `SELECT r.id, p.key, t.number, r.kind, r.target, r.anchor, r.label, r.created_by, r.created_at
		FROM ticket_refs r JOIN tickets t ON t.id = r.ticket_id JOIN projects p ON p.id = t.project_id ORDER BY r.id`
	scan := func(db *sql.DB) [][9]string {
		t.Helper()
		rows, err := db.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out [][9]string
		for rows.Next() {
			var r [9]string
			var n int
			if err := rows.Scan(&r[0], &r[1], &n, &r[3], &r[4], &r[5], &r[6], &r[7], &r[8]); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		return out
	}
	want := scan(d.R)
	if len(want) != 2 {
		t.Fatalf("the seed needs a repo reference and a URL reference: %v", want)
	}
	loaded, err := load(t, export(t, d, store))
	if err != nil {
		t.Fatal(err)
	}
	if got := scan(loaded.R); !slices.Equal(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}
