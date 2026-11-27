package db_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	appdb "github.com/stazelabs/vaquero/internal/db"
)

// movedWorld is a schema-13 database (v0.1.7) where an epic E was approved
// covering a child C and a grandchild G, and ticket M was approved on its
// own, all asked at 10:00. Later M and C were moved away and back (their
// ticket_old_keys moved_at at 11:00), as v0.1.7 allowed without clearing
// approvals. U was approved at 12:00, after its own move at 11:00, and K
// was approved with no move at all.
func movedWorld(t *testing.T) *sql.DB {
	t.Helper()
	w, err := appdb.Open(filepath.Join(t.TempDir(), "moved.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	if err := appdb.MigrateTo(w, 13); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := w.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO actors (id, kind, name, created_at) VALUES ('a', 'human', 'Owner', 't')`)
	exec(`INSERT INTO projects (id, key, name, created_at, updated_at, workflow) VALUES ('p', 'VAQ', 'P', 't', 't', 1)`)
	exec(`INSERT INTO projects (id, key, name, created_at, updated_at) VALUES ('o', 'OTH', 'P', 't', 't')`)
	exec(`INSERT INTO statuses (id, project_id, name, category, position) VALUES ('s', 'p', 'Todo', 'todo', 1)`)
	for i, id := range []string{"E", "C", "G", "M", "U", "K"} {
		exec(`INSERT INTO tickets (id, project_id, number, title, status_id, position, created_by, created_at, updated_at, status_changed_at)
			VALUES (?, 'p', ?, 'T', 's', 1024, 'a', 't', 't', 't')`, id, 10+i)
	}
	decision := func(id, at string) {
		exec(`INSERT INTO decisions (id, project_id, kind, subject, question, options, state, answer, answered_by, answered_at, asked_by, asked_at)
			VALUES (?, 'p', 'approval', 'plan', 'Approve?', '[{"label":"Approve"}]', 'answered', 'Approve', 'a', ?, 'a', ?)`, id, at, at)
	}
	approval := func(id, ticket, dec, pins, at string) {
		exec(`INSERT INTO approvals (id, ticket_id, decision_id, requirement_hash, plan_revision, pins, at) VALUES (?, ?, ?, 'h', 1, ?, ?)`,
			id, ticket, dec, pins, at)
	}
	at10, at11, at12 := "2026-11-01T10:00:00.000Z", "2026-11-01T11:00:00.000Z", "2026-11-01T12:00:00.000Z"
	decision("dE", at10)
	decision("dM", at10)
	decision("dU", at12)
	decision("dK", at10)
	approval("aE", "E", "dE", `{"children":[{"id":"C","parent":"E","requirement_hash":"hc"},{"id":"G","parent":"C","requirement_hash":"hg"}],"files":[],"parent":""}`, at10)
	approval("aM", "M", "dM", `{"children":[],"files":[],"parent":""}`, at10)
	approval("aU", "U", "dU", `{"children":[],"files":[],"parent":""}`, at12)
	approval("aK", "K", "dK", `{"children":[{"id":"G","parent":"C","requirement_hash":"hg"}],"parent":""}`, at10)
	// Moved away (to OTH-1, OTH-2, OTH-3) and back (the VAQ numbers they
	// left behind are their old keys too).
	exec(`INSERT INTO ticket_old_keys (project_id, number, ticket_id, moved_at) VALUES ('p', 1, 'M', ?), ('o', 1, 'M', ?)`, at10, at11)
	exec(`INSERT INTO ticket_old_keys (project_id, number, ticket_id, moved_at) VALUES ('o', 2, 'C', ?)`, at11)
	exec(`INSERT INTO ticket_old_keys (project_id, number, ticket_id, moved_at) VALUES ('o', 3, 'U', ?)`, at11)
	return w
}

func approvalRows(t *testing.T, d *sql.DB) map[string]string {
	t.Helper()
	rows, err := d.Query(`SELECT id, ticket_id || ' ' || decision_id || ' ' || requirement_hash || ' ' || plan_revision || ' ' || pins || ' ' || at FROM approvals`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, row string
		if err := rows.Scan(&id, &row); err != nil {
			t.Fatal(err)
		}
		out[id] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Ruling R-Q13: migration 0014 applies the move rule to approvals written
// before it: an approval asked before its ticket's latest move is gone, a
// covered descendant moved since is no longer covered, and nothing else
// changes.
func TestMigrationClearsApprovalsOfMovedTickets(t *testing.T) {
	d := movedWorld(t)
	before := approvalRows(t, d)
	if err := appdb.Migrate(d); err != nil {
		t.Fatal(err)
	}
	after := approvalRows(t, d)
	if _, ok := after["aM"]; ok {
		t.Error("M's approval, asked before its move, is still there")
	}
	for _, id := range []string{"aU", "aK"} { // asked after the move, or nothing moved
		if after[id] != before[id] {
			t.Errorf("%s changed:\n%s\n%s", id, before[id], after[id])
		}
	}
	want := `E dE h 1 {"children":[{"id":"G","parent":"C","requirement_hash":"hg"}],"files":[],"parent":""} ` + "2026-11-01T10:00:00.000Z"
	if after["aE"] != want {
		t.Errorf("the epic's approval:\n%s\nwant\n%s", after["aE"], want)
	}
	if len(after) != 3 {
		t.Errorf("%d approvals left, want 3", len(after))
	}

	// Run again, as an import does, it changes nothing.
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := appdb.ClearMovedApprovals(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	again := approvalRows(t, d)
	for id, row := range after {
		if again[id] != row {
			t.Errorf("a second run changed %s", id)
		}
	}
}
