package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/markdown"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/web/flash"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

const (
	// lapseWindow is how far back Check reads lapsed claims (ruling T-R5).
	lapseWindow = 24 * time.Hour
	// rulingWindow is how far back Rulings agents made reads.
	rulingWindow = 7 * 24 * time.Hour
	inboxPath    = "/inbox"
)

// Inbox is /inbox (VAQ-172, ruling T-R5): the open questions and approvals
// to decide, the tickets in review, the claims that lapsed lately and the
// rulings agents made this week, across the projects not archived, then
// what was answered today. ?project=KEY and ?kind=approval|question narrow
// it (an unknown kind is all); a kind shows Decide alone.
func Inbox(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := inboxView(r.Context(), d, r.URL.Query())
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		render(w, r, d, http.StatusOK, templates.InboxPage(v))
	}
}

// inboxKinds are the kinds ?kind= takes.
var inboxKinds = []string{core.DecisionApproval, core.DecisionQuestion}

func inboxView(ctx context.Context, d Deps, q url.Values) (templates.InboxView, error) {
	var v templates.InboxView
	now := d.Now()
	project := strings.ToUpper(strings.TrimSpace(q.Get("project")))
	kind := q.Get("kind")
	if !slices.Contains(inboxKinds, kind) {
		kind = ""
	}
	decs, err := d.R.ListInboxDecisions(ctx)
	if err != nil {
		return v, err
	}
	review, err := d.R.ListInboxReview(ctx)
	if err != nil {
		return v, err
	}
	lapses, err := d.R.ListRecentLapses(ctx, timefmt.Format(now.Add(-lapseWindow)))
	if err != nil {
		return v, err
	}
	rulings, err := d.R.ListAgentRulings(ctx, timefmt.Format(now.Add(-rulingWindow)))
	if err != nil {
		return v, err
	}
	y, m, day := now.UTC().Date()
	answered, err := d.R.ListAnsweredSince(ctx, gen.ListAnsweredSinceParams{
		Owner: sql.NullString{String: d.Actor.ID, Valid: true},
		Since: sql.NullString{String: timefmt.Format(time.Date(y, m, day, 0, 0, 0, 0, time.UTC)), Valid: true},
	})
	if err != nil {
		return v, err
	}
	all := slices.Concat(decs, rulings, answered)
	cards, err := decisionCards(ctx, d, all, inboxPath)
	if err != nil {
		return v, err
	}
	byID := make(map[string]templates.DecisionCardView, len(cards))
	for _, c := range cards {
		byID[c.ID] = c
	}

	// The chips name every project with something waiting, before filters.
	waiting := map[string]bool{}
	for _, dec := range slices.Concat(decs, rulings) {
		waiting[byID[dec.ID].ProjectKey] = true
	}
	for _, t := range review {
		waiting[t.ProjectKey] = true
	}
	for _, l := range lapses {
		waiting[l.ProjectKey] = true
	}
	keys := make([]string, 0, len(waiting))
	for k := range waiting {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	href := func(p, k string) string {
		u := url.Values{}
		if p != "" {
			u.Set("project", p)
		}
		if k != "" {
			u.Set("kind", k)
		}
		if len(u) == 0 {
			return inboxPath
		}
		return inboxPath + "?" + u.Encode()
	}
	v.Self = href(project, kind)
	toggle := func(cur, val string) string {
		if cur == val {
			return ""
		}
		return val
	}
	v.Chips = append(v.Chips, templates.InboxChip{Label: "All", Href: inboxPath, On: project == "" && kind == ""})
	for _, k := range keys {
		v.Chips = append(v.Chips, templates.InboxChip{Label: k, Href: href(toggle(project, k), kind), On: project == k})
	}
	v.Chips = append(v.Chips,
		templates.InboxChip{Label: "Approvals", Href: href(project, toggle(kind, core.DecisionApproval)), On: kind == core.DecisionApproval},
		templates.InboxChip{Label: "Questions", Href: href(project, toggle(kind, core.DecisionQuestion)), On: kind == core.DecisionQuestion})

	in := func(key string) bool { return project == "" || key == project }
	for _, dec := range decs {
		if c := byID[dec.ID]; in(c.ProjectKey) && (kind == "" || dec.Kind == kind) {
			v.Decide = append(v.Decide, c)
		}
	}
	for _, dec := range answered {
		c := byID[dec.ID]
		if !in(c.ProjectKey) || (kind != "" && dec.Kind != kind) {
			continue
		}
		at, _ := timefmt.Parse(dec.AnsweredAt.String)
		v.Answered = append(v.Answered, templates.InboxAnswered{ID: dec.ID, ProjectKey: c.ProjectKey,
			Question: firstLine(dec.Question), Answer: dec.Answer, How: c.Answered, Ago: timefmt.Ago(at, now, nil)})
	}
	if kind != "" {
		return v, nil
	}
	for _, t := range review {
		if !in(t.ProjectKey) {
			continue
		}
		at, _ := timefmt.Parse(t.StatusChangedAt)
		v.Review = append(v.Review, templates.InboxReviewRow{Key: ticketKeyOf(t.ProjectKey, t.Number), Title: t.Title,
			MovedBy: t.MovedBy, Age: timefmt.Ago(at, now, nil), Priority: t.Priority})
	}
	for _, l := range lapses {
		if !in(l.ProjectKey) {
			continue
		}
		at, _ := timefmt.Parse(l.At)
		v.Check = append(v.Check, templates.InboxLapse{Seq: l.Seq, Agent: l.AgentName,
			Key: ticketKeyOf(l.ProjectKey, l.Number), Title: l.Title, Ago: timefmt.Ago(at, now, nil)})
	}
	for _, dec := range rulings {
		c := byID[dec.ID]
		if !in(c.ProjectKey) {
			continue
		}
		v.Rulings = append(v.Rulings, templates.InboxRuling{Card: c, Question: firstLine(dec.Question), Ruled: dec.Answer})
	}
	return v, nil
}

func ticketKeyOf(project string, n int64) string {
	return core.TicketKey{Project: project, Number: n}.String()
}

// firstLine is a decision's question as one line, for a folded row.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

// cardData is what cards need beyond their decisions, read in a batch.
type cardData struct {
	projects map[string]string    // id → key
	actors   map[string]gen.Actor // id → actor
	tickets  map[string][]templates.TicketRef
}

// readCardData reads the projects, people and tickets decs name, in three
// queries however many.
func readCardData(ctx context.Context, d Deps, decs []gen.Decision) (cardData, error) {
	cd := cardData{projects: map[string]string{}, actors: map[string]gen.Actor{}, tickets: map[string][]templates.TicketRef{}}
	if len(decs) == 0 {
		return cd, nil
	}
	var ids, projectIDs, actorIDs []string
	for _, dec := range decs {
		ids = append(ids, dec.ID)
		projectIDs = append(projectIDs, dec.ProjectID)
		actorIDs = append(actorIDs, dec.AskedBy)
		for _, n := range []sql.NullString{dec.AnsweredBy, dec.RelayedBy} {
			if n.Valid {
				actorIDs = append(actorIDs, n.String)
			}
		}
	}
	ps, err := d.R.ListProjectKeysByIDs(ctx, distinct(projectIDs))
	if err != nil {
		return cd, err
	}
	for _, p := range ps {
		cd.projects[p.ID] = p.Key
	}
	as, err := d.R.ListActorsByIDs(ctx, distinct(actorIDs))
	if err != nil {
		return cd, err
	}
	for _, a := range as {
		cd.actors[a.ID] = a
	}
	ks, err := d.R.ListDecisionTicketKeys(ctx, distinct(ids))
	if err != nil {
		return cd, err
	}
	for _, k := range ks {
		cd.tickets[k.DecisionID] = append(cd.tickets[k.DecisionID], templates.TicketRef{Key: ticketKeyOf(k.ProjectKey, k.Number), Title: k.Title})
	}
	return cd, nil
}

// decisionCards is each of decs as a card, in order, read in a fixed
// number of queries.
func decisionCards(ctx context.Context, d Deps, decs []gen.Decision, back string) ([]templates.DecisionCardView, error) {
	cd, err := readCardData(ctx, d, decs)
	if err != nil {
		return nil, err
	}
	var plans []string
	for _, dec := range decs {
		if dec.Kind == core.DecisionApproval && dec.Subject == "plan" {
			plans = append(plans, dec.ID)
		}
	}
	asked, err := decisionsAsked(ctx, d, plans)
	if err != nil {
		return nil, err
	}
	out := make([]templates.DecisionCardView, 0, len(decs))
	for _, dec := range decs {
		c, err := buildCard(dec, asked[dec.ID], cd, back, d.Now())
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// decisionCard is one decision as a card the owner answers in place, with
// what a plan approval asked (decisionsAsked); back is where its forms
// return without JavaScript.
func decisionCard(ctx context.Context, d Deps, dec gen.Decision, asked []api.DecisionAsked, back string) (templates.DecisionCardView, error) {
	cd, err := readCardData(ctx, d, []gen.Decision{dec})
	if err != nil {
		return templates.DecisionCardView{}, err
	}
	return buildCard(dec, asked, cd, back, d.Now())
}

func buildCard(dec gen.Decision, asked []api.DecisionAsked, cd cardData, back string, now time.Time) (templates.DecisionCardView, error) {
	key, ok := cd.projects[dec.ProjectID]
	if !ok {
		return templates.DecisionCardView{}, fmt.Errorf("decision %s: no project %s: %w", dec.ID, dec.ProjectID, sql.ErrNoRows)
	}
	name := func(id string) string {
		if a, ok := cd.actors[id]; ok {
			return core.ActorName(a)
		}
		return ""
	}
	at, _ := timefmt.Parse(dec.AskedAt)
	c := templates.DecisionCardView{
		ID: dec.ID, Kind: dec.Kind, Subject: dec.Subject, ProjectKey: key, AskedBy: name(dec.AskedBy),
		Age: timefmt.Ago(at, now, nil), Question: markdown.Render(dec.Question), Tickets: cd.tickets[dec.ID], Back: back,
	}
	var opts []core.Option
	if err := json.Unmarshal([]byte(dec.Options), &opts); err != nil {
		return c, fmt.Errorf("decision %s options: %w", dec.ID, err)
	}
	for _, o := range opts {
		co := templates.CardOption{Label: o.Label, Tradeoff: o.Tradeoff, Recommended: dec.Recommended != "" && strings.EqualFold(o.Label, dec.Recommended)}
		if co.Recommended {
			c.Options = append([]templates.CardOption{co}, c.Options...)
		} else {
			c.Options = append(c.Options, co)
		}
	}
	if dec.Kind == core.DecisionApproval && dec.Subject == "plan" {
		c.Covers = coversLine(asked)
		for _, a := range asked {
			if a.PlanRevision > 0 {
				c.PlanKey = a.Ticket
				break
			}
		}
	}
	switch {
	case dec.State != "answered":
	case dec.Kind == core.DecisionRuling && dec.AnsweredBy.String == dec.AskedBy:
		c.Answered = c.AskedBy + " ruled " + dec.Answer + "."
	case dec.RelayedBy.Valid:
		c.Answered = "Relayed by " + name(dec.RelayedBy.String) + ": \"" + dec.RelayedWords + "\""
	default:
		c.Answered = "Answered directly by you"
	}
	return c, nil
}

// coversLine is what approving a plan approval covers, as asked: "Covers
// VAQ-1 (plan rev 2) and 3 sub-tickets as asked", a clause per ticket.
func coversLine(asked []api.DecisionAsked) string {
	if len(asked) == 0 {
		return ""
	}
	parts := make([]string, len(asked))
	for i, a := range asked {
		p := a.Ticket + " (no plan)"
		if a.PlanRevision > 0 {
			p = a.Ticket + " (plan rev " + strconv.FormatInt(a.PlanRevision, 10) + ")"
		}
		switch n := len(a.Children); n {
		case 0:
		case 1:
			p += " and 1 sub-ticket"
		default:
			p += " and " + strconv.Itoa(n) + " sub-tickets"
		}
		parts[i] = p
	}
	return "Covers " + strings.Join(parts, ", ") + " as asked"
}

// localBack is the form's back when it is a path on this site, else the
// inbox. A control character refuses it: a TAB makes "/\t/evil.example" a
// protocol-relative address to a browser.
func localBack(r *http.Request) string {
	b := r.PostFormValue("back")
	if !strings.HasPrefix(b, "/") || strings.HasPrefix(b, "//") || strings.ContainsRune(b, '\\') || slices.ContainsFunc([]rune(b), func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return inboxPath
	}
	return b
}

// InboxAnswer is POST /inbox/{id}/answer: the owner's direct answer, an
// option (or written text) with an optional note. With htmx a taken
// answer is an empty card, which leaves the list, and the page's live
// parts refresh (HX-Trigger vq:refresh, which app.js runs through its busy
// rule, so a note typed in another card stays); a refusal is the card
// again with core's sentence (Review Focus 2). Without htmx it returns to
// back with a toast.
func InboxAnswer(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		note := strings.TrimSpace(r.PostFormValue("note"))
		_, err := d.Core.AnswerDecision(r.Context(), d.Actor.ID, chi.URLParam(r, "id"), core.Answer{
			Option: r.PostFormValue("option"), Text: r.PostFormValue("text"), Note: note,
		})
		cardDone(w, r, d, err, "Answered.", note)
	}
}

// InboxWithdraw is POST /inbox/{id}/withdraw: the owner withdraws an open
// question or approval, answered as InboxAnswer answers.
func InboxWithdraw(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := d.Core.WithdrawDecision(r.Context(), d.Actor.ID, chi.URLParam(r, "id"))
		cardDone(w, r, d, err, "Withdrawn.", "")
	}
}

// cardDone answers a card's form after its core call returned err.
func cardDone(w http.ResponseWriter, r *http.Request, d Deps, err error, done, note string) {
	back := localBack(r)
	hx := r.Header.Get("HX-Request") == "true"
	msg, refused := refusal(err)
	if err != nil && !refused {
		serverError(w, r, d, err)
		return
	}
	if !hx {
		if refused {
			redirectWithFlash(w, r, flash.Error, msg, back)
		} else {
			redirectWithFlash(w, r, flash.Success, done, back)
		}
		return
	}
	if !refused {
		w.Header().Set("HX-Trigger", "vq:refresh")
		emptyCard(w)
		return
	}
	ctx := r.Context()
	dec, err := d.R.GetDecision(ctx, chi.URLParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) { // gone: the card leaves
		emptyCard(w)
		return
	}
	if err != nil {
		serverError(w, r, d, err)
		return
	}
	var asked []api.DecisionAsked
	if dec.Kind == core.DecisionApproval && dec.Subject == "plan" {
		m, err := decisionsAsked(ctx, d, []string{dec.ID})
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		asked = m[dec.ID]
	}
	c, err := decisionCard(ctx, d, dec, asked, back)
	if err != nil {
		serverError(w, r, d, err)
		return
	}
	c.Error, c.Note = msg, note
	renderPart(w, r, d, http.StatusOK, templates.DecisionCard(c))
}

// emptyCard answers a card that leaves the list.
func emptyCard(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
}

// refusal is core's refusal of err as one sentence for the person, and
// whether err was one (a *core.Invalid or core.ErrNotFound).
func refusal(err error) (string, bool) {
	var inv *core.Invalid
	switch {
	case errors.As(err, &inv):
		keys := make([]string, 0, len(inv.Fields))
		for k := range inv.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		msgs := make([]string, len(keys))
		for i, k := range keys {
			msgs[i] = inv.Fields[k]
		}
		return strings.Join(msgs, " "), true
	case errors.Is(err, core.ErrNotFound):
		return "That no longer exists.", true
	}
	return "", false
}

// DecisionLink is GET /decisions/{id}: the inbox at that decision's card
// (the CLI's link).
func DecisionLink(_ Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !idgen.Valid(id) {
			http.Redirect(w, r, inboxPath, http.StatusFound)
			return
		}
		http.Redirect(w, r, inboxPath+"#decision-"+id, http.StatusFound)
	}
}

