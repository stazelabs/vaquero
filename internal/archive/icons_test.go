package archive_test

import (
	"database/sql"
	"testing"
)

// TestExportImportKeepsProjectIcons: the seeded project's icon (its kind,
// glyph and stored image) survives an export and an import unchanged.
func TestExportImportKeepsProjectIcons(t *testing.T) {
	d, store := seeded(t)
	const q = `SELECT p.key, p.icon_kind, p.icon_glyph, coalesce(i.type, ''), coalesce(i.data, ''), coalesce(i.etag, ''), coalesce(i.updated_at, '')
		FROM projects p LEFT JOIN project_icons i ON i.project_id = p.id ORDER BY p.key`
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
	images, glyphs := 0, 0
	for _, r := range want {
		if r[1] == "image" && r[4] != "" {
			images++
		}
		if r[1] == "glyph" && r[2] != "" {
			glyphs++
		}
	}
	if images == 0 || glyphs == 0 {
		t.Fatalf("the seed needs an image icon and a glyph icon: %v", want)
	}
	loaded, err := load(t, export(t, d, store))
	if err != nil {
		t.Fatal(err)
	}
	got := scan(loaded.R)
	if len(got) != len(want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d: got %v\nwant %v", i, got[i], want[i])
		}
	}
}
