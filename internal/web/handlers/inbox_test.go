package handlers_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// inboxGroup is the inbox's group section called name ("" when absent).
func inboxGroup(page, name string) string {
	i := strings.Index(page, `data-inbox-group="`+name+`"`)
	if i < 0 {
		return ""
	}
	j := strings.Index(page[i:], "</section>")
	if j < 0 {
		return page[i:]
	}
	return page[i : i+j]
}

// inboxCard is the decision card with id dec, as the page draws it.
func inboxCard(t *testing.T, page, id string) string {
	t.Helper()
	i := strings.Index(page, `id="decision-`+id+`"`)
	if i < 0 {
		t.Fatalf("no card for %s", id)
	}
	j := strings.Index(page[i:], "</article>")
	if j < 0 {
		t.Fatal("card never ends")
	}
	return page[i : i+j]
}

// at runs f with core's clock set to d before webtest.Start.
func at(e *webtest.Env, ago time.Duration, f func()) {
	was := e.Deps.Core.Now
	e.Deps.Core.Now = func() time.Time { return webtest.Start.Add(-ago) }
	defer func() { e.Deps.Core.Now = was }()
	f()
}

func agent(t *testing.T, e *webtest.Env) gen.Actor {
	t.Helper()
	a, err := e.Deps.Core.EnsureAgent(bg, e.Deps.Actor.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func newProjectKey(t *testing.T, e *webtest.Env, key, name string) gen.Project {
	t.Helper()
	p, err := e.Deps.Core.CreateProject(bg, e.Deps.Actor.ID, core.ProjectInput{Key: key, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func ask(t *testing.T, e *webtest.Env, by string, in core.DecisionInput) gen.Decision {
	t.Helper()
	d, err := e.Deps.Core.AskDecision(bg, by, in)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func savePlan(t *testing.T, e *webtest.Env, ticketID, body string, base int64) {
	t.Helper()
	if _, err := e.Deps.Core.SavePlan(bg, e.Deps.Actor.ID, ticketID, body, base); err != nil {
		t.Fatal(err)
	}
}

func planApproval(t *testing.T, e *webtest.Env, by string, p gen.Project, ids ...string) gen.Decision {
	t.Helper()
	return ask(t, e, by, core.DecisionInput{ProjectID: p.ID, TicketIDs: ids, Kind: core.DecisionApproval, Subject: "plan", Question: "Approve the plan?"})
}

// lapse makes agent a's claim on ticket id lapse ago before Start.
func lapse(t *testing.T, e *webtest.Env, a gen.Actor, id string, ago time.Duration) {
	t.Helper()
	at(e, ago+time.Hour, func() {
		if _, err := e.Deps.Core.Claim(bg, a.ID, id, 0); err != nil {
			t.Fatal(err)
		}
	})
	at(e, ago, func() {
		if n, err := e.Deps.Core.ExpireClaims(bg); err != nil || n != 1 {
			t.Fatalf("expired %d: %v", n, err)
		}
	})
}

func ruling(t *testing.T, e *webtest.Env, by string, p gen.Project, q string, ago time.Duration) gen.Decision {
	t.Helper()
	var d gen.Decision
	at(e, ago, func() {
		var err error
		d, err = e.Deps.Core.RecordRuling(bg, by, core.DecisionInput{ProjectID: p.ID, Kind: core.DecisionRuling, Question: q,
			Options: []core.Option{{Label: "Left"}, {Label: "Right"}}}, core.Answer{Option: "Left"})
		if err != nil {
			t.Fatal(err)
		}
	})
	return d
}

var countRe = regexp.MustCompile(`data-count="(\d+)"`)

func groupCount(t *testing.T, group string) string {
	t.Helper()
	m := countRe.FindStringSubmatch(group)
	if m == nil {
		t.Fatalf("group has no count: %.200s", group)
	}
	return m[1]
}

// Ruling T-R5: Decide is the open questions and approvals, oldest first;
// Review the in-review tickets of workflow-on projects; Check the lapses
// of the last day; Rulings agents made those of the last week not
// overruled. An archived project's are left out.
func TestInboxGroups(t *testing.T) {
	e := webtest.New(t)
	owner := e.Deps.Actor.ID
	vaq := project(t, e)
	workflowOn(t, e)
	oth := newProjectKey(t, e, "OTH", "Other")
	arc := newProjectKey(t, e, "ARC", "Archive")
	a := agent(t, e)

	epic := ticket(t, e, vaq, core.TicketInput{Title: "Epic"})
	savePlan(t, e, epic.ID, "The plan.", 0)
	ticket(t, e, vaq, core.TicketInput{Title: "Waiting for review", StatusID: statusIn(t, e, vaq, "In review")})
	ticket(t, e, oth, core.TicketInput{Title: "Off review", StatusID: statusIn(t, e, oth, "In review")})
	lapsed := ticket(t, e, vaq, core.TicketInput{Title: "Lapsed lately"})
	old := ticket(t, e, vaq, core.TicketInput{Title: "Lapsed long ago"})

	var question gen.Decision
	at(e, 3*time.Hour, func() {
		question = ask(t, e, a.ID, core.DecisionInput{ProjectID: vaq.ID, Kind: core.DecisionQuestion, Question: "Which colour?",
			Options: []core.Option{{Label: "Red"}, {Label: "Blue"}}})
	})
	var approval gen.Decision
	at(e, time.Hour, func() { approval = planApproval(t, e, a.ID, vaq, epic.ID) })
	lapse(t, e, a, old.ID, 30*time.Hour)
	lapse(t, e, a, lapsed.ID, 2*time.Hour)
	recent := ruling(t, e, a.ID, vaq, "Tabs or spaces?", 48*time.Hour)
	ruling(t, e, a.ID, vaq, "Ancient ruling?", 9*24*time.Hour)
	over := ruling(t, e, a.ID, vaq, "Overruled ruling?", 24*time.Hour)
	if _, err := e.Deps.Core.AnswerDecision(bg, owner, over.ID, core.Answer{Option: "Right"}); err != nil {
		t.Fatal(err)
	}
	ask(t, e, a.ID, core.DecisionInput{ProjectID: arc.ID, Kind: core.DecisionQuestion, Question: "Archived question?"})
	if err := e.Deps.Core.SetProjectArchived(bg, owner, arc.ID, true); err != nil {
		t.Fatal(err)
	}

	page := e.Client().Get("/inbox").Body
	contains(t, page, `id="inbox"`, "data-live", `hx-trigger="vq:change"`, "data-inbox")
	decide := inboxGroup(page, "decide")
	if groupCount(t, decide) != "2" || strings.Count(decide, "data-decision=") != 2 {
		t.Fatalf("decide: %s", decide)
	}
	if qi, ai := strings.Index(decide, question.ID), strings.Index(decide, approval.ID); qi < 0 || ai < 0 || qi > ai {
		t.Fatalf("decide not oldest first: %d %d", qi, ai)
	}
	review := inboxGroup(page, "review")
	if groupCount(t, review) != "1" || !strings.Contains(review, "Waiting for review") || strings.Contains(page, "Off review") {
		t.Fatalf("review: %s", review)
	}
	check := inboxGroup(page, "check")
	if groupCount(t, check) != "1" || !strings.Contains(check, "claude&#39;s claim on VAQ-3 lapsed") || strings.Contains(check, "VAQ-4") {
		t.Fatalf("check: %s", check)
	}
	contains(t, check, "Dismiss", "data-lapse=")
	rulings := inboxGroup(page, "rulings")
	if groupCount(t, rulings) != "1" || !strings.Contains(rulings, recent.ID) || strings.Contains(page, "Ancient ruling?") {
		t.Fatalf("rulings: %s", rulings)
	}
	contains(t, rulings, "Overrule", "Tabs or spaces?")
	if strings.Contains(rulings, "Overruled ruling?") {
		t.Fatal("an overruled ruling is listed")
	}
	lacks(t, page, "Archived question?", "Nothing needs you.")
}

// A recommended option comes first, marked; without one the options keep
// their order and nothing is marked.
func TestInboxCardRecommended(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	a := agent(t, e)
	opts := []core.Option{{Label: "Alpha", Tradeoff: "Cheap"}, {Label: "Beta", Tradeoff: "Fast"}, {Label: "Gamma", Tradeoff: "Safe"}}
	rec := ask(t, e, a.ID, core.DecisionInput{ProjectID: p.ID, Kind: core.DecisionQuestion, Question: "Pick **one**", Options: opts, Recommended: "gamma"})
	plain := ask(t, e, a.ID, core.DecisionInput{ProjectID: p.ID, Kind: core.DecisionQuestion, Question: "Pick another", Options: opts})
	page := e.Client().Get("/inbox").Body
	c := inboxCard(t, page, rec.ID)
	g, al := strings.Index(c, `value="Gamma"`), strings.Index(c, `value="Alpha"`)
	if g < 0 || al < 0 || g > al || !strings.Contains(c, "Recommended") {
		t.Fatalf("recommended not first and marked: %s", c)
	}
	contains(t, c, "<strong>one</strong>", "Cheap", "Safe", "Question · VAQ · claude", `data-option`)
	c = inboxCard(t, page, plain.ID)
	if strings.Contains(c, "Recommended") {
		t.Fatal("marked without a recommendation")
	}
	if al, b, g := strings.Index(c, `value="Alpha"`), strings.Index(c, `value="Beta"`), strings.Index(c, `value="Gamma"`); al > b || b > g {
		t.Fatalf("not in the asked order: %s", c)
	}
}

// An approval says what approving covers.
func TestInboxCovers(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	workflowOn(t, e)
	mk := func(subs int, plan bool) string {
		epic := ticket(t, e, p, core.TicketInput{Title: "Epic"})
		if plan {
			savePlan(t, e, epic.ID, "One.", 0)
			savePlan(t, e, epic.ID, "Two.", 1)
		}
		for range subs {
			ticket(t, e, p, core.TicketInput{Title: "Sub", ParentID: epic.ID})
		}
		d := planApproval(t, e, e.Deps.Actor.ID, p, epic.ID)
		return d.ID
	}
	three, one, none, noPlan := mk(3, true), mk(1, true), mk(0, true), mk(0, false)
	page := e.Client().Get("/inbox").Body
	contains(t, inboxCard(t, page, three), "Covers VAQ-1 (plan rev 2) and 3 sub-tickets as asked", "Show plan", `hx-get="/t/VAQ-1/plan"`)
	contains(t, inboxCard(t, page, one), "Covers VAQ-5 (plan rev 2) and 1 sub-ticket as asked")
	contains(t, inboxCard(t, page, none), "Covers VAQ-7 (plan rev 2) as asked")
	c := inboxCard(t, page, noPlan)
	contains(t, c, "Covers VAQ-8 (no plan) as asked")
	lacks(t, c, "Show plan")

	// Show plan's fragment is the plan as the ticket page draws it.
	r := e.Client().Get("/t/VAQ-1/plan")
	if r.Status != http.StatusOK {
		t.Fatalf("plan fragment %d", r.Status)
	}
	contains(t, r.Body, "Two.", "Revision 2")
	lacks(t, r.Body, "<html")
}

// An option button answers through core as the owner, directly; the card
// leaves and the page refreshes.
func TestInboxAnswer(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	workflowOn(t, e)
	tk := ticket(t, e, p, core.TicketInput{Title: "Build it"})
	savePlan(t, e, tk.ID, "The plan.", 0)
	d := planApproval(t, e, agent(t, e).ID, p, tk.ID)
	c := e.Client()
	r := c.PostHX("/inbox/"+d.ID+"/answer", url.Values{"option": {"Approve"}, "note": {"Looks right."}})
	if r.Status != http.StatusOK || strings.TrimSpace(r.Body) != "" || r.Header.Get("HX-Trigger") != "vq:refresh" {
		t.Fatalf("%d %q %q", r.Status, r.Body, r.Header.Get("HX-Trigger"))
	}
	if n := dbCount(t, e, `SELECT count(*) FROM decisions WHERE id = ? AND state = 'answered' AND answer = 'Approve'
		AND answered_by = ? AND relayed_by IS NULL AND answer_note = 'Looks right.'`, d.ID, e.Deps.Actor.ID); n != 1 {
		t.Fatal("not the owner's direct answer")
	}
	page := c.Get("/inbox").Body
	lacks(t, page, `id="decision-`+d.ID+`"`)
	answered := inboxGroup(page, "answered")
	contains(t, answered, "Answered today (1)", "Answered directly by you", "Approve")

	// Without htmx: a redirect to the inbox with a toast.
	q := ask(t, e, e.Deps.Actor.ID, core.DecisionInput{ProjectID: p.ID, Kind: core.DecisionQuestion, Question: "Now?", Options: []core.Option{{Label: "Yes"}}})
	r = c.Post("/inbox/"+q.ID+"/answer", url.Values{"option": {"Yes"}})
	if r.Status != http.StatusSeeOther || r.Location != "/inbox" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	contains(t, c.Follow(r).Body, "Answered.")

	// A question without options takes a written answer.
	w := ask(t, e, e.Deps.Actor.ID, core.DecisionInput{ProjectID: p.ID, Kind: core.DecisionQuestion, Question: "What name?"})
	contains(t, inboxCard(t, c.Get("/inbox").Body, w.ID), `name="text"`)
	if r = c.PostHX("/inbox/"+w.ID+"/answer", url.Values{"text": {"Vaquero"}}); strings.TrimSpace(r.Body) != "" {
		t.Fatalf("%q", r.Body)
	}

	// Overrule: a ruling's card answers through the same route.
	rl := ruling(t, e, agent(t, e).ID, p, "Which side?", time.Hour)
	if r = c.PostHX("/inbox/"+rl.ID+"/answer", url.Values{"option": {"Right"}}); strings.TrimSpace(r.Body) != "" {
		t.Fatalf("%q", r.Body)
	}
	if n := dbCount(t, e, `SELECT count(*) FROM decisions WHERE id = ? AND answer = 'Right' AND answered_by = ?`, rl.ID, e.Deps.Actor.ID); n != 1 {
		t.Fatal("not overruled")
	}

	// Withdraw, as the owner.
	wd := ask(t, e, agent(t, e).ID, core.DecisionInput{ProjectID: p.ID, Kind: core.DecisionQuestion, Question: "Withdraw me?"})
	contains(t, inboxCard(t, c.Get("/inbox").Body, wd.ID), "Withdraw", "data-confirm=")
	if r = c.PostHX("/inbox/"+wd.ID+"/withdraw", nil); strings.TrimSpace(r.Body) != "" || r.Header.Get("HX-Trigger") != "vq:refresh" {
		t.Fatalf("%q", r.Body)
	}
	if n := dbCount(t, e, `SELECT count(*) FROM decisions WHERE id = ? AND state = 'withdrawn'`, wd.ID); n != 1 {
		t.Fatal("not withdrawn")
	}
}

// Review Focus 2: a refusal answers the card again with core's sentence,
// and the decision stays open.
func TestInboxAnswerRefused(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	workflowOn(t, e)
	tk := ticket(t, e, p, core.TicketInput{Title: "Build it"})
	savePlan(t, e, tk.ID, "The plan.", 0)
	d := planApproval(t, e, agent(t, e).ID, p, tk.ID)
	savePlan(t, e, tk.ID, "A new plan.", 1)
	c := e.Client()
	r := c.PostHX("/inbox/"+d.ID+"/answer", url.Values{"option": {"Approve"}, "note": {"kept"}})
	if r.Status != http.StatusOK || r.Header.Get("HX-Trigger") != "" {
		t.Fatalf("%d %q", r.Status, r.Header.Get("HX-Trigger"))
	}
	contains(t, r.Body, `id="decision-`+d.ID+`"`, "The plan or requirement for VAQ-1 changed since this was asked; ask again.", "kept")
	lacks(t, r.Body, "<html")
	if n := dbCount(t, e, `SELECT count(*) FROM decisions WHERE id = ? AND state = 'open'`, d.ID); n != 1 {
		t.Fatal("the decision isn't open")
	}

	// Answered in another tab: the second answer reads so.
	if r = c.PostHX("/inbox/"+d.ID+"/answer", url.Values{"option": {"Request changes"}}); strings.TrimSpace(r.Body) != "" {
		t.Fatalf("%q", r.Body)
	}
	r = c.PostHX("/inbox/"+d.ID+"/answer", url.Values{"option": {"Request changes"}})
	contains(t, r.Body, `id="decision-`+d.ID+`"`, "This decision is already answered.")

	// Without htmx the refusal is a toast on the inbox.
	r = c.Post("/inbox/"+d.ID+"/answer", url.Values{"option": {"Approve"}})
	if r.Status != http.StatusSeeOther || r.Location != "/inbox" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	contains(t, c.Follow(r).Body, "This decision is already answered.")

	// An unknown decision leaves the list.
	if r = c.PostHX("/inbox/01JZZZZZZZZZZZZZZZZZZZZZZZ/answer", url.Values{"option": {"Yes"}}); r.Status != http.StatusOK || strings.TrimSpace(r.Body) != "" {
		t.Fatalf("%d %q", r.Status, r.Body)
	}
}

// Answered today shows how each answer came: direct or relayed.
func TestInboxAnsweredToday(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	a := agent(t, e)
	q := ask(t, e, a.ID, core.DecisionInput{ProjectID: p.ID, Kind: core.DecisionQuestion, Question: "Relay me?", Options: []core.Option{{Label: "Yes"}}})
	if _, err := e.Deps.Core.AnswerDecision(bg, e.Deps.Actor.ID, q.ID, core.Answer{Option: "Yes", RelayedBy: a.ID, Words: "yes, go"}); err != nil {
		t.Fatal(err)
	}
	y := ask(t, e, a.ID, core.DecisionInput{ProjectID: p.ID, Kind: core.DecisionQuestion, Question: "Yesterday?", Options: []core.Option{{Label: "Yes"}}})
	at(e, 13*time.Hour, func() { // before midnight UTC
		if _, err := e.Deps.Core.AnswerDecision(bg, e.Deps.Actor.ID, y.ID, core.Answer{Option: "Yes"}); err != nil {
			t.Fatal(err)
		}
	})
	page := e.Client().Get("/inbox").Body
	contains(t, page, "Nothing needs you.")
	answered := inboxGroup(page, "answered")
	contains(t, answered, "Answered today (1)", "Relay me?", "Relayed by claude: &#34;yes, go&#34;")
	lacks(t, answered, "Yesterday?")
}

// The address narrows the inbox: project and kind; an unknown kind is all.
func TestInboxFilters(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	workflowOn(t, e)
	oth := newProjectKey(t, e, "OTH", "Other")
	a := agent(t, e)
	tk := ticket(t, e, p, core.TicketInput{Title: "Build it"})
	vq := ask(t, e, a.ID, core.DecisionInput{ProjectID: p.ID, Kind: core.DecisionQuestion, Question: "VAQ question?"})
	oq := ask(t, e, a.ID, core.DecisionInput{ProjectID: oth.ID, Kind: core.DecisionQuestion, Question: "OTH question?"})
	ap := planApproval(t, e, a.ID, p, tk.ID)
	c := e.Client()
	page := c.Get("/inbox").Body
	contains(t, page, `href="/inbox?project=VAQ"`, `href="/inbox?project=OTH"`, `href="/inbox?kind=approval"`, `href="/inbox?kind=question"`, ">All<")
	for path, want := range map[string][]string{
		"/inbox?project=OTH":                {oq.ID},
		"/inbox?project=vaq":                {vq.ID, ap.ID},
		"/inbox?kind=approval":              {ap.ID},
		"/inbox?kind=question":              {vq.ID, oq.ID},
		"/inbox?kind=question&project=VAQ":  {vq.ID},
		"/inbox?kind=nope":                  {vq.ID, oq.ID, ap.ID},
		"/inbox?project=NOPE&kind=nonsense": {},
	} {
		decide := inboxGroup(c.Get(path).Body, "decide")
		if got := strings.Count(decide, "data-decision="); got != len(want) {
			t.Errorf("%s: %d cards, want %d", path, got, len(want))
		}
		for _, id := range want {
			if !strings.Contains(decide, id) {
				t.Errorf("%s lacks %s", path, id)
			}
		}
	}
}

func TestInboxEmpty(t *testing.T) {
	page := webtest.New(t).Client().Get("/inbox").Body
	contains(t, page, "<h1", "Inbox", "Nothing needs you.")
	lacks(t, page, `data-inbox-group="decide"`, `data-inbox-group="answered"`)
}

// Ruling T-R6: the sidebar's count is the open questions and approvals
// plus Review, read in one query, shown when above zero.
func TestInboxCount(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	cdb := &navCountingDB{DBTX: e.DB.R, name: "CountInbox"}
	e.Deps.R = gen.New(cdb)
	e.Rebuild()
	c := e.Client()
	lacks(t, sidebar(t, c.Get("/my").Body), "data-inbox-count")
	workflowOn(t, e)
	a := agent(t, e)
	ask(t, e, a.ID, core.DecisionInput{ProjectID: p.ID, Kind: core.DecisionQuestion, Question: "One?"})
	ask(t, e, a.ID, core.DecisionInput{ProjectID: p.ID, Kind: core.DecisionQuestion, Question: "Two?"})
	ticket(t, e, p, core.TicketInput{Title: "Review me", StatusID: statusIn(t, e, p, "In review")})
	before := cdb.count()
	contains(t, sidebar(t, c.Get("/my").Body), `data-inbox-count`, ">3<")
	if n := cdb.count() - before; n != 1 {
		t.Fatalf("CountInbox ran %d times, want 1", n)
	}
	before = cdb.count()
	c.Get("/p/VAQ/board/fragment")
	if n := cdb.count() - before; n != 0 {
		t.Fatalf("a fragment counted the inbox %d times", n)
	}
}

// A failed count keeps the projects already read and shows no count.
func TestInboxCountFailureKeepsProjects(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	ask(t, e, e.Deps.Actor.ID, core.DecisionInput{ProjectID: p.ID, Kind: core.DecisionQuestion, Question: "One?"})
	e.Deps.R = gen.New(&navCountingDB{DBTX: e.DB.R, name: "CountInbox", fail: true})
	e.Rebuild()
	side := sidebar(t, e.Client().Get("/my").Body)
	contains(t, side, `data-nav-project="VAQ"`)
	lacks(t, side, "data-inbox-count")
}

func TestDecisionLinkRedirects(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	d := ask(t, e, e.Deps.Actor.ID, core.DecisionInput{ProjectID: p.ID, Kind: core.DecisionQuestion, Question: "Here?"})
	r := e.Client().Get("/decisions/" + d.ID)
	if r.Status != http.StatusFound || r.Location != "/inbox#decision-"+d.ID {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	if r = e.Client().Get("/decisions/not-an-id"); r.Status != http.StatusFound || r.Location != "/inbox" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
}
