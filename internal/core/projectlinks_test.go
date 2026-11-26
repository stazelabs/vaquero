package core_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

func init() {
	ops = append(ops,
		coreOp{"AddProjectLink",
			func(c *core.Core, f fixture) error {
				_, err := c.AddProjectLink(bg, f.Owner.ID, f.Project.ID, "depends on", "OTH")
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.AddProjectLink(bg, f.Owner.ID, f.Project.ID, "depends on", "VAQ")
				return err
			}},
		coreOp{"RemoveProjectLink",
			func(c *core.Core, f fixture) error {
				return c.RemoveProjectLink(bg, f.Owner.ID, f.Project.ID, f.ProjectLink.ID)
			},
			func(c *core.Core, f fixture) error { return c.RemoveProjectLink(bg, f.Owner.ID, f.Project.ID, "nope") }},
	)
}

// projectFor makes a project with key k.
func projectFor(t *testing.T, c *core.Core, f fixture, k string) string {
	t.Helper()
	p, err := c.CreateProject(bg, f.Owner.ID, core.ProjectInput{Key: k, Name: k + " project"})
	if err != nil {
		t.Fatal(err)
	}
	return p.ID
}

// payloadOf decodes an event payload.
func payloadOf(t *testing.T, raw string) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// fieldMsg is err's message for field, or "" when err is no *Invalid.
func fieldMsg(err error, field string) string {
	inv, ok := errors.AsType[*core.Invalid](err)
	if !ok {
		return ""
	}
	return inv.Fields[field]
}

// VAQ-119: a project depends on or relates to another, by key in any case;
// the link is one event on the project it starts from.
func TestAddProjectLink(t *testing.T) {
	c, d, f := newFixture(t)
	l, err := c.AddProjectLink(bg, f.Owner.ID, f.Project.ID, "depends-on", "oth")
	if err != nil {
		t.Fatal(err)
	}
	if l.Kind != "depends on" || l.FromProject != f.Project.ID || l.ToProject != f.Other.ID || l.CreatedBy != f.Owner.ID {
		t.Fatalf("%+v", l)
	}
	evs, err := d.RQ.ListEventsNewest(bg, 1)
	if err != nil {
		t.Fatal(err)
	}
	e := evs[0]
	p := payloadOf(t, e.Payload)
	if e.Kind != core.EvProjectLinked || e.ProjectID.String != f.Project.ID || e.TicketID.Valid ||
		p["link_id"] != l.ID || p["kind"] != "depends on" || p["to"] != "OTH" || p["to_id"] != f.Other.ID {
		t.Fatalf("%+v %v", e, p)
	}
}

func TestAddProjectLinkRefusals(t *testing.T) {
	c, _, f := newFixture(t)
	thr := projectFor(t, c, f, "THR")
	fou := projectFor(t, c, f, "FOU")
	// The fixture: VAQ relates to OTH. Here THR depends on FOU depends on VAQ.
	if _, err := c.AddProjectLink(bg, f.Owner.ID, thr, "depends on", "FOU"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddProjectLink(bg, f.Owner.ID, fou, "depends on", "VAQ"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, from, kind, to, field, want string
	}{
		{"kind", f.Project.ID, "blocks", "OTH", "kind", "Choose depends on or relates."},
		{"unknown", f.Project.ID, "relates", "NOPE", "project", "No project NOPE."},
		{"empty", f.Project.ID, "relates", " ", "project", "Choose a project."},
		{"self", f.Project.ID, "relates", "vaq", "project", "A project can't link to itself."},
		{"same", f.Project.ID, "relates", "OTH", "project", "VAQ and OTH are already related."},
		{"reverse relates", f.Other.ID, "relates", "VAQ", "project", "OTH and VAQ are already related."},
		{"same depends", fou, "depends on", "VAQ", "project", "FOU already depends on VAQ."},
		{"direct loop", f.Project.ID, "depends on", "FOU", "project", "That would make VAQ depend on itself: FOU already depends on VAQ."},
		{"chain loop", f.Project.ID, "depends on", "THR", "project", "That would make VAQ depend on itself: THR depends on VAQ through FOU."},
	} {
		_, err := c.AddProjectLink(bg, f.Owner.ID, tc.from, tc.kind, tc.to)
		if got := fieldMsg(err, tc.field); got != tc.want {
			t.Errorf("%s: %q (%v), want %q", tc.name, got, err, tc.want)
		}
	}
	if _, err := c.AddProjectLink(bg, f.Owner.ID, "no-such-project", "relates", "OTH"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("missing from project: %v", err)
	}
	// Both kinds between the same two projects, and a link to an archived
	// project, are allowed: links only describe.
	if err := c.SetProjectArchived(bg, f.Owner.ID, thr, true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddProjectLink(bg, f.Owner.ID, f.Project.ID, "depends on", "OTH"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddProjectLink(bg, f.Owner.ID, f.Other.ID, "relates", "THR"); err != nil {
		t.Fatal(err)
	}
}

// A link is removed from either of its projects, never through another.
func TestRemoveProjectLink(t *testing.T) {
	c, d, f := newFixture(t)
	thr := projectFor(t, c, f, "THR")
	if err := c.RemoveProjectLink(bg, f.Owner.ID, thr, f.ProjectLink.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("through another project: %v", err)
	}
	if err := c.RemoveProjectLink(bg, f.Owner.ID, f.Other.ID, f.ProjectLink.ID); err != nil {
		t.Fatal(err)
	}
	evs, err := d.RQ.ListEventsNewest(bg, 1)
	if err != nil {
		t.Fatal(err)
	}
	e := evs[0]
	p := payloadOf(t, e.Payload)
	if e.Kind != core.EvProjectUnlinked || e.ProjectID.String != f.Project.ID || p["link_id"] != f.ProjectLink.ID || p["kind"] != "relates" || p["to"] != "OTH" || p["to_id"] != f.Other.ID {
		t.Fatalf("%+v %v", e, p)
	}
	if n := count(t, d, `SELECT count(*) FROM project_links`); n != 0 {
		t.Fatalf("%d links left", n)
	}
	if err := c.RemoveProjectLink(bg, f.Owner.ID, f.Project.ID, f.ProjectLink.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("again: %v", err)
	}
}
