package search_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/search"
)

// Review Focus 1: typed text never becomes an FTS5 syntax error.
func TestMatchQuery(t *testing.T) {
	for in, want := range map[string]string{
		"colum":         `"colum"*`,
		"fix the  gate": `"fix"* "the"* "gate"*`,
		`"`:             "",
		"AND (":         `"AND"*`,
		"*":             "",
		"-x":            `"x"*`,
		"NEAR(a b)":     `"NEAR"* "a"* "b"*`,
		"":              "",
		"!!!":           "",
		"café VAQ-12":   `"café"* "VAQ"* "12"*`,
		`say "hi"`:      `"say"* "hi"*`,
	} {
		if got := search.MatchQuery(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
	long := search.MatchQuery(strings.Repeat("word ", 100))
	if n := strings.Count(long, `"*`); n != 20 {
		t.Fatalf("%d terms", n)
	}
}

func setup(t *testing.T) (*core.Core, dbtest.DB, string, string) {
	t.Helper()
	d := dbtest.New(t)
	c := core.New(d.W, func() time.Time { return time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC) })
	owner, _ := c.EnsureOwner(context.Background())
	p, err := c.CreateProject(context.Background(), owner.ID, core.ProjectInput{Key: "VAQ", Name: "V"})
	if err != nil {
		t.Fatal(err)
	}
	return c, d, owner.ID, p.ID
}

func TestRunFindsByTitleBodyAndComment(t *testing.T) {
	c, d, by, pid := setup(t)
	ctx := context.Background()
	c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "Board columns"})
	c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "Other", Body: "the columns render"})
	tk, _ := c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "Third"})
	c.AddComment(ctx, by, tk.ID, "what about columns?")
	hits, err := search.Run(ctx, d.R, "colum", 50)
	if err != nil || len(hits) != 3 {
		t.Fatalf("%d %v", len(hits), err)
	}
	for _, h := range hits {
		if !strings.HasPrefix(h.Key, "VAQ-") || h.Status != "Todo" || len(h.Snippet) == 0 {
			t.Errorf("%+v", h)
		}
		marked := false
		for _, s := range h.Snippet {
			marked = marked || s.Mark
		}
		if !marked {
			t.Errorf("%s: no highlighted part", h.Key)
		}
	}
	if hits, _ := search.Run(ctx, d.R, `"`, 50); len(hits) != 0 {
		t.Fatal("empty query matched")
	}
}

func TestRunIgnoresDeletedComments(t *testing.T) {
	c, d, by, pid := setup(t)
	ctx := context.Background()
	tk, _ := c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "x"})
	cm, _ := c.AddComment(ctx, by, tk.ID, "secretword")
	c.DeleteComment(ctx, by, cm.ID)
	if hits, _ := search.Run(ctx, d.R, "secretword", 50); len(hits) != 0 {
		t.Fatalf("%d", len(hits))
	}
}

// Brackets in ticket text are text, not highlight markers.
func TestSnippetKeepsBrackets(t *testing.T) {
	c, d, by, pid := setup(t)
	ctx := context.Background()
	c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "See [docs] about widgets"})
	hits, _ := search.Run(ctx, d.R, "widgets", 50)
	if len(hits) != 1 {
		t.Fatalf("%d", len(hits))
	}
	var plain, marked string
	for _, s := range hits[0].Snippet {
		plain += s.Text
		if s.Mark {
			marked += s.Text
		}
	}
	if !strings.Contains(plain, "[docs]") || marked != "widgets" {
		t.Fatalf("plain %q marked %q", plain, marked)
	}
}
