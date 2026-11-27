package templates

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// VAQ-171: Focus's lanes and the list draw tickets as one 28 px row:
// priority mark, the number alone (its project's key kept for screen
// readers), the title cut with an ellipsis, then the trailing meta.
func TestFocusAndListUseRows(t *testing.T) {
	k := board.Card{Key: "VAQ-12", Title: "A long title", Priority: 1, Ready: true, Assignee: "Owner", AssigneeID: "", When: "today"}
	fo := board.Focus{
		Project:  gen.Project{Key: "VAQ", Name: "Vaquero"},
		Next:     []board.Card{k},
		Finished: []board.Card{{Key: "VAQ-3", Title: "Shipped", When: "yesterday"}},
		Waiting:  []board.Waiting{{Key: "VAQ-4", Title: "Stuck", Reason: "Blocked by VAQ-1"}},
	}
	checkRow := func(where, page, key, num string) {
		t.Helper()
		li := regexp.MustCompile(`<li data-card="` + key + `"[^>]*>`).FindString(page)
		if li == "" {
			t.Fatalf("%s: no row for %s:\n%s", where, key, page)
		}
		for _, want := range []string{" h-row ", " flex ", " border-b ", " border-line "} {
			if !strings.Contains(" "+regexp.MustCompile(`class="([^"]*)"`).FindStringSubmatch(li)[1]+" ", want) {
				t.Errorf("%s: %s's row lacks %q: %s", where, key, want, li)
			}
		}
		if !regexp.MustCompile(`<span class="sr-only">VAQ-</span>` + num + `<`).MatchString(page) {
			t.Errorf("%s: %s doesn't show its number alone:\n%s", where, key, page)
		}
	}
	focus := html(t, FocusSection(fo))
	checkRow("focus next", focus, "VAQ-12", "12")
	checkRow("focus finished", focus, "VAQ-3", "3")
	checkRow("focus waiting", focus, "VAQ-4", "4")
	for _, want := range []string{"truncate", "yesterday", "Blocked by VAQ-1", `data-icon="` + PriorityIcon(1) + `"`} {
		if !strings.Contains(focus, want) {
			t.Errorf("focus lacks %q", want)
		}
	}
	if strings.Contains(focus, "shadow") {
		t.Error("focus rows carry a shadow; surfaces are separated by rules")
	}
	list := html(t, listRow(k))
	checkRow("list", list, "VAQ-12", "12")
}

// A board card keeps a title line and, when it has any, one meta line.
func TestBoardCardTwoLines(t *testing.T) {
	b := board.Board{Project: gen.Project{Key: "VAQ", Name: "Vaquero"}}
	col := board.Column{Status: gen.Status{ID: "s1", Name: "Todo", Category: "todo"}, Cards: []board.Card{
		{Key: "VAQ-1", Title: "Bare"},
		{Key: "VAQ-2", Title: "Tagged", Priority: 2, Tags: []string{"docs"}},
	}}
	page := html(t, column(b, col))
	card := func(key string) string {
		return regexp.MustCompile(`(?s)<li data-card="` + key + `".*?</li>`).FindString(page)
	}
	if bare := card("VAQ-1"); strings.Contains(bare, "data-card-meta") || strings.Contains(bare, "shadow") {
		t.Errorf("a card with nothing to say draws a meta line or a shadow:\n%s", bare)
	}
	if tagged := card("VAQ-2"); strings.Count(tagged, "data-card-meta") != 1 || !strings.Contains(tagged, "docs") {
		t.Errorf("a tagged card wants one meta line:\n%s", tagged)
	}
}
