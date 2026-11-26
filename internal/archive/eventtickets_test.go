package archive_test

import (
	"database/sql"
	"fmt"
	"maps"
	"testing"

	"github.com/stazelabs/vaquero/internal/db/gen"
)

// TestImportRebuildsEventTickets: event_tickets isn't exported; an import
// rebuilds it from the events' payloads, so every ticket's history is the
// same after an export and an import as before.
func TestImportRebuildsEventTickets(t *testing.T) {
	d, store := seeded(t)
	histories := func(db *sql.DB) map[string]string {
		t.Helper()
		ids, err := db.Query(`SELECT id FROM tickets ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		var tickets []string
		for ids.Next() {
			var id string
			if err := ids.Scan(&id); err != nil {
				t.Fatal(err)
			}
			tickets = append(tickets, id)
		}
		ids.Close()
		out := map[string]string{}
		for _, id := range tickets {
			evs, err := gen.New(db).ListTicketEvents(bg, sql.NullString{String: id, Valid: true})
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range evs {
				out[id] += fmt.Sprintf("%d %s;", e.Seq, e.Kind)
			}
		}
		return out
	}
	var n int
	if err := d.R.QueryRow(`SELECT count(*) FROM event_tickets`).Scan(&n); err != nil || n == 0 {
		t.Fatalf("the seed needs events naming tickets in ticket_ids: %d %v", n, err)
	}
	want := histories(d.R)
	loaded, err := load(t, export(t, d, store))
	if err != nil {
		t.Fatal(err)
	}
	var m int
	if err := loaded.R.QueryRow(`SELECT count(*) FROM event_tickets`).Scan(&m); err != nil || m != n {
		t.Fatalf("%d event_tickets rows after the import, want %d (%v)", m, n, err)
	}
	if got := histories(loaded.R); !maps.Equal(got, want) {
		t.Fatalf("histories after the import:\n%v\nwant\n%v", got, want)
	}
}
