package db_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
)

const stamp = "2026-11-18T12:00:00Z"

func TestTrackerTablesExist(t *testing.T) {
	d := dbtest.New(t)
	for _, tbl := range []string{"actors", "projects", "statuses", "milestones", "tickets", "tags", "ticket_tags", "comments", "ticket_links", "events"} {
		var n int
		if err := d.R.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&n); err != nil || n != 1 {
			t.Fatalf("table %s: %d %v", tbl, n, err)
		}
	}
}

func TestProjectKeyCheck(t *testing.T) {
	d := dbtest.New(t)
	for key, ok := range map[string]bool{"VAQ": true, "A1": true, "ABCDEFGHIJ": true, "A": false, "vaq": false, "1AB": false, "AB-C": false, "ABCDEFGHIJK": false} {
		_, err := d.W.Exec(`INSERT INTO projects (id, key, name, created_at, updated_at) VALUES (?, ?, 'P', ?, ?)`, idgen.New(), key, stamp, stamp)
		if (err == nil) != ok {
			t.Errorf("key %q: err=%v, want ok=%v", key, err, ok)
		}
	}
}

// seed makes an actor, a project with one status, and returns their ids.
func seed(t *testing.T, d dbtest.DB) (actorID, projectID, statusID string) {
	t.Helper()
	ctx := context.Background()
	a, err := d.Q.CreateActor(ctx, gen.CreateActorParams{ID: idgen.New(), Kind: "human", Name: "Owner", CreatedAt: stamp})
	if err != nil {
		t.Fatal(err)
	}
	p, err := d.Q.CreateProject(ctx, gen.CreateProjectParams{ID: idgen.New(), Key: "VAQ", Name: "Vaquero", CreatedAt: stamp, UpdatedAt: stamp})
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.Q.CreateStatus(ctx, gen.CreateStatusParams{ID: idgen.New(), ProjectID: p.ID, Name: "Todo", Category: "todo", Position: 1})
	if err != nil {
		t.Fatal(err)
	}
	return a.ID, p.ID, s.ID
}

func TestTicketNumberUniquePerProject(t *testing.T) {
	d := dbtest.New(t)
	ctx := context.Background()
	actor, project, status := seed(t, d)
	mk := func() error {
		_, err := d.Q.CreateTicket(ctx, gen.CreateTicketParams{
			ID: idgen.New(), ProjectID: project, Number: 1, Title: "t", StatusID: status,
			Position: 1024, CreatedBy: actor, CreatedAt: stamp, UpdatedAt: stamp,
		})
		return err
	}
	if err := mk(); err != nil {
		t.Fatal(err)
	}
	if err := mk(); err == nil {
		t.Fatal("duplicate (project, number) accepted")
	}
}

func TestNextTicketNumber(t *testing.T) {
	d := dbtest.New(t)
	ctx := context.Background()
	_, project, _ := seed(t, d)
	for want := int64(1); want <= 3; want++ {
		n, err := d.Q.NextTicketNumber(ctx, project)
		if err != nil || n != want {
			t.Fatalf("got %d %v, want %d", n, err, want)
		}
	}
}

func TestTagNamesCaseInsensitive(t *testing.T) {
	d := dbtest.New(t)
	ctx := context.Background()
	a, err := d.Q.UpsertTag(ctx, gen.UpsertTagParams{ID: idgen.New(), Name: "Bug", CreatedAt: stamp})
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.Q.UpsertTag(ctx, gen.UpsertTagParams{ID: idgen.New(), Name: "bug", CreatedAt: stamp})
	if err != nil || b.ID != a.ID || b.Name != "Bug" {
		t.Fatalf("%+v %+v %v", a, b, err)
	}
}

func TestEventPayloadMustBeJSON(t *testing.T) {
	d := dbtest.New(t)
	ctx := context.Background()
	actor, _, _ := seed(t, d)
	if _, err := d.Q.AppendEvent(ctx, gen.AppendEventParams{At: stamp, ActorID: actor, Kind: "x", Payload: "not json", Meta: "{}"}); err == nil {
		t.Fatal("non-JSON payload accepted")
	}
	if _, err := d.Q.AppendEvent(ctx, gen.AppendEventParams{At: stamp, ActorID: actor, Kind: "x", Payload: "{}", Meta: "not json"}); err == nil {
		t.Fatal("non-JSON meta accepted")
	}
	if _, err := d.Q.AppendEvent(ctx, gen.AppendEventParams{At: stamp, ActorID: actor, Kind: "x", Payload: "{}", Meta: "{}"}); err != nil {
		t.Fatal(err)
	}
}

func TestAncestorIDs(t *testing.T) {
	d := dbtest.New(t)
	ctx := context.Background()
	actor, project, status := seed(t, d)
	var ids []string
	parent := ""
	for i := int64(1); i <= 3; i++ {
		tk, err := d.Q.CreateTicket(ctx, gen.CreateTicketParams{
			ID: idgen.New(), ProjectID: project, Number: i, Title: "t", StatusID: status,
			ParentID: nullStr(parent), Position: 1024, CreatedBy: actor, CreatedAt: stamp, UpdatedAt: stamp,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, tk.ID)
		parent = tk.ID
	}
	got, err := d.Q.ListAncestorIDs(ctx, ids[2])
	if err != nil || len(got) != 2 || got[0] != ids[1] || got[1] != ids[0] {
		t.Fatalf("%v %v (want %v)", got, err, ids[:2])
	}
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
