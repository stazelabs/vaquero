package search_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/search"
)

func keys(hits []search.Hit) string {
	var ks []string
	for _, h := range hits {
		ks = append(ks, h.Key)
	}
	return strings.Join(ks, ",")
}

func snippetText(h search.Hit) (plain, marked string) {
	for _, p := range h.Snippet {
		plain += p.Text
		if p.Mark {
			marked += p.Text + " "
		}
	}
	return plain, strings.TrimSpace(marked)
}

// VAQ-40 (1): decomposed and composed accents find each other, both for
// text core writes now (stored NFC) and for text stored decomposed before.
func TestRunNormalisesAccents(t *testing.T) {
	c, d, by, pid := setup(t)
	ctx := context.Background()
	nfd := norm.NFD.String("résumé")
	tk, _ := c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "Update " + nfd})
	stored, _ := d.RQ.GetTicket(ctx, tk.ID)
	if stored.Title != "Update résumé" || !norm.NFC.IsNormalString(stored.Title) {
		t.Fatalf("title not stored NFC: %q", stored.Title)
	}
	// An older row, written decomposed before core normalised.
	old, _ := c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "old"})
	if _, err := d.W.Exec(`UPDATE tickets SET title = ? WHERE id = ?`, "Old "+nfd, old.ID); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"résumé", nfd, "resume", "RÉSUMÉ"} {
		res, err := search.Run(ctx, d.R, q, 50)
		if err != nil || keys(res.Hits) != "VAQ-1,VAQ-2" && keys(res.Hits) != "VAQ-2,VAQ-1" {
			t.Errorf("%q: %s %v", q, keys(res.Hits), err)
		}
	}
	cm, _ := c.AddComment(ctx, by, tk.ID, "a "+nfd)
	if cm.Body != "a résumé" {
		t.Fatalf("comment not stored NFC: %q", cm.Body)
	}
}

// VAQ-40 (2): a ticket matches when its terms are split across its title,
// description and comments.
func TestRunMatchesTermsAcrossTicketAndComments(t *testing.T) {
	c, d, by, pid := setup(t)
	ctx := context.Background()
	a, _ := c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "alpha thing", Body: "charlie"})
	c.AddComment(ctx, by, a.ID, "bravo said")
	c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "alpha only"})
	b, _ := c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "other"})
	c.AddComment(ctx, by, b.ID, "alpha")
	c.AddComment(ctx, by, b.ID, "bravo")
	for q, want := range map[string]string{
		"alpha bravo":         "VAQ-1,VAQ-3",
		"alpha bravo charlie": "VAQ-1",
		"alpha":               "VAQ-1,VAQ-2,VAQ-3",
		"bravo zulu":          "",
	} {
		res, err := search.Run(ctx, d.R, q, 50)
		got := strings.Split(keys(res.Hits), ",")
		if err != nil || !sameSet(got, strings.Split(want, ",")) || res.Total != len(res.Hits) {
			t.Errorf("%q: %v (total %d) want %s", q, got, res.Total, want)
		}
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]bool{}
	for _, x := range a {
		m[x] = true
	}
	for _, x := range b {
		if !m[x] {
			return false
		}
	}
	return true
}

// VAQ-40 (3): one merged ranking: a title hit, then a description hit,
// then a comment hit, whichever table they came from.
func TestRunRanksTitleThenBodyThenComment(t *testing.T) {
	c, d, by, pid := setup(t)
	ctx := context.Background()
	cm, _ := c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "one"}) // VAQ-1
	c.AddComment(ctx, by, cm.ID, "widget widget widget widget")
	c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "two", Body: "a widget in the text"})                      // VAQ-2
	c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "The widget", Body: "long long long long long long text"}) // VAQ-3
	res, err := search.Run(ctx, d.R, "widget", 50)
	if err != nil || keys(res.Hits) != "VAQ-3,VAQ-2,VAQ-1" {
		t.Fatalf("%s %v", keys(res.Hits), err)
	}
	// More terms in better places rank higher.
	c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "widget gear", Body: "x"})      // VAQ-4: both in title
	c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "widget", Body: "gear inside"}) // VAQ-5: title + body
	res, _ = search.Run(ctx, d.R, "widget gear", 50)
	if keys(res.Hits) != "VAQ-4,VAQ-5" {
		t.Fatalf("%s", keys(res.Hits))
	}
}

// VAQ-40 (4): a title hit's snippet comes from the description, not the
// title again: the description's match when it has one, else its opening.
func TestRunTitleHitSnippetFromBody(t *testing.T) {
	c, d, by, pid := setup(t)
	ctx := context.Background()
	c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "Gizmo", Body: "Start here. The gizmo breaks on Tuesdays."})
	c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "Sprocket", Body: "Nothing in common with the title."})
	res, _ := search.Run(ctx, d.R, "gizmo", 50)
	plain, marked := snippetText(res.Hits[0])
	if marked != "gizmo" || strings.Contains(plain, "Gizmo") {
		t.Fatalf("plain %q marked %q", plain, marked)
	}
	res, _ = search.Run(ctx, d.R, "sprocket", 50)
	plain, marked = snippetText(res.Hits[0])
	if marked != "" || plain != "Nothing in common with the title." {
		t.Fatalf("plain %q marked %q", plain, marked)
	}
}

// VAQ-40 (5): the total counts every match, past the limit.
func TestRunCountsPastTheLimit(t *testing.T) {
	c, d, by, pid := setup(t)
	ctx := context.Background()
	for i := range 7 {
		c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "match " + strconv.Itoa(i)})
	}
	res, err := search.Run(ctx, d.R, "match", 5)
	if err != nil || len(res.Hits) != 5 || res.Total != 7 {
		t.Fatalf("%d of %d, %v", len(res.Hits), res.Total, err)
	}
}

// Review Focus 3: hostile input stays text, whatever it holds.
func TestRunHostileInput(t *testing.T) {
	c, d, by, pid := setup(t)
	ctx := context.Background()
	c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "plain"})
	for _, q := range []string{`title : plain`, `"plain" OR 1=1 --`, `plain'); DROP TABLE tickets; --`, `{title body}: plain`, `NEAR(plain plain)`, `plain*`, `^plain`} {
		if _, err := search.Run(ctx, d.R, q, 50); err != nil {
			t.Errorf("%q: %v", q, err)
		}
	}
	if res, _ := search.Run(ctx, d.R, strings.Repeat("plain ", 30), 50); len(res.Hits) != 1 {
		t.Fatal("repeated terms")
	}
}

// K1 review M8: a stray combining mark is dropped, not searched for, so
// it doesn't empty the results.
func TestRunIgnoresLoneCombiningMark(t *testing.T) {
	c, d, by, pid := setup(t)
	ctx := context.Background()
	c.CreateTicket(ctx, by, pid, core.TicketInput{Title: "gadget"})
	res, err := search.Run(ctx, d.R, "gadget ́", 50)
	if err != nil || len(res.Hits) != 1 {
		t.Fatalf("%d %v", len(res.Hits), err)
	}
	if res, _ := search.Run(ctx, d.R, "́", 50); len(res.Hits) != 0 {
		t.Fatal("a lone mark matched")
	}
}
