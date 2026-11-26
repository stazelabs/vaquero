package handlers

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// countingDB counts the tag queries: the sqlc queries named List…Tags….
type countingDB struct {
	gen.DBTX
	tagReads int
	n        int // every query
}

func (c *countingDB) note(q string) {
	c.n++
	name, _, _ := strings.Cut(strings.TrimPrefix(q, "-- name: "), " ")
	if strings.HasPrefix(name, "List") && strings.Contains(name, "Tags") {
		c.tagReads++
	}
}

func (c *countingDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	c.note(q)
	return c.DBTX.QueryContext(ctx, q, args...)
}

func (c *countingDB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	c.note(q)
	return c.DBTX.QueryRowContext(ctx, q, args...)
}

// VAQ-122: a ticket's answer reads its sub-tickets' tags in one query,
// however many sub-tickets it has, and each sub-ticket keeps its own tags.
func TestTicketDTOReadsSubTicketTagsOnce(t *testing.T) {
	db := dbtest.New(t)
	now := func() time.Time { return time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC) }
	c := core.New(db.W, now)
	ctx := context.Background()
	owner, err := c.EnsureOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.CreateProject(ctx, owner.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := c.CreateTicket(ctx, owner.ID, p.ID, core.TicketInput{Title: "Parent", Tags: []string{"top"}})
	if err != nil {
		t.Fatal(err)
	}
	wantTags := [][]string{{"a", "b"}, {}, {"c"}, {"a"}}
	for i, tags := range wantTags {
		if _, err := c.CreateTicket(ctx, owner.ID, p.ID, core.TicketInput{Title: "Sub " + string(rune('A'+i)), ParentID: parent.ID, Tags: tags}); err != nil {
			t.Fatal(err)
		}
	}
	cdb := &countingDB{DBTX: db.R}
	d := Deps{R: gen.New(cdb), Logger: slog.New(slog.DiscardHandler), Now: now}
	out, err := ticketDTO(httptest.NewRequest("GET", "/", nil), d, parent, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.SubTickets) != len(wantTags) {
		t.Fatalf("%+v", out.SubTickets)
	}
	for i, s := range out.SubTickets {
		if s.Tags == nil || !slices.Equal(s.Tags, wantTags[i]) {
			t.Errorf("%s tags %q, want %q", s.Key, s.Tags, wantTags[i])
		}
	}
	if cdb.tagReads != 2 {
		t.Fatalf("%d tag queries, want 2 (the ticket's, then its sub-tickets')", cdb.tagReads)
	}
}
