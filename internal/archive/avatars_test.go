package archive_test

import (
	"database/sql"
	"slices"
	"testing"
)

// TestExportImportKeepsTheProfile (VAQ-149): the owner's display name and
// avatar survive an export and an import unchanged.
func TestExportImportKeepsTheProfile(t *testing.T) {
	d, store := seeded(t)
	const q = `SELECT a.id, a.name, a.display_name, coalesce(v.content_type, ''), coalesce(v.data, ''), coalesce(v.etag, ''), coalesce(v.updated_at, '')
		FROM actors a LEFT JOIN actor_avatars v ON v.actor_id = a.id ORDER BY a.id`
	scan := func(db *sql.DB) [][7]string {
		t.Helper()
		rows, err := db.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out [][7]string
		for rows.Next() {
			var r [7]string
			if err := rows.Scan(&r[0], &r[1], &r[2], &r[3], &r[4], &r[5], &r[6]); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		return out
	}
	want := scan(d.R)
	if !slices.ContainsFunc(want, func(r [7]string) bool { return r[1] == "Owner" && r[2] != "" && r[4] != "" }) {
		t.Fatalf("the seed needs the owner's display name and avatar: %v", want)
	}
	loaded, err := load(t, export(t, d, store))
	if err != nil {
		t.Fatal(err)
	}
	if got := scan(loaded.R); !slices.Equal(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}
