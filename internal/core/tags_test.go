package core_test

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
)

func init() {
	ops = append(ops,
		coreOp{"RenameTag",
			func(c *core.Core, f fixture) error {
				return c.RenameTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, "defect", true)
			},
			func(c *core.Core, f fixture) error {
				return c.RenameTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, "  ", true)
			}},
		// Renaming onto another tag's name merges the two.
		coreOp{"RenameTagOntoAnother",
			func(c *core.Core, f fixture) error {
				return c.RenameTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, "UI", true)
			},
			func(c *core.Core, f fixture) error {
				return c.RenameTag(bg, f.Owner.ID, f.Other.ID, f.Bug.ID, "UI", true)
			}},
		coreOp{"MergeTag",
			func(c *core.Core, f fixture) error {
				return c.MergeTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, f.UI.ID)
			},
			func(c *core.Core, f fixture) error {
				return c.MergeTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, f.Bug.ID)
			}},
		coreOp{"DeleteTag",
			func(c *core.Core, f fixture) error { return c.DeleteTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID) },
			func(c *core.Core, f fixture) error { return c.DeleteTag(bg, f.Owner.ID, f.Project.ID, "no-such-tag") }},
	)
}

// projectTags is a project's tags with their ticket counts.
func projectTags(t *testing.T, d dbtest.DB, projectID string) map[string]int64 {
	t.Helper()
	rows, err := d.RQ.ListProjectTags(bg, projectID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int64{}
	for _, r := range rows {
		out[r.Name] = r.Tickets
	}
	return out
}

// lastEvent is the newest event's kind and payload.
func lastEvent(t *testing.T, d dbtest.DB) (string, map[string]any) {
	t.Helper()
	var kind, payload string
	if err := d.R.QueryRow(`SELECT kind, payload FROM events ORDER BY seq DESC LIMIT 1`).Scan(&kind, &payload); err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatal(err)
	}
	return kind, m
}

func wantTags(t *testing.T, d dbtest.DB, projectID string, want map[string]int64) {
	t.Helper()
	if got := projectTags(t, d, projectID); !maps.Equal(got, want) {
		t.Fatalf("tags %v, want %v", got, want)
	}
}

func TestProjectTagsCountTickets(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Third", Tags: []string{"bug", "ui"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateTicket(bg, f.Owner.ID, f.Other.ID, core.TicketInput{Title: "Other", Tags: []string{"bug"}}); err != nil {
		t.Fatal(err)
	}
	wantTags(t, d, f.Project.ID, map[string]int64{"bug": 2, "ui": 2})
	wantTags(t, d, f.Other.ID, map[string]int64{"bug": 1})
}

func TestRenameTag(t *testing.T) {
	c, d, f := newFixture(t)
	v := f.T1.Version
	if err := c.RenameTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, " defect ", true); err != nil {
		t.Fatal(err)
	}
	wantTags(t, d, f.Project.ID, map[string]int64{"defect": 1, "ui": 1})
	kind, p := lastEvent(t, d)
	if kind != core.EvTagRenamed || p["from"] != "bug" || p["to"] != "defect" || p["tickets"] != 1.0 {
		t.Fatalf("%s %v", kind, p)
	}
	// The ticket changed: its version rises, so a stale edit form is refused.
	tk, _ := d.RQ.GetTicket(bg, f.T1.ID)
	if tk.Version != v+1 {
		t.Fatalf("version %d, want %d", tk.Version, v+1)
	}
	// The same name again changes nothing and writes nothing.
	before := eventCount(t, d)
	tag, _ := d.RQ.ListTicketTags(bg, f.T1.ID)
	if err := c.RenameTag(bg, f.Owner.ID, f.Project.ID, tag[0].ID, "defect", true); err != nil {
		t.Fatal(err)
	}
	if eventCount(t, d) != before {
		t.Fatal("a no-op rename wrote an event")
	}
}

