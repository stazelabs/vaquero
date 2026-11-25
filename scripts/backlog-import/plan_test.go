package main

import (
	"strings"
	"testing"
)

const sample = "# Backlog\n\nIntro text.\n\n" +
	"## Direction (owner, 2026-11-25; more to come)\n\n" +
	"1. First goal: self-hosting.\n" +
	"3. Open threads:\n" +
	"   - [ ] Git and GitHub, deeply: tight integration options, growing out of Phase 5.\n" +
	"   - [ ] A rich content editor: fluid mixed content. Constraints:\n" +
	"     - Stored text stays Markdown.\n" +
	"     - Strict CSP.\n" +
	"4. Design note: link by ID.\n\n" +
	"## Phases\n\n" +
	"- [x] Phase 0: Foundation — merged a69c53a\n" +
	"- [ ] Phase 3: Releases and upgrades (first public release)\n" +
	"  - [ ] Phase 3b: export and import, then move this backlog\n" +
	"  - [ ] Move attachments between storage backends (deferred from 3b)\n" +
	"  - [ ] Linear importer (deferred from 3b: CSV only) — pick the formats\n" +
	"- [ ] Phase 4: Hosted mode (and the container image, moved from Phase 3)\n" +
	"- [ ] Phase 7: LLM assists\n\n" +
	"## Deferred from Phase 2a\n\n" +
	"- [ ] Per-token API rate limits (and on failed authentication) — with hosted mode, Phase 4\n" +
	"- [x] `vaquero stop` (Phase 3a)\n" +
	"- [ ] Destructive buttons (comment delete, status delete) act on one click with no confirmation (review 1a #16)\n" +
	"- [ ] A JSON 500 for panics inside `/api/v1`; today Recover answers plain text\n\n" +
	"## Something new\n\n" +
	"- [ ] `c.tx` should `defer tx.Rollback()` so a panic mid-operation can't hold the only writer connection, and this title is far too long to keep (review 1a #4)\n"

func TestParsePlan(t *testing.T) {
	p, err := Parse(sample)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.ProjectDescription, "First goal: self-hosting.") || !strings.Contains(p.ProjectDescription, "Design note: link by ID.") {
		t.Errorf("project description should carry the Direction section, got %q", p.ProjectDescription)
	}
	wantMilestones := []string{"Phase 4: Hosted mode", "Phase 7: LLM assists"}
	if len(p.Milestones) != len(wantMilestones) {
		t.Fatalf("milestones = %+v, want %v", p.Milestones, wantMilestones)
	}
	for i, m := range p.Milestones {
		if m.Name != wantMilestones[i] {
			t.Errorf("milestone %d = %q, want %q", i, m.Name, wantMilestones[i])
		}
	}
	want := []Ticket{
		{Title: "Git and GitHub, deeply: tight integration options, growing out of Phase 5", Tag: "direction"},
		{Title: "A rich content editor: fluid mixed content. Constraints:", Tag: "direction"},
		{Title: "Move attachments between storage backends", Tag: "phase-3b"},
		{Title: "Linear importer", Tag: "linear-import"},
		{Title: "Per-token API rate limits", Tag: "phase-2a", Milestone: "Phase 4: Hosted mode"},
		{Title: "Destructive buttons act on one click with no confirmation", Tag: "phase-2a"},
		{Title: "A JSON 500 for panics inside `/api/v1`", Tag: "phase-2a"},
		{Title: "`c.tx` should `defer tx.Rollback()` so a panic mid-operation can't hold the…", Tag: "something-new"},
	}
	if len(p.Tickets) != len(want) {
		for _, tk := range p.Tickets {
			t.Logf("got %q [%s]", tk.Title, tk.Tag)
		}
		t.Fatalf("got %d tickets, want %d", len(p.Tickets), len(want))
	}
	for i, w := range want {
		g := p.Tickets[i]
		if g.Title != w.Title || g.Tag != w.Tag || g.Milestone != w.Milestone {
			t.Errorf("ticket %d = {%q %q %q}, want {%q %q %q}", i, g.Title, g.Tag, g.Milestone, w.Title, w.Tag, w.Milestone)
		}
		if len([]rune(g.Title)) > 80 {
			t.Errorf("ticket %d title is %d runes", i, len([]rune(g.Title)))
		}
	}
	// The Phase 5 mention in a direction thread is a reference, not membership:
	// only Phases 4 to 7 that exist as milestones are joined.
	if p.Tickets[0].Milestone != "" {
		t.Errorf("Phase 5 isn't a milestone in the sample, got %q", p.Tickets[0].Milestone)
	}
	rich := p.Tickets[1].Body
	if !strings.Contains(rich, "Stored text stays Markdown.") || !strings.Contains(rich, "Strict CSP.") {
		t.Errorf("continuation lines belong in the body, got %q", rich)
	}
	if !strings.HasSuffix(p.Tickets[4].Body, "From docs/BACKLOG.md, section: Deferred from Phase 2a") {
		t.Errorf("body should end with its source, got %q", p.Tickets[4].Body)
	}
	if !strings.HasPrefix(p.Tickets[4].Body, "Per-token API rate limits (and on failed authentication) — with hosted mode, Phase 4") {
		t.Errorf("body should start with the whole line, got %q", p.Tickets[4].Body)
	}
}
