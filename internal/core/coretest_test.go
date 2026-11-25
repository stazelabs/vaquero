package core_test

import (
	"context"
	"fmt"
	"hash/fnv"
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
var trackerTables = []string{"api_tokens", "actors", "projects", "statuses", "milestones", "tickets", "tags", "ticket_tags", "comments", "ticket_links", "attachments", "events"}

// snapshot is the row count and a content hash of every tracker table, so
// any insert, update or delete shows, whatever it does to lengths.
func snapshot(t *testing.T, d dbtest.DB) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, tbl := range trackerTables {
		rows, err := d.R.Query(`SELECT * FROM ` + tbl + ` ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		h := fnv.New64a()
		var n int64
		for rows.Next() {
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(h, "%v\x1f", vals)
			n++
		}
		rows.Close()
		out[tbl] = n
		out[tbl+"#"] = int64(h.Sum64())
	}
	return out
}

type timeT = time.Time

const time3h = 3 * time.Hour

func itoa(i int) string { return fmt.Sprint(i) }
