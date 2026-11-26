package db_test

import (
	"testing"

	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/idgen"
)

// 0012 adds part 2's whole schema (VAQ-136): the projects' repo settings,
// ticket_refs and decision_tickets.files, whose rules hold in the database.
func TestGitSchema(t *testing.T) {
	d := dbtest.New(t)
	actor, project, status := seed(t, d)

	var path, branch, remote string
	var minutes int
	if err := d.R.QueryRow(`SELECT repo_path, main_branch, remote, fetch_minutes FROM projects WHERE id = ?`, project).
		Scan(&path, &branch, &remote, &minutes); err != nil {
		t.Fatal(err)
	}
	if path != "" || branch != "main" || remote != "" || minutes != 0 {
		t.Fatalf("defaults %q %q %q %d", path, branch, remote, minutes)
	}
	fetch := func(n int) error {
		_, err := d.W.Exec(`UPDATE projects SET fetch_minutes = ? WHERE id = ?`, n, project)
		return err
	}
	for _, n := range []int{0, 5, 60, 1440} {
		if err := fetch(n); err != nil {
			t.Errorf("fetch_minutes %d: %v", n, err)
		}
	}
	for _, n := range []int{-1, 1, 4, 1441} {
		if fetch(n) == nil {
			t.Errorf("fetch_minutes %d taken", n)
		}
	}

	tk := idgen.New()
	if _, err := d.W.Exec(`INSERT INTO tickets (id, project_id, number, title, status_id, position, created_by, created_at, updated_at, status_changed_at)
		VALUES (?, ?, 1, 'T', ?, 1024, ?, ?, ?, ?)`, tk, project, status, actor, stamp, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	ref := func(ticket, kind, target, anchor string) error {
		_, err := d.W.Exec(`INSERT INTO ticket_refs (id, ticket_id, kind, target, anchor, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			idgen.New(), ticket, kind, target, anchor, actor, stamp)
		return err
	}
	for _, kind := range []string{"requirement", "design", "plan", "spec", "other"} {
		if err := ref(tk, kind, "docs/"+kind+".md", ""); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	if err := ref(tk, "spec", "docs/spec.md", "#part-2"); err != nil {
		t.Errorf("the same target with another anchor: %v", err)
	}
	if ref(tk, "design", "docs/spec.md", "") == nil {
		t.Error("the same target and anchor twice")
	}
	if ref(tk, "readme", "README.md", "") == nil {
		t.Error("an unknown kind")
	}
	if ref("nope", "other", "README.md", "") == nil {
		t.Error("a reference on no ticket")
	}

	dec := idgen.New()
	if _, err := d.W.Exec(`INSERT INTO decisions (id, project_id, kind, subject, question, asked_by, asked_at) VALUES (?, ?, 'approval', 'plan', 'Approve?', ?, ?)`,
		dec, project, actor, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`INSERT INTO decision_tickets (decision_id, ticket_id) VALUES (?, ?)`, dec, tk); err != nil {
		t.Fatal(err)
	}
	var files string
	if err := d.R.QueryRow(`SELECT files FROM decision_tickets WHERE decision_id = ?`, dec).Scan(&files); err != nil || files != "[]" {
		t.Fatalf("files %q %v", files, err)
	}
	set := func(v string) error {
		_, err := d.W.Exec(`UPDATE decision_tickets SET files = ? WHERE decision_id = ?`, v, dec)
		return err
	}
	if err := set(`[{"path":"docs/a.md","commit":"abc","blob":"def"}]`); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{`{}`, `not json`, `"x"`} {
		if set(bad) == nil {
			t.Errorf("files %s taken", bad)
		}
	}
}
