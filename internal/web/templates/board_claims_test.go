package templates

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

func TestBoardShowsClaim(t *testing.T) {
	b := board.Board{Project: gen.Project{Key: "VAQ", Name: "Vaquero"}}
	col := board.Column{Status: gen.Status{ID: "s1", Name: "In progress", Category: "in_progress"}, Cards: []board.Card{
		{Key: "VAQ-1", Title: "Held", Assignee: "builder", AssigneeIsAgent: true, ClaimedBy: "builder", ClaimantIsAgent: true, ClaimExpiresAt: "2026-11-18T12:15:00.000Z"},
	}}
	page := html(t, column(b, col))
	for _, want := range []string{`data-icon="agent"`, `data-icon="lease"`, `data-claim-expires="2026-11-18T12:15:00.000Z"`, "builder", "until 12:15 UTC"} {
		if !strings.Contains(page, want) {
			t.Errorf("card lacks %q:\n%s", want, page)
		}
	}
	if strings.Count(page, ">builder<") != 1 {
		t.Errorf("the claimant shows twice:\n%s", page)
	}
	list := html(t, listRow(col.Cards[0]))
	if !strings.Contains(list, `data-claim-expires="2026-11-18T12:15:00.000Z"`) {
		t.Errorf("list row lacks the claim:\n%s", list)
	}
}
