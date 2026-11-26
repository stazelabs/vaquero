package handlers_test

import (
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// linkTickets makes VAQ-1 "Self", VAQ-2 "Login page", VAQ-3 to VAQ-12
// ("Filler N"), and OTH-1 "Login API".
func linkTickets(t *testing.T, e *webtest.Env) {
	t.Helper()
	p := project(t, e)
	titles := []string{"Self", "Login page"}
	for i := 3; i <= 12; i++ {
		titles = append(titles, "Filler")
	}
	for _, title := range titles {
		if _, err := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, p.ID, core.TicketInput{Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	o, err := e.Deps.Core.CreateProject(bg, e.Deps.Actor.ID, core.ProjectInput{Key: "OTH", Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, o.ID, core.TicketInput{Title: "Login API"}); err != nil {
		t.Fatal(err)
	}
}

var optionRe = regexp.MustCompile(`<option value="([^"]+)"`)

func optionValues(body string) []string {
	var out []string
	for _, m := range optionRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// VAQ-109: with no links the form folds behind "Add link"; it names the
// ticket by its key; the ticket box suggests tickets as you type.
func TestLinkFormFoldsWithoutLinks(t *testing.T) {
	e := webtest.New(t)
	linkTickets(t, e)
	c := e.Client()
	page := c.Get("/t/VAQ-1").Body
	links := page[strings.Index(page, "Links</h2>"):strings.Index(page, `id="attachments"`)]
	if !regexp.MustCompile(`(?s)<details[^>]*data-link-add[^>]*>\s*<summary[^>]*>.*Add link.*</summary>.*<form method="post" action="/t/VAQ-1/links"`).MatchString(links) {
		t.Fatalf("no folded form:\n%s", links)
	}
	// Opened, the button gives way to the form, and app.js puts the cursor
	// in the ticket box.
	if !regexp.MustCompile(`<summary[^>]*group-open:hidden`).MatchString(links) {
		t.Error("the Add link button stays above the open form")
	}
	if js, err := os.ReadFile("../static/js/app.js"); err != nil || !strings.Contains(string(js), "details[data-link-add]") {
		t.Errorf("app.js doesn't focus the opened link form: %v", err)
	}
	if strings.Contains(links, "No links.") {
		t.Error("the fold says it all; no separate empty line")
	}
	contains(t, links, `<label for="f-kind" class="mb-1 block text-xs font-semibold uppercase tracking-wider text-ink/80">VAQ-1</label>`)
	if strings.Contains(links, "This ticket") {
		t.Error(`still "This ticket"`)
	}
	contains(t, links, `list="link-targets"`, `hx-get="/t/VAQ-1/link-targets"`, `<datalist id="link-targets"`)

	// A refused link opens the fold, with its message.
	r := c.Post("/t/VAQ-1/links", url.Values{"kind": {"blocks"}, "to": {"nope"}})
	if r.Status != 422 {
		t.Fatal(r.Status)
	}
	if !regexp.MustCompile(`<details[^>]*data-link-add[^>]*open`).MatchString(r.Body) {
		t.Error("a refused link keeps the form folded")
	}

	// With a link the form shows as it is.
	if _, err := e.Deps.Core.AddLink(bg, e.Deps.Actor.ID, mustTicket(t, e, 1), "relates", core.TicketKey{Project: "VAQ", Number: 2}); err != nil {
		t.Fatal(err)
	}
	page = c.Get("/t/VAQ-1").Body
	if strings.Contains(page, "data-link-add") {
		t.Error("the form is folded although the ticket has links")
	}
	contains(t, page, `action="/t/VAQ-1/links"`)
}

func mustTicket(t *testing.T, e *webtest.Env, n int64) string {
	t.Helper()
	p, err := e.Deps.R.GetProjectByKey(bg, "VAQ")
	if err != nil {
		t.Fatal(err)
	}
	tk, err := e.Deps.R.GetTicketByNumber(bg, gen.GetTicketByNumberParams{ProjectID: p.ID, Number: n})
	if err != nil {
		t.Fatal(err)
	}
	return tk.ID
}

func TestLinkTargetsSuggest(t *testing.T) {
	e := webtest.New(t)
	linkTickets(t, e)
	c := e.Client()
	for _, tc := range []struct {
		q    string
		want []string
	}{
		{"", nil},
		{"1", []string{"VAQ-12", "VAQ-11", "VAQ-10"}}, // the number, prefix optional; never the ticket itself
		{"12", []string{"VAQ-12"}},
		{"#2", []string{"VAQ-2"}},
		{"vaq-2", []string{"VAQ-2"}},
		{"oth-", []string{"OTH-1"}},
		{"login", []string{"VAQ-2", "OTH-1"}}, // by title, this project first
		{"self", nil},
		{"zzz", nil},
	} {
		r := c.Get("/t/VAQ-1/link-targets?to=" + url.QueryEscape(tc.q))
		if r.Status != 200 {
			t.Fatalf("%q: %d", tc.q, r.Status)
		}
		if got := optionValues(r.Body); strings.Join(got, " ") != strings.Join(tc.want, " ") {
			t.Errorf("%q: %v, want %v", tc.q, got, tc.want)
		}
	}
	contains(t, c.Get("/t/VAQ-1/link-targets?to=login").Body, `<option value="VAQ-2" label="VAQ-2 Login page">`)
}

// A bare number links to that ticket of the same project.
func TestLinkByBareNumber(t *testing.T) {
	e := webtest.New(t)
	linkTickets(t, e)
	c := e.Client()
	r := c.Post("/t/VAQ-1/links", url.Values{"kind": {"blocks"}, "to": {" 2 "}})
	if r.Status != 303 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	contains(t, c.Follow(r).Body, "Link added.", `href="/t/VAQ-2"`)
}

// The query is put in NFC like stored titles, and a key-like query that
// names no project searches titles instead.
func TestLinkTargetsNormaliseAndFallBack(t *testing.T) {
	e := webtest.New(t)
	linkTickets(t, e)
	p, err := e.Deps.R.GetProjectByKey(bg, "VAQ")
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Caf\u00e9 menu", "UI-kit refresh", "Ship v2-3 notes"} {
		if _, err := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, p.ID, core.TicketInput{Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	c := e.Client()
	for _, tc := range []struct{ q, want string }{
		{"cafe\u0301", "VAQ-13"}, // e + combining acute, NFC é
		{"UI-", "VAQ-14"},        // no project UI: a title search
		{"v2-3", "VAQ-15"},       // no project V2
	} {
		got := optionValues(c.Get("/t/VAQ-1/link-targets?to=" + url.QueryEscape(tc.q)).Body)
		if strings.Join(got, " ") != tc.want {
			t.Errorf("%q: %v, want %s", tc.q, got, tc.want)
		}
	}
}
