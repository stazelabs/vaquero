package handlers

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/units"
)

// decisionRequest is POST /api/v1/decisions' body. Answer is for a ruling
// only, which is recorded already answered by the caller.
type decisionRequest struct {
	Project        string               `json:"project"`
	Tickets        []string             `json:"tickets"`
	Run            string               `json:"run"`
	Kind           string               `json:"kind"`
	Subject        string               `json:"subject"`
	Question       string               `json:"question"`
	Options        []api.DecisionOption `json:"options"`
	Recommendation string               `json:"recommendation"`
	Why            string               `json:"why"`
	Answer         *rulingAnswerRequest `json:"answer"`
}

type rulingAnswerRequest struct {
	Option string `json:"option"`
	Text   string `json:"text"`
	Note   string `json:"note"`
}

// projectByKey resolves a project named in a body or query; an unknown
// key is a field message.
func projectByKey(ctx context.Context, d Deps, n *names, field, key string) gen.Project {
	k := strings.ToUpper(strings.TrimSpace(key))
	if k == "" {
		n.fields[field] = "Name the project, by key."
		return gen.Project{}
	}
	p, err := d.R.GetProjectByKey(ctx, k)
	if errors.Is(err, sql.ErrNoRows) {
		n.fields[field] = "No project " + units.Clip(k, 80) + "."
		return p
	}
	n.lookup(err)
	return p
}

// APIDecisionCreate is POST /api/v1/decisions: asks a question or an
// approval as the caller, or records the caller's ruling with its answer.
// Answers 201 with the decision.
func APIDecisionCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in decisionRequest
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		ctx := r.Context()
		n := &names{ctx: ctx, d: d, fields: map[string]string{}}
		p := projectByKey(ctx, d, n, "project", in.Project)
		n.p = p
		var ids []string
		for _, key := range in.Tickets {
			if id := n.ticket("tickets", key); id != "" {
				ids = append(ids, id)
			}
		}
		if in.Answer != nil && strings.TrimSpace(in.Kind) != core.DecisionRuling {
			n.fields["answer"] = "Only a ruling is recorded with its answer; answer a question or an approval through /decisions/{id}/answer."
		}
		if n.fail(w, r) {
			return
		}
		opts := make([]core.Option, len(in.Options))
		for i, o := range in.Options {
			opts[i] = core.Option{Label: o.Label, Tradeoff: o.Tradeoff}
		}
		di := core.DecisionInput{
			ProjectID: p.ID, TicketIDs: ids, RunID: in.Run, Kind: strings.TrimSpace(in.Kind), Subject: in.Subject,
			Question: in.Question, Options: opts, Recommendation: in.Recommendation, Why: in.Why,
		}
		by := apiActor(r).ID
		var dec gen.Decision
		var err error
		if di.Kind == core.DecisionRuling {
			var a core.Answer
			if in.Answer != nil {
				a = core.Answer{Option: in.Answer.Option, Text: in.Answer.Text, Note: in.Answer.Note}
			}
			dec, err = d.Core.RecordRuling(ctx, by, di, a)
		} else {
			dec, err = d.Core.AskDecision(ctx, by, di)
		}
		if err != nil {
			apiFail(w, r, d, api.FromCore(err, nil), err)
			return
		}
		writeDecision(w, r, d, dec, http.StatusCreated)
	}
}

