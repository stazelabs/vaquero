package core_test

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
)

// newestPayload is the payload of the ticket's newest event.
func newestPayload(t *testing.T, d dbtest.DB, ticketID string) map[string]any {
	t.Helper()
	evs, err := d.RQ.ListTicketEvents(bg, sql.NullString{String: ticketID, Valid: true})
	if err != nil || len(evs) == 0 {
		t.Fatalf("%d %v", len(evs), err)
	}
	m := map[string]any{}
	if err := json.Unmarshal([]byte(evs[0].Payload), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// VAQ-21: a description edit keeps the text before and after.
func TestBodyEditStoresBeforeAndAfter(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{Body: new("one\ntwo")}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{Body: new("one\nthree")}, 0); err != nil {
		t.Fatal(err)
	}
	p := newestPayload(t, d, f.T1.ID)
	if p["body_before"] != "one\ntwo" || p["body_after"] != "one\nthree" || p["body_too_long"] != nil {
		t.Fatalf("%v", p)
	}
	// A change that leaves the body alone carries neither.
	if _, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{Title: new("Renamed")}, 0); err != nil {
		t.Fatal(err)
	}
	if p := newestPayload(t, d, f.T1.ID); p["body_before"] != nil || p["body_after"] != nil {
		t.Fatalf("%v", p)
	}
}

// VAQ-21: past 64 KiB on either side, neither text is kept, only the mark.
func TestBodyEditTooLongKeepsNoText(t *testing.T) {
	c, d, f := newFixture(t)
	long := strings.Repeat("x", core.BodyHistoryMax+1)
	before := eventCount(t, d)
	if _, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{Body: &long}, 0); err != nil {
		t.Fatal(err)
	}
	if eventCount(t, d) != before+1 {
		t.Fatal("want one event")
	}
	p := newestPayload(t, d, f.T1.ID)
	if p["body_too_long"] != true || p["body_before"] != nil || p["body_after"] != nil {
		t.Fatalf("%v", p)
	}
	if _, ok := p["changes"].(map[string]any)["body"]; !ok {
		t.Fatalf("%v", p)
	}
	// Exactly at the limit is kept.
	edge := strings.Repeat("y", core.BodyHistoryMax)
	c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{Body: new("short")}, 0)
	c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{Body: &edge}, 0)
	if p := newestPayload(t, d, f.T1.ID); p["body_after"] != edge || p["body_before"] != "short" {
		t.Fatal("edge case not kept")
	}
}