// A tag another project also uses is renamed in this project only.
func TestRenameSharedTagLeavesOtherProjects(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := c.CreateTicket(bg, f.Owner.ID, f.Other.ID, core.TicketInput{Title: "Other", Tags: []string{"bug"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.RenameTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, "defect", true); err != nil {
		t.Fatal(err)
	}
	wantTags(t, d, f.Project.ID, map[string]int64{"defect": 1, "ui": 1})
	wantTags(t, d, f.Other.ID, map[string]int64{"bug": 1})
}

// A change of case alone renames the tag in place.
func TestRenameTagCase(t *testing.T) {
	c, d, f := newFixture(t)
	if err := c.RenameTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, "Bug", true); err != nil {
		t.Fatal(err)
	}
	wantTags(t, d, f.Project.ID, map[string]int64{"Bug": 1, "ui": 1})
	if kind, _ := lastEvent(t, d); kind != core.EvTagRenamed {
		t.Fatal(kind)
	}
}

// Renaming onto an existing name (matched by SameTag) merges: every ticket
// of the project keeps one copy of the target, and the counts add up
// without double-counting a ticket that had both.
func TestRenameOntoExistingMerges(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Both", Tags: []string{"bug", "ui"}}); err != nil {
		t.Fatal(err)
	}
	wantTags(t, d, f.Project.ID, map[string]int64{"bug": 2, "ui": 2})
	before, _ := d.RQ.MaxEventSeq(bg)
	if err := c.RenameTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, "UI", true); err != nil {
		t.Fatal(err)
	}
	wantTags(t, d, f.Project.ID, map[string]int64{"ui": 3})
	if after, _ := d.RQ.MaxEventSeq(bg); after != before+1 {
		t.Fatalf("%d events", after-before)
	}
	kind, p := lastEvent(t, d)
	if kind != core.EvTagMerged || p["from"] != "bug" || p["to"] != "ui" || p["tickets"] != 2.0 {
		t.Fatalf("%s %v", kind, p)
	}
	// The merged tag is gone from the database once nothing uses it.
	var n int
	if err := d.R.QueryRow(`SELECT count(*) FROM tags WHERE id = ?`, f.Bug.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("bug left: %d %v", n, err)
	}
}