// APIDecisions is GET /api/v1/decisions: newest first, narrowed by
// project, state, kind and ticket (a key); an unknown one is a 422.
func APIDecisions(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		q := r.URL.Query()
		n := &names{ctx: ctx, d: d, fields: map[string]string{}}
		arg := gen.ListDecisionsParams{Lim: limitParam(r, 100, maxAPIList)}
		if key := q.Get("project"); key != "" {
			if p := projectByKey(ctx, d, n, "project", key); p.ID != "" {
				arg.ProjectID = p.ID
			}
		}
		if s := q.Get("state"); s != "" {
			if !slices.Contains(core.DecisionStates(), s) {
				n.fields["state"] = "No state " + units.Clip(s, 80) + ". Use " + orList(core.DecisionStates()) + "."
			}
			arg.State = s
		}
		if k := q.Get("kind"); k != "" {
			if !slices.Contains(core.DecisionKinds(), k) {
				n.fields["kind"] = "No kind " + units.Clip(k, 80) + ". Use " + orList(core.DecisionKinds()) + "."
			}
			arg.Kind = k
		}
		if key := q.Get("ticket"); key != "" {
			if id := n.ticket("ticket", key); id != "" {
				arg.TicketID = id
			}
		}
		if n.fail(w, r) {
			return
		}
		rows, err := d.R.ListDecisions(ctx, arg)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		out := make([]api.Decision, 0, len(rows))
		for _, dec := range rows {
			dto, err := decisionDTO(ctx, d, dec)
			if err != nil {
				apiFail(w, r, d, api.Internal(), err)
				return
			}
			out = append(out, dto)
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// apiDecision loads the {id} decision.
func apiDecision(w http.ResponseWriter, r *http.Request, d Deps) (gen.Decision, bool) {
	id := chi.URLParam(r, "id")
	dec, err := d.R.GetDecision(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		api.WriteFailure(w, api.NotFound("No decision "+units.Clip(id, 80)+"."))
		return dec, false
	}
	if err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return dec, false
	}
	return dec, true
}

// APIDecision is GET /api/v1/decisions/{id}.
func APIDecision(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if dec, ok := apiDecision(w, r, d); ok {
			writeDecision(w, r, d, dec, http.StatusOK)
		}
	}
}

// APIDecisionAnswer is POST /api/v1/decisions/{id}/answer {option | text,
// note, words}. Only the owner answers. A request acting as an agent (the
// owner's token with Vaquero-Agent) relays the owner's answer, and must
// quote it in words; without the header the owner answers directly.
func APIDecisionAnswer(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dec, ok := apiDecision(w, r, d)
		if !ok {
			return
		}
		var in struct {
			Option string `json:"option"`
			Text   string `json:"text"`
			Note   string `json:"note"`
			Words  string `json:"words"`
		}
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		ctx := r.Context()
		a := core.Answer{Option: in.Option, Text: in.Text, Note: in.Note, Words: in.Words}
		actor := apiActor(r)
		owner := actor.ID
		if actor.Kind == "agent" {
			o, err := d.R.GetActorByKindName(ctx, gen.GetActorByKindNameParams{Kind: "human", Name: core.OwnerName})
			if err != nil {
				apiFail(w, r, d, api.Internal(), err)
				return
			}
			owner, a.RelayedBy = o.ID, actor.ID
		}
		answered, err := d.Core.AnswerDecision(ctx, owner, dec.ID, a)
		if err != nil {
			apiFail(w, r, d, api.FromCore(err, nil), err)
			return
		}
		writeDecision(w, r, d, answered, http.StatusOK)
	}
}

// APIDecisionWithdraw is POST /api/v1/decisions/{id}/withdraw: by who
// asked, or the owner, while the decision is open.
func APIDecisionWithdraw(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dec, ok := apiDecision(w, r, d)
		if !ok {
			return
		}
		if err := d.Core.WithdrawDecision(r.Context(), apiActor(r).ID, dec.ID); err != nil {
			apiFail(w, r, d, api.FromCore(err, nil), err)
			return
		}
		if dec, ok = apiDecision(w, r, d); ok {
			writeDecision(w, r, d, dec, http.StatusOK)
		}
	}
}

func writeDecision(w http.ResponseWriter, r *http.Request, d Deps, dec gen.Decision, status int) {
	out, err := decisionDTO(r.Context(), d, dec)
	if err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return
	}
	api.WriteJSON(w, status, out)
}

