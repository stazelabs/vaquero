package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

var start = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

// newCore is a core on a fresh database with a fixed clock, and its owner.
func newCore(t *testing.T) (*core.Core, dbtest.DB, gen.Actor) {
	t.Helper()
	d := dbtest.New(t)
	c := core.New(d.W, func() time.Time { return start })
	owner, err := c.EnsureOwner(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return c, d, owner
}

func eventCount(t *testing.T, d dbtest.DB) int64 {
	t.Helper()
	n, err := d.RQ.CountEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// trackerTables are every table only core writes.
var trackerTables = []string{"actors", "projects", "statuses", "milestones", "tickets", "tags", "ticket_tags", "comments", "ticket_links", "events"}

// snapshot is the row count of every tracker table, plus a checksum of the
// mutable ticket and comment columns so an UPDATE that changes no row count
// still shows.
func snapshot(t *testing.T, d dbtest.DB) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, tbl := range trackerTables {
		var n int64
		if err := d.R.QueryRow(`SELECT count(*) FROM ` + tbl).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[tbl] = n
	}
	for name, q := range map[string]string{
		"tickets~":    `SELECT coalesce(sum(length(title || body || status_id || priority || coalesce(parent_id,'') || coalesce(milestone_id,'') || coalesce(assignee_id,'') || position)), 0) FROM tickets`,
		"comments~":   `SELECT coalesce(sum(length(body || coalesce(deleted_at,''))), 0) FROM comments`,
		"projects~":   `SELECT coalesce(sum(length(name || description || coalesce(archived_at,''))), 0) FROM projects`,
		"statuses~":   `SELECT coalesce(sum(length(name || position)), 0) FROM statuses`,
		"milestones~": `SELECT coalesce(sum(length(name || description || coalesce(due_on,'') || coalesce(closed_at,''))), 0) FROM milestones`,
	} {
		var n int64
		if err := d.R.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[name] = n
	}
	return out
}
