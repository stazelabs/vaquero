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
// the workflow off or for anything but a todo. It carries the open plan
// approval the owner can answer, the ticket's own or its epic's, whichever
// readiness says to ask on.
func readinessView(r *http.Request, d Deps, t gen.Ticket, p gen.Project, status gen.Status, key string) (*templates.ReadinessView, error) {
	if p.Workflow != 1 || status.Category != "todo" {
		return nil, nil
	}
	ctx := r.Context()
	rd, err := readiness.Check(ctx, d.R, d.files(), t)
	if err != nil {
		return nil, err
	}
	v := &templates.ReadinessView{Ready: rd.Ready, Ask: rd.Action == readiness.ActionAsk || rd.Action == readiness.ActionAskAgain}
	if len(rd.Reasons) > 0 {
		v.Reason = rd.Reasons[0]
	}
	target, targetKey := t, key
	if rd.Epic != "" {
		k, ok := core.ParseTicketKey(rd.Epic)
		if !ok {
			return v, nil
		}
		et, _, err := d.Core.ResolveTicket(ctx, d.R, k)
		if err != nil {
			return nil, err
		}
		target, targetKey = et, rd.Epic
	}
	v.AskKey = targetKey
	open, err := openPlanApproval(r, d, target.ID)
	if err != nil {
		return nil, err
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

// openPlanApproval is the newest open plan approval naming the ticket, or
// nil.
func openPlanApproval(r *http.Request, d Deps, ticketID string) (*gen.Decision, error) {
	ds, err := d.R.ListDecisions(r.Context(), gen.ListDecisionsParams{
		State: "open", Kind: core.DecisionApproval, TicketID: ticketID, Lim: 20,
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

// ApprovalAsk is POST /t/{ticket}/approval/ask: asks a plan approval of
// the ticket, or of its epic when readiness says to ask there, as the
// owner. One already open is shown, not asked again.
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
		rd, err := readiness.Check(ctx, d.R, d.files(), t)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		target, targetKey := t, key
		if rd.Epic != "" {
			k, ok := core.ParseTicketKey(rd.Epic)
			if ok {
				if et, _, err := d.Core.ResolveTicket(ctx, d.R, k); err == nil {
					target, targetKey = et, rd.Epic
				}
			}
		}
		if open, err := openPlanApproval(r, d, target.ID); err != nil {
			serverError(w, r, d, err)
			return
		} else if open != nil {
			redirectWithFlash(w, r, flash.Success, "A plan approval for "+targetKey+" is already waiting; answer it below.", back)
			return
		}
		_, err = d.Core.AskDecision(ctx, d.Actor.ID, core.DecisionInput{
			ProjectID: p.ID, TicketIDs: []string{target.ID}, Kind: core.DecisionApproval, Subject: "plan",
			Question: "Approve the plan for " + targetKey + " (" + target.Title + ")?",
		})
		if err != nil {
			approvalFailed(w, r, d, err, back)
			return
		}
		redirectWithFlash(w, r, flash.Success, "Asked for approval of the plan for "+targetKey+".", back)
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