// decisionDTO is dec as the API answers it, read through the reader pool.
func decisionDTO(ctx context.Context, d Deps, dec gen.Decision) (api.Decision, error) {
	p, err := d.R.GetProject(ctx, dec.ProjectID)
	if err != nil {
		return api.Decision{}, err
	}
	actorNames := map[string]string{}
	name := func(id string) (string, error) {
		if id == "" {
			return "", nil
		}
		if n, ok := actorNames[id]; ok {
			return n, nil
		}
		a, err := d.R.GetActor(ctx, id)
		if err != nil {
			return "", err
		}
		actorNames[id] = a.Name
		return a.Name, nil
	}
	out := api.Decision{
		ID: dec.ID, Project: p.Key, Tickets: []string{}, Run: dec.RunID.String, Kind: dec.Kind, Subject: dec.Subject,
		Question: dec.Question, Options: []api.DecisionOption{}, Recommendation: dec.Recommendation, Why: dec.Why,
		State: dec.State, Answer: dec.Answer, Note: dec.AnswerNote, AnsweredAt: dec.AnsweredAt.String,
		Words: dec.RelayedWords, AskedAt: dec.AskedAt,
	}
	if err := json.Unmarshal([]byte(dec.Options), &out.Options); err != nil {
		return api.Decision{}, err
	}
	if dec.Kind == core.DecisionApproval {
		for _, o := range out.Options {
			if strings.EqualFold(o.Label, core.ApproveLabel) {
				out.Approves = o.Label
			}
		}
	}
	for _, s := range []struct {
		into *string
		id   string
	}{{&out.AskedBy, dec.AskedBy}, {&out.AnsweredBy, dec.AnsweredBy.String}, {&out.RelayedBy, dec.RelayedBy.String}} {
		if *s.into, err = name(s.id); err != nil {
			return api.Decision{}, err
		}
	}
	// An answer by the owner is direct or relayed; a ruling an agent made
	// is its own call, and is neither until the owner overrules it.
	if dec.AnsweredBy.Valid {
		a, err := d.R.GetActor(ctx, dec.AnsweredBy.String)
		if err != nil {
			return api.Decision{}, err
		}
		if core.IsOwner(a) {
			out.Answered = "direct"
			if dec.RelayedBy.Valid {
				out.Answered = "relayed"
			}
		}
	}
	keys, err := d.R.ListDecisionTicketKeys(ctx, dec.ID)
	if err != nil {
		return api.Decision{}, err
	}
	for _, k := range keys {
		out.Tickets = append(out.Tickets, core.TicketKey{Project: k.ProjectKey, Number: k.Number}.String())
	}
	aps, err := d.R.ListDecisionApprovals(ctx, dec.ID)
	if err != nil {
		return api.Decision{}, err
	}
	for _, a := range aps {
		out.Approvals = append(out.Approvals, api.DecisionApproval{
			Ticket: core.TicketKey{Project: a.ProjectKey, Number: a.Number}.String(), RequirementHash: a.RequirementHash, PlanRevision: a.PlanRevision, At: a.At,
		})
	}
	if dec.Kind == core.DecisionApproval && dec.Subject == "plan" {
		if out.Asked, err = decisionAsked(ctx, d, dec.ID); err != nil {
			return api.Decision{}, err
		}
	}
	return out, nil
}

// decisionAsked is what plan approval decisionID asked about for each of
// its tickets, in two reads: the pins, then the keys and titles they name.
func decisionAsked(ctx context.Context, d Deps, decisionID string) ([]api.DecisionAsked, error) {
	pins, err := d.R.ListDecisionTicketPins(ctx, decisionID)
	if err != nil {
		return nil, err
	}
	children := make([][]core.PinnedChild, len(pins))
	var ids []string
	for i, p := range pins {
		if err := json.Unmarshal([]byte(p.Children), &children[i]); err != nil {
			return nil, err
		}
		if p.ParentID != "" {
			ids = append(ids, p.ParentID)
		}
		for _, c := range children[i] {
			ids = append(ids, c.ID)
		}
	}
	type brief struct {
		item   api.DecisionItem
		number int64
	}
	briefs := map[string]brief{}
	if len(ids) > 0 {
		rows, err := d.R.ListTicketBriefs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			briefs[r.ID] = brief{api.DecisionItem{Key: core.TicketKey{Project: r.ProjectKey, Number: r.Number}.String(), Title: r.Title}, r.Number}
		}
	}
	out := []api.DecisionAsked{}
	for i, p := range pins {
		a := api.DecisionAsked{Ticket: core.TicketKey{Project: p.ProjectKey, Number: p.Number}.String(), PlanRevision: p.PlanRevision,
			Parent: briefs[p.ParentID].item.Key, Children: []api.DecisionItem{}}
		var kids []brief // in ticket number order (sub-tickets share the epic's project)
		for _, c := range children[i] {
			if b, ok := briefs[c.ID]; ok {
				kids = append(kids, b)
			}
		}
		slices.SortFunc(kids, func(x, y brief) int { return cmp.Compare(x.number, y.number) })
		for _, k := range kids {
			a.Children = append(a.Children, k.item)
		}
		out = append(out, a)
	}
	return out, nil
}
