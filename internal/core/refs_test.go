package core_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
)

func init() {
	ops = append(ops,
		coreOp{"AddRef",
			func(c *core.Core, f fixture) error {
				_, err := c.AddRef(bg, f.Owner.ID, f.T1.ID, core.RefInput{Kind: "spec", Target: "https://example.com/spec"})
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.AddRef(bg, f.Owner.ID, f.T1.ID, core.RefInput{Kind: "spec", Target: "javascript:alert(1)"})
				return err
			}},
		coreOp{"RemoveRef",
			func(c *core.Core, f fixture) error {
				// Arranged without an event, so the operation's is the only one.
				if _, err := c.DB.Exec(`INSERT INTO ticket_refs (id, ticket_id, kind, target, created_by, created_at) VALUES ('ref1', ?, 'spec', 'https://example.com/spec', ?, '2026-11-20T12:00:00Z')`, f.T1.ID, f.Owner.ID); err != nil {
					return err
				}
				return c.RemoveRef(bg, f.Owner.ID, "ref1")
			},
			func(c *core.Core, f fixture) error { return c.RemoveRef(bg, f.Owner.ID, "nope") }},
	)
	// The row is deleted, then the event refuses.
	lateFail["RemoveRef"] = func(c *core.Core, f fixture, snap func()) error {
		r, err := c.AddRef(bg, f.Owner.ID, f.T1.ID, core.RefInput{Kind: "spec", Target: "https://example.com/spec"})
		if err != nil {
			return err
		}
		snap()
		return c.RemoveRef(bg, f.Owner.ID, r.ID)
	}
}

// setRepo points a project at r.
func setRepo(t *testing.T, c *core.Core, f fixture, projectID string, r *gittest.R) {
	t.Helper()
	path, main := r.Dir, "main"
	if _, err := c.UpdateProject(bg, f.Owner.ID, projectID, core.ProjectPatch{RepoPath: &path, MainBranch: &main}); err != nil {
		t.Fatal(err)
	}
}

func TestAddRef(t *testing.T) {
	c, f, r := repoFixture(t)
	add := func(in core.RefInput) error { _, err := c.AddRef(bg, f.Owner.ID, f.T1.ID, in); return err }

	// A URL needs no repo.
	if err := add(core.RefInput{Kind: "design", Target: "https://example.com/d#x", Label: "Mock"}); err != nil {
		t.Fatal(err)
	}
	if err := add(core.RefInput{Kind: "other", Target: "http://example.com/"}); err != nil {
		t.Fatal(err)
	}
	// A repo path needs the project's repository.
	fieldErr(t, add(core.RefInput{Kind: "spec", Target: "docs/a.md"}), "target", "Set the project's repository first.")
	setRepo(t, c, f, f.Project.ID, r)
	// It doesn't have to exist on main.
	before := lastEventSeq(t, c)
	if err := add(core.RefInput{Kind: "spec", Target: "docs/a.md", Anchor: "intro-1"}); err != nil {
		t.Fatal(err)
	}
	if lastEventSeq(t, c) != before+1 {
		t.Fatal("add wrote no single event")
	}
	p := lastPayload(t, c)
	if p["kind"] != "spec" || p["target"] != "docs/a.md" || p["anchor"] != "intro-1" || p["ref_id"] == "" {
		t.Fatalf("payload %v", p)
	}

	for _, tc := range []struct {
		in    core.RefInput
		field string
		want  string
	}{
		{core.RefInput{Kind: "spec", Target: "javascript:alert(1)"}, "target", "Use a repo path or an http or https URL."},
		{core.RefInput{Kind: "spec", Target: "ftp://example.com/x"}, "target", "Use a repo path or an http or https URL."},
		{core.RefInput{Kind: "spec", Target: "https://"}, "target", "That isn't a URL."},
		{core.RefInput{Kind: "spec", Target: "/etc/passwd"}, "target", "That isn't a repo path."},
		{core.RefInput{Kind: "spec", Target: "../x.md"}, "target", "That isn't a repo path."},
		{core.RefInput{Kind: "spec", Target: ""}, "target", "Enter a repo path or a URL."},
		{core.RefInput{Kind: "memo", Target: "docs/b.md"}, "kind", "Choose requirement, design, plan, spec or other."},
		{core.RefInput{Kind: "spec", Target: "docs/b.md", Anchor: "Has Space"}, "anchor", "Use a heading anchor: lower-case letters, digits and hyphens."},
		{core.RefInput{Kind: "spec", Target: "docs/b.md", Anchor: "-x"}, "anchor", "Use a heading anchor: lower-case letters, digits and hyphens."},
		{core.RefInput{Kind: "spec", Target: "docs/b.md", Anchor: strings.Repeat("a", 81)}, "anchor", "Use a heading anchor: lower-case letters, digits and hyphens."},
		{core.RefInput{Kind: "spec", Target: "docs/b.md", Label: strings.Repeat("l", 121)}, "label", "Keep the label under 120 characters."},
		{core.RefInput{Kind: "spec", Target: "docs/a.md", Anchor: "intro-1"}, "target", "VAQ-1 already references docs/a.md."},
	} {
		if err := add(tc.in); err == nil {
			t.Errorf("%+v accepted", tc.in)
		} else {
			fieldErr(t, err, tc.field, tc.want)
		}
	}
	// A label of exactly 120 is fine; the same target with another anchor is not a repeat.
	if err := add(core.RefInput{Kind: "spec", Target: "docs/a.md", Anchor: "other", Label: strings.Repeat("l", 120)}); err != nil {
		t.Fatal(err)
	}
	// Remove writes one event too.
	var id string
	if err := c.DB.QueryRow(`SELECT id FROM ticket_refs LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	before = lastEventSeq(t, c)
	if err := c.RemoveRef(bg, f.Owner.ID, id); err != nil || lastEventSeq(t, c) != before+1 {
		t.Fatalf("remove: %v", err)
	}
	if p := lastPayload(t, c); p["ref_id"] != id {
		t.Fatalf("payload %v", p)
	}
}

func TestAddRefNeedsGit(t *testing.T) {
	c, _, f := newFixture(t)
	path := "docs/a.md"
	_, err := c.AddRef(bg, f.Owner.ID, f.T1.ID, core.RefInput{Kind: "spec", Target: path})
	fieldErr(t, err, "target", "Set the project's repository first.")
}

func TestRefsMoveWithTicket(t *testing.T) {
	c, f, r := repoFixture(t)
	setRepo(t, c, f, f.Project.ID, r)
	other := gittest.Repo(t)
	other.WriteCommit("a.md", "b", "first")
	setRepo(t, c, f, f.Other.ID, other)
	if _, err := c.AddRef(bg, f.Owner.ID, f.T1.ID, core.RefInput{Kind: "spec", Target: "docs/a.md"}); err != nil {
		t.Fatal(err)
	}
	var v int64
	if err := c.DB.QueryRow(`SELECT version FROM tickets WHERE id = ?`, f.T1.ID).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if _, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Other.ID, v); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := c.DB.QueryRow(`SELECT count(*) FROM ticket_refs WHERE ticket_id = ? AND target = 'docs/a.md'`, f.T1.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("refs after move: %d %v", n, err)
	}
}
