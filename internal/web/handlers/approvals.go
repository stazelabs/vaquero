package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/readiness"
	"github.com/stazelabs/vaquero/internal/web/flash"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// readinessView is the ticket page's readiness block (VAQ-160): nil with
// the workflow off. For a todo it says whether the ticket is ready, or the
// one reason, with the Ask button when readiness says to ask; for any
// status it carries the open plan approval the owner can answer (ruling
// R-Q15), the ticket's own or, for a todo, its epic's when readiness says
// to ask there. A ticket that isn't a todo with nothing open has no block.
func readinessView(r *http.Request, d Deps, t gen.Ticket, p gen.Project, status gen.Status, key string) (*templates.ReadinessView, error) {
	if p.Workflow != 1 {
		return nil, nil
	}
	todo := status.Category == "todo"
	var rd readiness.Readiness // readiness says nothing of a ticket that isn't a todo
	target, gone := t, false
	if todo {
		var err error
		rd, target, _, err = approvalTarget(r, d, t, key)
		if gone = errors.Is(err, errEpicGone); err != nil && !gone {
			return nil, err
		}
	}
	v := &templates.ReadinessView{Todo: todo, Ready: rd.Ready, Ask: asks(rd) && !gone}
	if len(rd.Reasons) > 0 {
		v.Reason = rd.Reasons[0]
	}
	if gone {
		return v, nil
	}
	open, err := openPlanApproval(r, d, target)
	if err != nil {
		return nil, err
	}
	if open == nil && !todo {
		return nil, nil
	}
	if open != nil {
		var opts []core.Option
		_ = json.Unmarshal([]byte(open.Options), &opts)
		a := &templates.ApprovalView{ID: open.ID, Question: open.Question, Ticket: key}
		for _, o := range opts {
			a.Options = append(a.Options, templates.Option{Value: o.Label, Label: o.Label})
		}
		v.Open = a
	}
	return v, nil
}

// asks reports whether readiness's action is to ask for a plan approval.
func asks(rd readiness.Readiness) bool {
	return rd.Action == readiness.ActionAsk || rd.Action == readiness.ActionAskAgain
}

// errEpicGone is an epic readiness names that no longer resolves.
var errEpicGone = errors.New("the epic to ask on is gone")

// approvalTarget is t's readiness and the ticket a plan approval is asked
// of: t itself, or the epic readiness names, with its key. An epic that
// doesn't resolve is errEpicGone, never a silent ask on t.
func approvalTarget(r *http.Request, d Deps, t gen.Ticket, key string) (readiness.Readiness, gen.Ticket, string, error) {
	ctx := r.Context()
	rd, err := readiness.Check(ctx, d.R, d.files(), t)
	if err != nil {
		return rd, t, key, err
	}
	if rd.Epic == "" {
		return rd, t, key, nil
	}
	k, ok := core.ParseTicketKey(rd.Epic)
	if !ok {
		return rd, t, key, errEpicGone
	}
	et, _, err := d.Core.ResolveTicket(ctx, d.R, k)
	if errors.Is(err, core.ErrNotFound) {
		return rd, t, key, errEpicGone
	}
	if err != nil {
		return rd, t, key, err
	}
	return rd, et, rd.Epic, nil
}

// openPlanApproval is the newest open plan approval naming the ticket in
// its current project, or nil. A move leaves its decisions in the old
// project (VAQ-195); they can't count here and aren't shown.
func openPlanApproval(r *http.Request, d Deps, ticket gen.Ticket) (*gen.Decision, error) {
	ds, err := d.R.ListDecisions(r.Context(), gen.ListDecisionsParams{
		State: "open", Kind: core.DecisionApproval, TicketID: ticket.ID, ProjectID: ticket.ProjectID, Lim: 20,
	})
	if err != nil {
		return nil, err
	}
	for _, x := range ds {
		if x.Subject == "plan" {
			return &x, nil
		}
	}
	return nil, nil
}

// approvalQuestion is the question the ticket page asks.
func approvalQuestion(key string, t gen.Ticket) string {
	return "Approve the plan for " + key + " (" + t.Title + ")?"
}

