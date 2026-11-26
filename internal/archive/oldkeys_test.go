package archive_test

import (
	"database/sql"
	"slices"
	"testing"
)

// TestExportImportKeepsOldKeys: the seeded moved ticket's old key (OLD-2,
// now VAQ-8) survives an export and an import, still naming the ticket.
func TestExportImportKeepsOldKeys(t *testing.T) {
	d, store := seeded(t)
	const q = `SELECT p.key, k.number, t.id, tp.key, t.number, k.moved_at FROM ticket_old_keys k
		JOIN projects p ON p.id = k.project_id JOIN tickets t ON t.id = k.ticket_id JOIN projects tp ON tp.id = t.project_id
		ORDER BY p.key, k.number`
	scan := func(db *sql.DB) [][6]string {
		t.Helper()
		rows, err := db.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out [][6]string
		for rows.Next() {
			var r [6]string
			if err := rows.Scan(&r[0], &r[1], &r[2], &r[3], &r[4], &r[5]); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		return out
	}
	want := scan(d.R)
	if len(want) != 1 || want[0][0] != "OLD" || want[0][1] != "2" || want[0][3] != "VAQ" || want[0][4] != "8" || want[0][5] == "" {
		t.Fatalf("the seed needs OLD-2 moved to VAQ-8: %v", want)
	}
	loaded, err := load(t, export(t, d, store))
	if err != nil {
		t.Fatal(err)
	}
	if got := scan(loaded.R); !slices.Equal(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}
