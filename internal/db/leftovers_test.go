package db_test

import (
	"testing"

	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/idgen"
)

// 0011 adds the run's whole schema: ticket_old_keys (VAQ-18) and
// project_links (VAQ-119), whose rules hold in the database too.
func TestLeftoversSchema(t *testing.T) {
	d := dbtest.New(t)
	actor, p1, _ := seed(t, d)
	p2 := idgen.New()
	if _, err := d.W.Exec(`INSERT INTO projects (id, key, name, created_at, updated_at) VALUES (?, 'OPS', 'Ops', ?, ?)`, p2, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	link := func(from, to, kind string) error {
		_, err := d.W.Exec(`INSERT INTO project_links (id, from_project, to_project, kind, created_at, created_by) VALUES (?, ?, ?, ?, ?, ?)`,
			idgen.New(), from, to, kind, stamp, actor)
		return err
	}
	for _, kind := range []string{"depends on", "relates"} {
		if err := link(p1, p2, kind); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	if link(p1, p2, "relates") == nil {
		t.Error("the same pair and kind twice")
	}
	if link(p1, p2, "blocks") == nil {
		t.Error("an unknown kind")
	}
	if link(p1, p1, "relates") == nil {
		t.Error("a project linked to itself")
	}
	if link(p1, "nope", "relates") == nil {
		t.Error("a link to no project")
	}

	tk := idgen.New()
	if _, err := d.W.Exec(`INSERT INTO tickets (id, project_id, number, title, status_id, position, created_by, created_at, updated_at, status_changed_at)
		SELECT ?, ?, 1, 'T', id, 1024, ?, ?, ?, ? FROM statuses WHERE project_id = ?`, tk, p2, actor, stamp, stamp, stamp, p1); err != nil {
		t.Fatal(err)
	}
	old := func(project string, number int, ticket string) error {
		_, err := d.W.Exec(`INSERT INTO ticket_old_keys (project_id, number, ticket_id) VALUES (?, ?, ?)`, project, number, ticket)
		return err
	}
	if err := old(p1, 7, tk); err != nil {
		t.Fatal(err)
	}
	if old(p1, 7, tk) == nil {
		t.Error("one old key twice")
	}
	if old(p1, 8, "nope") == nil {
		t.Error("an old key for no ticket")
	}
	var got string
	if err := d.R.QueryRow(`SELECT ticket_id FROM ticket_old_keys WHERE project_id = ? AND number = 7`, p1).Scan(&got); err != nil || got != tk {
		t.Fatalf("%v %q", err, got)
	}
}
