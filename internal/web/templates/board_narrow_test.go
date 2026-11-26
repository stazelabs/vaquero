package templates

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

func narrowBoard() board.Board {
	return board.Board{
		Project: gen.Project{Key: "VAQ", Name: "Vaquero"},
		Filter:  board.Filter{Milestone: "m1"},
		Active:  []board.Column{{Status: gen.Status{ID: "s2", Name: "Todo", Category: "todo"}}},
		Rails: []board.Column{
			{Status: gen.Status{ID: "s1", Name: "Backlog", Category: "backlog"}, Cards: []board.Card{{Key: "VAQ-1", Title: "Later"}}},
			{Status: gen.Status{ID: "s9", Name: "Cancelled", Category: "cancelled"}},
		},
		Milestones:  []board.MilestoneProgress{{ID: "m1", Name: "Phase 4", Due: "2027-01-31", Done: 1, Total: 3}, {ID: "m2", Name: "Phase 5", Total: 2}},
		NoMilestone: 4,
	}
}

// classOf returns the class attribute of the first element matching tag
// and marker (an attribute that names it).
func classOf(t *testing.T, page, tag, marker string) string {
	t.Helper()
	m := regexp.MustCompile(`<` + tag + `\b[^>]*` + regexp.QuoteMeta(marker) + `[^>]*>`).FindString(page)
	if m == "" {
		t.Fatalf("no <%s %s> in:\n%s", tag, marker, page)
	}
	c := regexp.MustCompile(`class="([^"]*)"`).FindStringSubmatch(m)
	if c == nil {
		t.Fatalf("no class on %s", m)
	}
	return " " + c[1] + " "
}

// VAQ-30: below lg the side panel is hidden and a Milestones disclosure
// above the board carries the same filter links, inside the live #board so
// it follows refreshes.
func TestBoardMilestonesDisclosureBelowLg(t *testing.T) {
	page := html(t, BoardSection(narrowBoard()))
	if c := classOf(t, page, "aside", `aria-label="Milestones"`); !strings.Contains(c, " max-lg:hidden ") {
		t.Errorf("side panel class %q", c)
	}
	dc := classOf(t, page, "details", "data-milestones")
	if !strings.Contains(dc, " lg:hidden ") {
		t.Errorf("disclosure class %q", dc)
	}
	if strings.Index(page, "data-milestones") > strings.Index(page, "data-rail=") {
		t.Error("the disclosure must come before the board's rails and columns")
	}
	start := strings.Index(page, "<details data-milestones")
	end := start + strings.Index(page[start:], "</details>")
	disc := page[start:end]
	if !strings.Contains(disc, "<summary") || !strings.Contains(disc, "Milestones") {
		t.Errorf("no Milestones summary:\n%s", disc)
	}
	aside := page[strings.Index(page, "<aside"):strings.Index(page, "</aside>")]
	links := regexp.MustCompile(`<a [^>]*href="([^"]*)"`)
	var want, got []string
	for _, m := range links.FindAllStringSubmatch(aside, -1) {
		want = append(want, m[1])
	}
	for _, m := range links.FindAllStringSubmatch(disc, -1) {
		got = append(got, m[1])
	}
	if len(want) != 3 || strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("disclosure links %q, panel links %q", got, want)
	}
	if !strings.Contains(disc, `aria-current="true"`) || !strings.Contains(disc, "No milestone · 4 open") {
		t.Errorf("disclosure lacks the current filter or the no-milestone link:\n%s", disc)
	}
}
