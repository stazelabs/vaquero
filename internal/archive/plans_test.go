package archive_test

import (
	"database/sql"
	"testing"
)

// TestExportImportKeepsPlans: the seeded plan and both its revisions
// survive an export and an import unchanged.
func TestExportImportKeepsPlans(t *testing.T) {
	d, store := seeded(t)
	const q = `SELECT p.id, p.ticket_id, p.revision, p.body, p.created_at, p.updated_at, p.updated_by,
		group_concat(r.revision || ':' || r.body || ':' || r.at || ':' || r.actor_id, '|')
		FROM plans p JOIN plan_revisions r ON r.plan_id = p.id GROUP BY p.id ORDER BY p.id`
	scan := func(db *sql.DB) [8]string {
		t.Helper()
		var out [8]string
		ptrs := make([]any, len(out))
		for i := range out {
			ptrs[i] = &out[i]
		}
		if err := db.QueryRow(q).Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		return out
	}
	want := scan(d.R)
	if want[2] != "2" {
		t.Fatalf("the seed has no plan with two revisions: %v", want)
	}
	loaded, err := load(t, export(t, d, store))
	if err != nil {
		t.Fatal(err)
	}
	if got := scan(loaded.R); got != want {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}
