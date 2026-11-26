package archive_test

import (
	"database/sql"
	"slices"
	"testing"
)

// TestExportImportKeepsProjectLinks: the seeded project link (VAQ depends
// on OLD, VAQ-119) survives an export and an import whole.
func TestExportImportKeepsProjectLinks(t *testing.T) {
	d, store := seeded(t)
	const q = `SELECT l.id, f.key, t.key, l.kind, l.created_at, a.name FROM project_links l
		JOIN projects f ON f.id = l.from_project JOIN projects t ON t.id = l.to_project JOIN actors a ON a.id = l.created_by
		ORDER BY l.id`
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
	if len(want) != 1 || want[0][1] != "VAQ" || want[0][2] != "OLD" || want[0][3] != "depends on" || want[0][4] == "" {
		t.Fatalf("the seed needs VAQ depending on OLD: %v", want)
	}
	loaded, err := load(t, export(t, d, store))
	if err != nil {
		t.Fatal(err)
	}
	if got := scan(loaded.R); !slices.Equal(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}
