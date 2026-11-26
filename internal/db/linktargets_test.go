package db_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
)

// linkWorld is a schema-7 database (v0.1.x) holding link events without
// to_id, one per backfill case, before 0008 runs.
func linkWorld(t *testing.T) *sql.DB {
	t.Helper()
	w, err := appdb.Open(filepath.Join(t.TempDir(), "links.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	if err := appdb.MigrateTo(w, 7); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := w.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	ticket := func(id, project, status string, number int, at string) {
		t.Helper()
		exec(`INSERT INTO tickets (id, project_id, number, title, status_id, position, created_by, created_at, updated_at, status_changed_at)
			VALUES (?, ?, ?, 'T', ?, 1024, 'a', ?, ?, ?)`, id, project, number, status, at, at, at)
	}
	event := func(seq int, at, kind, project, ticket, payload string) {
		t.Helper()
		exec(`INSERT INTO events (seq, at, actor_id, kind, project_id, ticket_id, payload) VALUES (?, ?, 'a', ?, nullif(?, ''), nullif(?, ''), ?)`,
			seq, at, kind, project, ticket, payload)
	}
	exec(`INSERT INTO actors (id, kind, name, created_at) VALUES ('a', 'human', 'Owner', 't')`)
	for _, p := range [][2]string{{"p1", "OLD"}, {"p2", "VAQ"}, {"p3", "NEW"}} {
		exec(`INSERT INTO projects (id, key, name, created_at, updated_at) VALUES (?, ?, 'P', 't', 't')`, p[0], p[1])
		exec(`INSERT INTO statuses (id, project_id, name, category, position) VALUES (?, ?, 'Todo', 'todo', 1)`, "s"+p[0], p[0])
	}
	// p1 was VAQ when the links were made, and is OLD now; p2 took VAQ
	// later. p3 was BBB then and took NEW from nobody, after the events.
	ticket("t1", "p1", "sp1", 1, "2026-01-01T00:00:00.000Z") // VAQ-1 then, OLD-1 now
	ticket("t2", "p1", "sp1", 2, "2026-01-01T00:00:00.000Z") // VAQ-2 then, OLD-2 now
	ticket("t3", "p1", "sp1", 3, "2026-01-01T00:00:00.000Z") // VAQ-3 then, OLD-3 now
	ticket("u1", "p2", "sp2", 1, "2026-03-01T00:00:00.000Z") // VAQ-1 now, made after the events
	ticket("k1", "p3", "sp3", 1, "2026-01-01T00:00:00.000Z") // BBB-1 then, NEW-1 now
	ticket("m1", "p3", "sp3", 2, "2026-01-01T00:00:00.000Z") // BBB-2 then, NEW-2 now
	ticket("x1", "p2", "sp2", 2, "2026-01-01T00:00:00.000Z") // VAQ-2 now; p2's own, made early
	exec(`INSERT INTO ticket_links (id, from_id, to_id, kind, created_at) VALUES ('l1', 't2', 't1', 'blocks', 't')`)
	exec(`INSERT INTO projects (id, key, name, created_at, updated_at) VALUES ('p4', 'DEV', 'P', 't', 't')`)
	exec(`INSERT INTO statuses (id, project_id, name, category, position) VALUES ('sp4', 'p4', 'Todo', 'todo', 1)`)
	ticket("d1", "p4", "sp4", 1, "2026-01-01T00:00:00.000Z") // DEV-1, never renamed
	ticket("d2", "p4", "sp4", 2, "2026-01-01T00:00:00.000Z")

	// REU was deleted and made again after the events (no rename), so its
	// REU-1 is newer than them.
	exec(`INSERT INTO projects (id, key, name, created_at, updated_at) VALUES ('p5', 'REU', 'P', 't', 't')`)
	exec(`INSERT INTO statuses (id, project_id, name, category, position) VALUES ('sp5', 'p5', 'Todo', 'todo', 1)`)
	ticket("r1", "p5", "sp5", 1, "2026-03-01T00:00:00.000Z")

	const then = "2026-02-01T00:00:00.000Z"
	// 1: the link row still exists, though its key now names another ticket.
	event(1, then, "link.created", "p1", "t2", `{"kind":"blocks","link_id":"l1","to":"VAQ-1"}`)
	// 2: the link is gone and its key still names the same ticket.
	event(2, then, "link.created", "p4", "d2", `{"kind":"relates","link_id":"gone1","to":"DEV-1"}`)
	event(3, then, "link.deleted", "p4", "d2", `{"kind":"relates","link_id":"gone1","to":"DEV-1"}`)
	// 4: the key now names a ticket made after the event.
	event(4, then, "link.deleted", "p1", "t3", `{"kind":"relates","link_id":"gone2","to":"VAQ-1"}`)
	// 5: the key is now another project's, renamed to it after the event;
	// that project's ticket is older than the event (x1 is VAQ-2 through p2,
	// which took VAQ by a rename below).
	event(5, then, "link.deleted", "p1", "t3", `{"kind":"relates","link_id":"gone3","to":"VAQ-2"}`)
	event(6, "2026-02-02T00:00:00.000Z", "project.updated", "p1", "", `{"changes":{"key":{"from":"VAQ","to":"OLD"}}}`)
	event(7, "2026-02-03T00:00:00.000Z", "project.updated", "p2", "", `{"changes":{"key":{"from":"ZZZ","to":"VAQ"}}}`)
	// 8: renamed project, the event made after the rename, with its new key.
	event(8, "2026-02-04T00:00:00.000Z", "link.deleted", "p1", "t3", `{"kind":"relates","link_id":"gone4","to":"OLD-1"}`)
	// 9: already has a to_id; it is never changed.
	event(9, then, "link.deleted", "p1", "t3", `{"kind":"relates","link_id":"gone5","to":"VAQ-1","to_id":"t1"}`)
	// 10: a key that names no ticket.
	event(10, then, "link.deleted", "p1", "t3", `{"kind":"relates","link_id":"gone6","to":"NOPE-9"}`)
	// 11: not a link event.
	event(11, then, "ticket.created", "p1", "t3", `{"title":"T"}`)
	// 12: the key's project was made again later; its ticket is newer.
	event(12, then, "link.deleted", "p1", "t3", `{"kind":"relates","link_id":"gone7","to":"REU-1"}`)
	return w
}

func toIDs(t *testing.T, w *sql.DB) map[int64]string {
	t.Helper()
	rows, err := w.Query(`SELECT seq, coalesce(json_extract(payload, '$.to_id'), '') FROM events ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var seq int64
		var to string
		if err := rows.Scan(&seq, &to); err != nil {
			t.Fatal(err)
		}
		out[seq] = to
	}
	return out
}

var wantToIDs = map[int64]string{1: "t1", 2: "d1", 3: "d1", 4: "", 5: "", 6: "", 7: "", 8: "t1", 9: "t1", 10: "", 11: "", 12: ""}

// Review Focus 2: the backfill never names the wrong ticket.
func TestMigrationBackfillsLinkTargets(t *testing.T) {
	w := linkWorld(t)
	var before string
	if err := w.QueryRow(`SELECT payload FROM events WHERE seq = 11`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := appdb.Migrate(w); err != nil {
		t.Fatal(err)
	}
	got := toIDs(t, w)
	for seq, want := range wantToIDs {
		if got[seq] != want {
			t.Errorf("event %d: to_id %q, want %q", seq, got[seq], want)
		}
	}
	var after, one string
	if err := w.QueryRow(`SELECT payload FROM events WHERE seq = 11`).Scan(&after); err != nil || after != before {
		t.Errorf("a non-link event changed: %q → %q (%v)", before, after, err)
	}
	if err := w.QueryRow(`SELECT payload FROM events WHERE seq = 1`).Scan(&one); err != nil ||
		one != `{"kind":"blocks","link_id":"l1","to":"VAQ-1","to_id":"t1"}` {
		t.Errorf("event 1 payload %q (%v)", one, err)
	}
}

// The same backfill runs again after an import, and finds the same.
func TestBackfillLinkTargetsMatchesTheMigration(t *testing.T) {
	w := linkWorld(t)
	tx, err := w.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := appdb.BackfillLinkTargets(t.Context(), tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got := toIDs(t, w)
	for seq, want := range wantToIDs {
		if got[seq] != want {
			t.Errorf("event %d: to_id %q, want %q", seq, got[seq], want)
		}
	}
	// A second run changes nothing.
	tx, _ = w.Begin()
	if err := appdb.BackfillLinkTargets(t.Context(), tx); err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	if again := toIDs(t, w); len(again) != len(got) {
		t.Fatal("rows changed")
	}
}

// ticketEventsQuery is ListTicketEvents as written in queries/events.sql,
// with its parameter as ?.
func ticketEventsQuery(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("queries/events.sql")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(raw), "-- name: ListTicketEvents :many\n")
	if !ok {
		t.Fatal("no ListTicketEvents")
	}
	q, _, _ := strings.Cut(rest, "-- name:")
	return strings.ReplaceAll(strings.TrimSpace(q), "sqlc.arg(ticket_id)", "?1")
}

var scansEvents = regexp.MustCompile(`\bSCAN (e|o|l|g|v|events)\b`)

// Review Focus 2: a ticket's history is four index lookups (its own
// events, link events aimed at it, its projects' tag events, its
// ancestors' moves that took it along), never a scan of events, on a new
// database and on every released one migrated.
func TestTicketHistoryUsesIndexes(t *testing.T) {
	dbs := map[string]*sql.DB{"new": dbtest.New(t).W}
	files, _ := filepath.Glob(filepath.Join(fixtureDir, "*.sqlite"))
	for _, f := range files {
		path := filepath.Join(t.TempDir(), "vaquero.db")
		copyFixture(t, f, path)
		d, err := appdb.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		if err := appdb.Migrate(d); err != nil {
			t.Fatal(err)
		}
		dbs[filepath.Base(f)] = d
	}
	q := ticketEventsQuery(t)
	for name, d := range dbs {
		rows, err := d.Query("EXPLAIN QUERY PLAN "+q, "01M8A0K2WGG8CDMJYX5VMYTT70")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		all := strings.Join(plan, "\n")
		t.Logf("%s:\n%s", name, all)
		if scansEvents.MatchString(all) {
			t.Errorf("%s: the history query scans events:\n%s", name, all)
		}
		if !strings.Contains(all, "SEARCH v USING INDEX events_ticket") {
			t.Errorf("%s: the ancestors' moves aren't found through events_ticket:\n%s", name, all)
		}
		for _, idx := range []string{"events_ticket", "events_link_to", "events_tag"} {
			if !strings.Contains(all, idx) {
				t.Errorf("%s: the plan doesn't use %s:\n%s", name, idx, all)
			}
		}
	}
}