// TicketPlan is GET /t/{ticket}/plan: the ticket's plan as its page draws
// it, for a card's Show plan.
func TicketPlan(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, _, ok := ticketFromURL(w, r, d)
		if !ok {
			return
		}
		pv, err := planView(r, d, t.ID, d.Now())
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		renderPart(w, r, d, http.StatusOK, templates.PlanFragment(pv))
	}
}

// ticketDecisionCards is every open decision naming the ticket in its
// project now (a move leaves earlier ones in the old project, VAQ-195),
// newest first, as the inbox's cards with the ticket page as their back,
// and extra (the open plan approval readiness shows, which may name its
// epic instead) when it isn't among them.
func ticketDecisionCards(ctx context.Context, d Deps, t gen.Ticket, extra *gen.Decision, key string) ([]templates.DecisionCardView, error) {
	decs, err := d.R.ListDecisions(ctx, gen.ListDecisionsParams{State: "open", TicketID: t.ID, ProjectID: t.ProjectID, Lim: 50})
	if err != nil {
		return nil, err
	}
	if extra != nil && !slices.ContainsFunc(decs, func(x gen.Decision) bool { return x.ID == extra.ID }) {
		decs = append(decs, *extra)
	}
	return decisionCards(ctx, d, decs, "/t/"+key)
}