func TestMergeTagKeepsOtherProjects(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := c.CreateTicket(bg, f.Owner.ID, f.Other.ID, core.TicketInput{Title: "Other", Tags: []string{"bug"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.MergeTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, f.UI.ID); err != nil {
		t.Fatal(err)
	}
	wantTags(t, d, f.Project.ID, map[string]int64{"ui": 2})
	wantTags(t, d, f.Other.ID, map[string]int64{"bug": 1})
}

func TestDeleteTag(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := c.CreateTicket(bg, f.Owner.ID, f.Other.ID, core.TicketInput{Title: "Other", Tags: []string{"bug"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID); err != nil {
		t.Fatal(err)
	}
	wantTags(t, d, f.Project.ID, map[string]int64{"ui": 1})
	wantTags(t, d, f.Other.ID, map[string]int64{"bug": 1})
	kind, p := lastEvent(t, d)
	if kind != core.EvTagDeleted || p["name"] != "bug" || p["tickets"] != 1.0 {
		t.Fatalf("%s %v", kind, p)
	}
	if tt, _ := d.RQ.ListTicketTags(bg, f.T1.ID); len(tt) != 0 {
		t.Fatalf("T1 still tagged %v", tt)
	}
}

func TestTagOperationsRefuse(t *testing.T) {
	c, _, f := newFixture(t)
	fieldErr(t, c.RenameTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, "", true), "name", "Enter a name.")
	fieldErr(t, c.RenameTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, strings.Repeat("x", 41), true), "name", "Keep the name under 40 characters.")
	fieldErr(t, c.MergeTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, f.Bug.ID), "name", "Choose another tag.")
	// A tag the project doesn't use isn't the project's to change.
	if err := c.DeleteTag(bg, f.Owner.ID, f.Other.ID, f.Bug.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("other project's delete: %v", err)
	}
}

// Renaming onto a name only another project uses joins that tag, and reads
// as a rename here.
func TestRenameOntoOtherProjectsTag(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := c.CreateTicket(bg, f.Owner.ID, f.Other.ID, core.TicketInput{Title: "Other", Tags: []string{"Defect"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.RenameTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, "defect", true); err != nil {
		t.Fatal(err)
	}
	wantTags(t, d, f.Project.ID, map[string]int64{"Defect": 1, "ui": 1})
	wantTags(t, d, f.Other.ID, map[string]int64{"Defect": 1})
	if kind, p := lastEvent(t, d); kind != core.EvTagRenamed || p["to"] != "Defect" {
		t.Fatalf("%s %v", kind, p)
	}
}

// ticketIDs is a tag event payload's ticket_ids.
func ticketIDs(t *testing.T, p map[string]any) []string {
	t.Helper()
	raw, ok := p["ticket_ids"].([]any)
	if !ok {
		t.Fatalf("no ticket_ids in %v", p)
	}
	out := make([]string, len(raw))
	for i, v := range raw {
		out[i], _ = v.(string)
	}
	return out
}

func wantIDs(t *testing.T, p map[string]any, want ...string) {
	t.Helper()
	got := ticketIDs(t, p)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("ticket_ids %v, want %v", got, want)
	}
}

// Every tag event names the tickets whose tags changed, recorded before
// the rows move: the project's tickets only, a ticket with both tags once.
func TestTagEventsNameTheirTickets(t *testing.T) {
	setup := func(t *testing.T) (*core.Core, dbtest.DB, fixture, string) {
		c, d, f := newFixture(t)
		both, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Both", Tags: []string{"bug", "ui"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.CreateTicket(bg, f.Owner.ID, f.Other.ID, core.TicketInput{Title: "Other", Tags: []string{"bug"}}); err != nil {
			t.Fatal(err)
		}
		return c, d, f, both.ID
	}
	t.Run("renamed", func(t *testing.T) {
		c, d, f, both := setup(t)
		if err := c.RenameTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, "defect", true); err != nil {
			t.Fatal(err)
		}
		_, p := lastEvent(t, d)
		wantIDs(t, p, f.T1.ID, both)
	})
	t.Run("renamed in place", func(t *testing.T) {
		c, d, f, _ := setup(t)
		if err := c.RenameTag(bg, f.Owner.ID, f.Project.ID, f.UI.ID, "UI", true); err != nil {
			t.Fatal(err)
		}
		_, p := lastEvent(t, d)
		wantIDs(t, p, f.T2.ID, ticketWith(t, d, "Both"))
	})
	t.Run("merged", func(t *testing.T) {
		c, d, f, both := setup(t)
		if err := c.MergeTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, f.UI.ID); err != nil {
			t.Fatal(err)
		}
		kind, p := lastEvent(t, d)
		if kind != core.EvTagMerged || p["tickets"] != 2.0 {
			t.Fatalf("%s %v", kind, p)
		}
		wantIDs(t, p, f.T1.ID, both)
		wantTags(t, d, f.Project.ID, map[string]int64{"ui": 3})
	})
	t.Run("merged by rename", func(t *testing.T) {
		c, d, f, both := setup(t)
		if err := c.RenameTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, "UI", true); err != nil {
			t.Fatal(err)
		}
		_, p := lastEvent(t, d)
		wantIDs(t, p, f.T1.ID, both)
	})
	t.Run("deleted", func(t *testing.T) {
		c, d, f, both := setup(t)
		if err := c.DeleteTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID); err != nil {
			t.Fatal(err)
		}
		_, p := lastEvent(t, d)
		wantIDs(t, p, f.T1.ID, both)
	})
}

func ticketWith(t *testing.T, d dbtest.DB, title string) string {
	t.Helper()
	var id string
	if err := d.R.QueryRow(`SELECT id FROM tickets WHERE title = ?`, title).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// A rename that would merge, sent without merge, is refused inside the
// transaction (the handler's own check can be stale): nothing is written.
func TestRenameWithoutMergeRefusesAMerge(t *testing.T) {
	c, d, f := newFixture(t)
	snap := snapshot(t, d)
	err := c.RenameTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, "ui", false)
	mr, ok := errors.AsType[*core.MergeRefused](err)
	if !ok || mr.Into != "ui" || mr.Error() != "VAQ already has the tag ui. Merge the two, or choose another name." {
		t.Fatalf("%v", err)
	}
	if got := snapshot(t, d); !maps.Equal(got, snap) {
		t.Fatal("a refused merge wrote")
	}
	// A plain rename needs no merge.
	if err := c.RenameTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, "defect", false); err != nil {
		t.Fatal(err)
	}
}
