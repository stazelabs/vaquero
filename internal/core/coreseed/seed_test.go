package coreseed_test

import (
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/core/coreseed"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
)

// The seed carries every event payload shape this release writes, so the
// upgrade fixture made from it does too: a description edit with its
// before and after text, a tag rename naming its tickets, a reorder, and
// the owner's profile (a display name and an avatar, VAQ-149).
func TestSeedCarriesThisReleasesPayloads(t *testing.T) {
	d := dbtest.New(t)
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	c := core.New(d.W, func() time.Time { return now })
	c.BlobStore = "sqlite"
	if err := coreseed.Seed(t.Context(), c, d.Q, blobs.NewSQLite(d.W, d.R, func() time.Time { return now }), "01jx00000000000000000seed00", now); err != nil {
		t.Fatal(err)
	}
	for what, query := range map[string]string{
		"a description edit": `SELECT count(*) FROM events WHERE kind = 'ticket.updated'
			AND json_extract(payload, '$.body_before') IS NOT NULL AND json_extract(payload, '$.body_after') IS NOT NULL`,
		"a tag rename naming its tickets": `SELECT count(*) FROM events WHERE kind = 'tag.renamed'
			AND json_array_length(json_extract(payload, '$.ticket_ids')) = 1`,
		"a reorder": `SELECT count(*) FROM events WHERE kind = 'ticket.reordered'`,
		"a display name": `SELECT count(*) FROM events WHERE kind = 'actor.updated'
			AND json_extract(payload, '$.changes.display_name') IS NOT NULL`,
		"an avatar": `SELECT count(*) FROM events WHERE kind = 'actor.updated'
			AND json_extract(payload, '$.changes.avatar') IS NOT NULL`,
	} {
		var n int
		if err := d.R.QueryRowContext(t.Context(), query).Scan(&n); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if n != 1 {
			t.Errorf("%s: %d events, want 1", what, n)
		}
	}
}