// ApprovalAsk is POST /t/{ticket}/approval/ask: asks a plan approval of
// the ticket, or of its epic when readiness says to ask there, as the
// owner, only when readiness says to ask (or ask again). One already open
// is shown, not asked again.
func ApprovalAsk(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := ticketFromURL(w, r, d)
		if !ok {
			return
		}
		ctx := r.Context()
		key := ticketKey(p, t.Number)
		back := "/t/" + key
		if p.Workflow != 1 {
			redirectWithFlash(w, r, flash.Error, "The agent workflow isn't on for "+p.Key+".", back)
			return
		}
		rd, target, targetKey, err := approvalTarget(r, d, t, key)
		switch {
		case errors.Is(err, errEpicGone):
			redirectWithFlash(w, r, flash.Error, "The epic to ask on, "+rd.Epic+", no longer exists.", back)
			return
		case err != nil:
			serverError(w, r, d, err)
			return
		case !asks(rd):
			redirectWithFlash(w, r, flash.Error, key+" has nothing to ask for.", back)
			return
		}
		if open, err := openPlanApproval(r, d, target); err != nil {
			serverError(w, r, d, err)
			return
		} else if open != nil {
			redirectWithFlash(w, r, flash.Success, "A plan approval for "+targetKey+" is already waiting; answer it below, or use Ask again.", back)
			return
		}
		_, err = d.Core.AskDecision(ctx, d.Actor.ID, core.DecisionInput{
			ProjectID: p.ID, TicketIDs: []string{target.ID}, Kind: core.DecisionApproval, Subject: "plan",
			Question: approvalQuestion(targetKey, target),
		})
		if err != nil {
			approvalFailed(w, r, d, err, back)
			return
		}
		redirectWithFlash(w, r, flash.Success, "Asked for approval of the plan for "+targetKey+".", back)
	}
}

// ApprovalAskAgain is POST /t/{ticket}/approval/ask-again (ruling R-Q14):
// withdraws the open plan approval the ticket page shows, as the owner,
// and asks a new one of the same tickets about them as they are now. The
// ticket page shows it beside any open plan approval, whatever the status.
func ApprovalAskAgain(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := ticketFromURL(w, r, d)
		if !ok {
			return
		}
		ctx := r.Context()
		key := ticketKey(p, t.Number)
		back := "/t/" + key
		if p.Workflow != 1 {
			redirectWithFlash(w, r, flash.Error, "The agent workflow isn't on for "+p.Key+".", back)
			return
		}
		target, targetKey := t, key
		if st, err := d.R.GetStatus(ctx, t.StatusID); err != nil {
			serverError(w, r, d, err)
			return
		} else if st.Category == "todo" { // as the page: a todo's epic when readiness says so
			_, et, ek, err := approvalTarget(r, d, t, key)
			if err != nil && !errors.Is(err, errEpicGone) {
				serverError(w, r, d, err)
				return
			}
			if err == nil {
				target, targetKey = et, ek
			}
		}
		open, err := openPlanApproval(r, d, target)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		if open == nil {
			redirectWithFlash(w, r, flash.Error, "No plan approval for "+targetKey+" is waiting.", back)
			return
		}
		ids, err := d.R.ListDecisionTicketIDs(ctx, open.ID)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		var opts []core.Option
		_ = json.Unmarshal([]byte(open.Options), &opts)
		in := core.DecisionInput{
			ProjectID: target.ProjectID, TicketIDs: ids, Kind: core.DecisionApproval, Subject: "plan",
			Question: open.Question, Options: opts, Recommended: open.Recommended, Recommendation: open.Recommendation, Why: open.Why,
		}
		if len(ids) == 1 && ids[0] == target.ID {
			in.Question = approvalQuestion(targetKey, target) // with the title as it is now
		}
		if err := d.Core.WithdrawDecision(ctx, d.Actor.ID, open.ID); err != nil {
			approvalFailed(w, r, d, err, back)
			return
		}
		if _, err := d.Core.AskDecision(ctx, d.Actor.ID, in); err != nil {
			approvalFailed(w, r, d, err, back)
			return
		}
		redirectWithFlash(w, r, flash.Success, "Asked again for approval of the plan for "+targetKey+".", back)
	}
}

// DecisionAnswer is POST /decisions/{id}/answer: the owner's direct answer
// (an option and an optional note). The form's ticket field says which
// ticket page to return to.
func DecisionAnswer(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		back := "/"
		if k, ok := core.ParseTicketKey(r.PostFormValue("ticket")); ok {
			back = "/t/" + k.String()
		}
		_, err := d.Core.AnswerDecision(r.Context(), d.Actor.ID, chi.URLParam(r, "id"), core.Answer{
			Option: r.PostFormValue("option"), Note: strings.TrimSpace(r.PostFormValue("note")),
		})
		var inv *core.Invalid
		if errors.As(err, &inv) && strings.HasSuffix(inv.Fields["answer"], "; ask again.") {
			// Stale since asked: the page offers Ask again beside it.
			redirectWithFlash(w, r, flash.Error, inv.Fields["answer"]+" Use Ask again.", back)
			return
		}
		if err != nil {
			approvalFailed(w, r, d, err, back)
			return
		}
		redirectWithFlash(w, r, flash.Success, "Answered.", back)
	}
}

// approvalFailed answers a refused ask or answer as an error toast on the
// page the form came from.
func approvalFailed(w http.ResponseWriter, r *http.Request, d Deps, err error, back string) {
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
		redirectWithFlash(w, r, flash.Error, strings.Join(msgs, " "), back)
	case errors.Is(err, core.ErrNotFound):
		redirectWithFlash(w, r, flash.Error, "That no longer exists.", back)
	default:
		serverError(w, r, d, err)
	}
}
