package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/units"
)

// Decision kinds: a question an agent parks for the owner, a ruling an agent
// (or the owner) made and records already answered, open to overruling, and
// an approval of a plan, a merge or a release.
const (
	DecisionQuestion = "question"
	DecisionRuling   = "ruling"
	DecisionApproval = "approval"
)

// DecisionKinds, DecisionSubjects and DecisionStates are the values the
// schema allows (a subject only on an approval).
func DecisionKinds() []string    { return []string{DecisionQuestion, DecisionRuling, DecisionApproval} }
func DecisionSubjects() []string { return []string{"plan", "merge", "release"} }
func DecisionStates() []string   { return []string{"open", "answered", "withdrawn"} }

// Decision limits: the schema bounds the texts at 20,000 characters.
const (
	MaxDecisionText     = 20000
	MaxDecisionOptions  = 10
	MaxOptionLabel      = 200
	MaxOptionTradeoff   = 2000
	MaxDecisionTickets  = 100
	decisionPayloadClip = 200
)

// ApproveLabel is the one option that approves an approval (matched in
// any case); an approval's options must include it. One asked with no
// options gets
// Approve and Request changes.
const ApproveLabel = "Approve"

// DecisionInput is a decision as asked (or, for a ruling, as made).
// TicketIDs may be empty for a project-level decision; a plan approval
// names at least one, and every one must be in ProjectID.
type DecisionInput struct {
	ProjectID           string
	TicketIDs           []string
	RunID               string
	Kind, Subject       string
	Question            string
	Options             []Option
	Recommendation, Why string
}

// Option is one answer a decision offers, with what choosing it costs.
type Option struct {
	Label    string `json:"label"`
	Tradeoff string `json:"tradeoff"`
}

// Answer is the answer to a decision: one of its options by label (any
// case), or free text, and a note. RelayedBy is the agent recording an
// answer the owner gave it, with the owner's Words; empty when the owner
// answers directly.
type Answer struct {
	Option    string
	Text      string
	Note      string
	RelayedBy string
	Words     string
}

// DecisionPayload is a decision.* event's payload. Question and Answer are
// cut to 200 characters (the decision holds them whole); the owner's
// relayed words stay on the decision. Overruled is the answer an
// overruling replaced, its answer and note cut the same way.
type DecisionPayload struct {
	DecisionID string     `json:"decision_id"`
	Kind       string     `json:"kind"`
	Subject    string     `json:"subject,omitempty"`
	TicketIDs  []string   `json:"ticket_ids"`
	Question   string     `json:"question,omitempty"`
	Answer     string     `json:"answer,omitempty"`
	Approved   bool       `json:"approved,omitempty"`
	Relayed    bool       `json:"relayed,omitempty"`
	RelayedBy  string     `json:"relayed_by,omitempty"`
	Overruled  *Overruled `json:"overruled,omitempty"`
}

// Overruled is a ruling's answer before an overruling: who gave it (an
// actor id), when, and who relayed it, if anyone.
type Overruled struct {
	Answer     string `json:"answer"`
	Note       string `json:"note,omitempty"`
	AnsweredBy string `json:"answered_by"`
	AnsweredAt string `json:"answered_at"`
	RelayedBy  string `json:"relayed_by,omitempty"`
}

// RequirementHash is what an approval pins of a ticket's requirement:
// hex(sha256(NFC(title) + "\n" + NFC(description))). The ticket's version
// won't do: it rises on moves and claims too.
func RequirementHash(title, body string) string {
	h := sha256.Sum256([]byte(norm.NFC.String(title) + "\n" + norm.NFC.String(body)))
	return hex.EncodeToString(h[:])
}

// checkedDecision is a DecisionInput checked against the database.
type checkedDecision struct {
	project gen.Project
	tickets []string // ids, deduplicated, in the order given
	in      DecisionInput
	options string // JSON
}

// checkDecision validates in inside q's transaction. kinds are the kinds
// the caller takes.
func checkDecision(ctx context.Context, q *gen.Queries, in DecisionInput, kinds ...string) (checkedDecision, error) {
	f := fields{}
	in.Kind = strings.TrimSpace(in.Kind)
	in.Subject = strings.TrimSpace(in.Subject)
	switch {
	case !slices.Contains(kinds, in.Kind) && slices.Contains(DecisionKinds(), in.Kind):
		if in.Kind == DecisionRuling {
			f.add("kind", "Record a ruling with its answer.")
		} else {
			f.add("kind", "Ask a question or an approval; a ruling is recorded with its answer.")
		}
	case !slices.Contains(kinds, in.Kind):
		f.add("kind", "Choose a kind: "+orWords(kinds)+".")
	}
	if in.Kind == DecisionApproval {
		if !slices.Contains(DecisionSubjects(), in.Subject) {
			f.add("subject", "Say what is to be approved: plan, merge or release.")
		}
	} else if in.Subject != "" {
		f.add("subject", "Only an approval has a subject.")
	}
	in.Question = text(f, "question", norm.NFC.String(in.Question), MaxDecisionText, "Write the question.", "Keep the question under 20,000 characters.")
	in.Recommendation = text(f, "recommendation", norm.NFC.String(in.Recommendation), MaxDecisionText, "", "Keep the recommendation under 20,000 characters.")
	in.Why = text(f, "why", norm.NFC.String(in.Why), MaxDecisionText, "", "Keep why it matters under 20,000 characters.")
	in.RunID = text(f, "run", in.RunID, MaxOriginField, "", "Keep the run under 80 characters.")
	opts := make([]Option, 0, len(in.Options))
	if len(in.Options) > MaxDecisionOptions {
		f.add("options", "Offer at most 10 options.")
	}
	for _, o := range in.Options {
		label := text(f, "options", norm.NFC.String(o.Label), MaxOptionLabel, "Give every option a label.", "Keep each option's label under 200 characters.")
		trade := text(f, "options", norm.NFC.String(o.Tradeoff), MaxOptionTradeoff, "", "Keep each trade-off under 2,000 characters.")
		if slices.ContainsFunc(opts, func(p Option) bool { return strings.EqualFold(p.Label, label) }) {
			f.add("options", "Give each option its own label.")
		}
		opts = append(opts, Option{Label: label, Tradeoff: trade})
	}
	if in.Kind == DecisionApproval && len(opts) == 0 {
		opts = []Option{{Label: ApproveLabel}, {Label: "Request changes"}}
	}
	if in.Kind == DecisionApproval && ApproveOption(opts) < 0 {
		f.add("options", "An approval needs an Approve option.")
	}
	in.Options = opts
	var ids []string
	for _, id := range in.TicketIDs {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) > MaxDecisionTickets {
		f.add("tickets", "Name at most 100 tickets.")
	}
	if in.Kind == DecisionApproval && in.Subject == "plan" && len(ids) == 0 {
		f.add("tickets", "Name the tickets whose plan is to be approved.")
	}
	if err := f.err(); err != nil {
		return checkedDecision{}, err
	}
	p, err := q.GetProject(ctx, in.ProjectID)
	if err != nil {
		return checkedDecision{}, notFound(err, "get project")
	}
	if p.ArchivedAt.Valid {
		return checkedDecision{}, invalid("project", "Restore this project to record decisions in it.")
	}
	for _, id := range ids {
		t, err := q.GetTicket(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return checkedDecision{}, invalid("tickets", "One of the tickets doesn't exist.")
		}
		if err != nil {
			return checkedDecision{}, fmt.Errorf("get ticket: %w", err)
		}
		if t.ProjectID != p.ID {
			op, err := q.GetProject(ctx, t.ProjectID)
			if err != nil {
				return checkedDecision{}, fmt.Errorf("get project: %w", err)
			}
			return checkedDecision{}, invalid("tickets", TicketKey{Project: op.Key, Number: t.Number}.String()+" isn't in "+p.Key+".")
		}
	}
	b, err := json.Marshal(opts)
	if err != nil {
		return checkedDecision{}, fmt.Errorf("options: %w", err)
	}
	return checkedDecision{project: p, tickets: ids, in: in, options: string(b)}, nil
}

// orWords is "a, b or c".
func orWords(xs []string) string {
	if len(xs) < 2 {
		return strings.Join(xs, "")
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " or " + xs[len(xs)-1]
}

// resolveAnswer checks a against the decision's options: the chosen label
// as the options spell it, or the free text; index is the option chosen,
// -1 for free text.
func resolveAnswer(opts []Option, a Answer) (answer, note string, index int, err error) {
	f := fields{}
	opt := strings.TrimSpace(norm.NFC.String(a.Option))
	txt := text(f, "text", norm.NFC.String(a.Text), MaxDecisionText, "", "Keep the answer under 20,000 characters.")
	note = text(f, "note", norm.NFC.String(a.Note), MaxDecisionText, "", "Keep the note under 20,000 characters.")
	labels := make([]string, len(opts))
	for i, o := range opts {
		labels[i] = o.Label
	}
	choose := "Write an answer."
	if len(labels) > 0 {
		choose = "Choose " + orWords(labels) + ", or write an answer."
	}
	index = -1
	switch {
	case opt != "" && txt != "":
		f.add("option", "Choose an option or write an answer, not both.")
	case opt != "":
		index = slices.IndexFunc(opts, func(o Option) bool { return strings.EqualFold(o.Label, opt) })
		if index < 0 {
			f.add("option", choose)
		} else {
			answer = opts[index].Label
		}
	case txt != "":
		answer = txt
	default:
		f.add("option", choose)
	}
	return answer, note, index, f.err()
}

// ApproveOption is the index of the option labelled Approve (any case),
// the only one that approves; -1 when there is none.
func ApproveOption(opts []Option) int {
	return slices.IndexFunc(opts, func(o Option) bool { return strings.EqualFold(o.Label, ApproveLabel) })
}

// approves reports whether option index approves.
func approves(opts []Option, index int) bool { return index >= 0 && index == ApproveOption(opts) }

// decisionEvent is the event's ticket: the one ticket a decision names,
// else none (a project event, ticket_ids naming them).
func decisionEvent(ids []string) string {
	if len(ids) == 1 {
		return ids[0]
	}
	return ""
}

// createDecision writes d and its tickets. A plan approval pins, for each
// ticket, what it asks about: the RequirementHash, the plan revision, the
// parent and the sub-tickets with their RequirementHash, so the answer
// approves exactly what was asked (rulings L-R7 and L-R9).
func createDecision(ctx context.Context, q *gen.Queries, d gen.Decision, tickets []string) error {
	if err := q.CreateDecision(ctx, gen.CreateDecisionParams(d)); err != nil {
		return fmt.Errorf("create decision: %w", err)
	}
	for _, id := range tickets {
		row := gen.AddDecisionTicketParams{DecisionID: d.ID, TicketID: id, Children: "[]"}
		if d.Kind == DecisionApproval && d.Subject == "plan" {
			t, err := q.GetTicket(ctx, id)
			if err != nil {
				return fmt.Errorf("get ticket: %w", err)
			}
			if row.PlanRevision, err = planRevision(ctx, q, id); err != nil {
				return err
			}
			row.RequirementHash = RequirementHash(t.Title, t.Body)
			row.ParentID = t.ParentID.String
			if row.Children, err = childPins(ctx, q, id); err != nil {
				return err
			}
		}
		if err := q.AddDecisionTicket(ctx, row); err != nil {
			return fmt.Errorf("add decision ticket: %w", err)
		}
	}
	return nil
}

// childPins is ticketID's sub-tickets with their RequirementHash, by id, as
// JSON: what a plan approval asks about and pins of them.
func childPins(ctx context.Context, q *gen.Queries, ticketID string) (string, error) {
	subs, err := q.ListSubTickets(ctx, sql.NullString{String: ticketID, Valid: true})
	if err != nil {
		return "", fmt.Errorf("list sub-tickets: %w", err)
	}
	pins := []PinnedChild{}
	for _, c := range subs {
		pins = append(pins, PinnedChild{ID: c.ID, RequirementHash: RequirementHash(c.Title, c.Body)})
	}
	slices.SortFunc(pins, func(a, b PinnedChild) int { return strings.Compare(a.ID, b.ID) })
	b, err := json.Marshal(pins)
	if err != nil {
		return "", fmt.Errorf("child pins: %w", err)
	}
	return string(b), nil
}

// askedUnchanged is what plan approval d asked about for each ticket,
// refusing an Approve when any of it changed since: the ticket's
// requirement, its plan revision, its parent, or its sub-tickets (one
// added, removed, or with its requirement changed).
func askedUnchanged(ctx context.Context, q *gen.Queries, d gen.Decision) ([]gen.ListDecisionTicketPinsRow, error) {
	pins, err := q.ListDecisionTicketPins(ctx, d.ID)
	if err != nil {
		return nil, fmt.Errorf("decision pins: %w", err)
	}
	for _, p := range pins {
		rev, err := planRevision(ctx, q, p.TicketID)
		if err != nil {
			return nil, err
		}
		children, err := childPins(ctx, q, p.TicketID)
		if err != nil {
			return nil, err
		}
		if rev != p.PlanRevision || RequirementHash(p.Title, p.Body) != p.RequirementHash ||
			p.CurrentParentID != p.ParentID || children != p.Children {
			key := TicketKey{Project: p.ProjectKey, Number: p.Number}.String()
			return nil, invalid("answer", "The plan or requirement for "+key+" changed since this was asked; ask again.")
		}
	}
	return pins, nil
}

// AskDecision records a question for the owner, or asks for an approval
// (subject plan, merge or release), open until answered or withdrawn. One
// decision.asked event, on the decision's ticket when it names one.
func (c *Core) AskDecision(ctx context.Context, by string, in DecisionInput) (gen.Decision, error) {
	var d gen.Decision
	err := c.tx(ctx, func(q *gen.Queries) error {
		cd, err := checkDecision(ctx, q, in, DecisionQuestion, DecisionApproval)
		if err != nil {
			return err
		}
		d = gen.Decision{
			ID: idgen.New(), ProjectID: cd.project.ID, RunID: nullString(cd.in.RunID), Kind: cd.in.Kind, Subject: cd.in.Subject,
			Question: cd.in.Question, Options: cd.options, Recommendation: cd.in.Recommendation, Why: cd.in.Why,
			State: "open", AskedBy: by, AskedAt: timefmt.Format(c.Now()),
		}
		if err := createDecision(ctx, q, d, cd.tickets); err != nil {
			return err
		}
		return c.emit(ctx, q, by, EvDecisionAsked, cd.project.ID, decisionEvent(cd.tickets), DecisionPayload{
			DecisionID: d.ID, Kind: d.Kind, Subject: d.Subject, TicketIDs: nonNil(cd.tickets), Question: units.Clip(d.Question, decisionPayloadClip),
		})
	})
	if err != nil {
		return gen.Decision{}, err
	}
	return d, nil
}

// RecordRuling records a ruling by made, already answered by by (marked as
// its call): kind ruling, with a's option or text. A ruling stays open to
// overruling by the owner (AnswerDecision). a.RelayedBy and a.Words are not
// taken: a ruling is the maker's own.
func (c *Core) RecordRuling(ctx context.Context, by string, in DecisionInput, a Answer) (gen.Decision, error) {
	var d gen.Decision
	err := c.tx(ctx, func(q *gen.Queries) error {
		cd, err := checkDecision(ctx, q, in, DecisionRuling)
		if err != nil {
			return err
		}
		if a.RelayedBy != "" || strings.TrimSpace(a.Words) != "" {
			return invalid("words", "A ruling is its maker's own; record it without relayed words.")
		}
		var opts []Option
		_ = json.Unmarshal([]byte(cd.options), &opts)
		answer, note, _, err := resolveAnswer(opts, a)
		if err != nil {
			return err
		}
		now := timefmt.Format(c.Now())
		d = gen.Decision{
			ID: idgen.New(), ProjectID: cd.project.ID, RunID: nullString(cd.in.RunID), Kind: DecisionRuling,
			Question: cd.in.Question, Options: cd.options, Recommendation: cd.in.Recommendation, Why: cd.in.Why,
			State: "answered", Answer: answer, AnswerNote: note, AnsweredBy: nullString(by), AnsweredAt: nullString(now),
			AskedBy: by, AskedAt: now,
		}
		if err := createDecision(ctx, q, d, cd.tickets); err != nil {
			return err
		}
		return c.emit(ctx, q, by, EvDecisionRuled, cd.project.ID, decisionEvent(cd.tickets), DecisionPayload{
			DecisionID: d.ID, Kind: d.Kind, TicketIDs: nonNil(cd.tickets),
			Question: units.Clip(d.Question, decisionPayloadClip), Answer: units.Clip(answer, decisionPayloadClip),
		})
	})
	if err != nil {
		return gen.Decision{}, err
	}
	return d, nil
}

// AnswerDecision records the owner's answer to an open question or
// approval, or overrules a ruling (the old answer kept in the event's
// overruled). owner must be the local owner (IsOwner): an agent never
// answers as itself, whoever asked. With a.RelayedBy (an agent) the answer
// is relayed: the agent recorded what the owner said, quoted in a.Words,
// which it then needs. Without it the answer is direct, and carries no
// words. Through MCP (the Origin's surface) an answer must be relayed: the
// bridge without an agent can't answer directly. An approval of a plan
// answered Approve writes one approvals row per ticket from what was asked
// (createDecision): the ticket's RequirementHash, its own plan's revision
// (0 with none: a sub-ticket then leans on its epic's own approval) and its
// sub-tickets with theirs. The Approve is refused when any of that, or the
// ticket's parent, changed since it was asked (askedUnchanged); any other
// answer is taken. An answered approval
// is final; a later approval answered otherwise takes its place. One
// decision.answered event either way, the group's ticket_ids in it. The
// project must not be archived.
func (c *Core) AnswerDecision(ctx context.Context, owner, decisionID string, a Answer) (gen.Decision, error) {
	var d gen.Decision
	err := c.tx(ctx, func(q *gen.Queries) error {
		var err error
		d, err = q.GetDecision(ctx, decisionID)
		if err != nil {
			return notFound(err, "get decision")
		}
		who, err := q.GetActor(ctx, owner)
		if err != nil {
			return notFound(err, "get answerer")
		}
		if !IsOwner(who) {
			return invalid("answer", "Only the owner answers a decision; relay the owner's answer with their words.")
		}
		if a.RelayedBy == "" && OriginFrom(ctx).Surface == SurfaceMCP {
			return invalid("agent", "Run vaquero mcp with --agent NAME to relay the owner's answer.")
		}
		p, err := q.GetProject(ctx, d.ProjectID)
		if err != nil {
			return fmt.Errorf("get project: %w", err)
		}
		if p.ArchivedAt.Valid {
			return invalid("project", "Restore this project to record decisions in it.")
		}
		words := strings.TrimSpace(norm.NFC.String(a.Words))
		var relayer gen.Actor
		if a.RelayedBy != "" {
			relayer, err = q.GetActor(ctx, a.RelayedBy)
			if err != nil {
				return notFound(err, "get relayer")
			}
			if relayer.Kind != "agent" {
				return invalid("relayed_by", "Only an agent relays the owner's answer.")
			}
			f := fields{}
			words = checked(f, "words", words, MaxDecisionText, "Quote the owner's answer when relaying it.", "Keep the owner's words under 20,000 characters.")
			if err := f.err(); err != nil {
				return err
			}
		} else if words != "" {
			return invalid("words", "Only a relayed answer quotes the owner's words.")
		}
		switch {
		case d.State == "withdrawn":
			return invalid("decision", "This decision was withdrawn.")
		case d.State == "answered" && d.Kind != DecisionRuling:
			return invalid("decision", "This decision is already answered.")
		}
		var opts []Option
		if err := json.Unmarshal([]byte(d.Options), &opts); err != nil {
			return fmt.Errorf("decision options: %w", err)
		}
		answer, note, index, err := resolveAnswer(opts, a)
		if err != nil {
			return err
		}
		var asked []gen.ListDecisionTicketPinsRow
		if d.Kind == DecisionApproval && d.Subject == "plan" && approves(opts, index) {
			if asked, err = askedUnchanged(ctx, q, d); err != nil {
				return err
			}
		}
		ids, err := q.ListDecisionTicketIDs(ctx, d.ID)
		if err != nil {
			return fmt.Errorf("decision tickets: %w", err)
		}
		pl := DecisionPayload{
			DecisionID: d.ID, Kind: d.Kind, Subject: d.Subject, TicketIDs: nonNil(ids),
			Question: units.Clip(d.Question, decisionPayloadClip), Answer: units.Clip(answer, decisionPayloadClip),
		}
		if a.RelayedBy != "" {
			pl.Relayed, pl.RelayedBy = true, relayer.Name
		}
		if d.State == "answered" {
			pl.Overruled = &Overruled{Answer: units.Clip(d.Answer, decisionPayloadClip), Note: units.Clip(d.AnswerNote, decisionPayloadClip), AnsweredBy: d.AnsweredBy.String, AnsweredAt: d.AnsweredAt.String, RelayedBy: d.RelayedBy.String}
		}
		now := timefmt.Format(c.Now())
		d.State, d.Answer, d.AnswerNote = "answered", answer, note
		d.AnsweredBy, d.AnsweredAt = nullString(who.ID), nullString(now)
		d.RelayedBy, d.RelayedWords = nullString(a.RelayedBy), words
		if err := q.AnswerDecision(ctx, gen.AnswerDecisionParams{
			Answer: d.Answer, AnswerNote: d.AnswerNote, AnsweredBy: d.AnsweredBy, AnsweredAt: d.AnsweredAt,
			RelayedBy: d.RelayedBy, RelayedWords: d.RelayedWords, ID: d.ID,
		}); err != nil {
			return fmt.Errorf("answer decision: %w", err)
		}
		if d.Kind == DecisionApproval && approves(opts, index) {
			pl.Approved = true
			for _, p := range asked { // a plan approval's tickets
				if err := approve(ctx, q, d.ID, p, now); err != nil {
					return err
				}
			}
		}
		return c.emit(ctx, q, who.ID, EvDecisionAnswered, d.ProjectID, decisionEvent(ids), pl)
	})
	if err != nil {
		return gen.Decision{}, err
	}
	return d, nil
}

// ApprovalPins is an approval row's pins: the ticket's sub-tickets as the
// owner was asked about them, the only children its approval covers, each
// with its requirement then (rulings L-R8 and L-R9). Repo pins arrive in
// part 2.
type ApprovalPins struct {
	Children []PinnedChild `json:"children"`
}

// PinnedChild is a sub-ticket an approval covers, with its RequirementHash
// as asked.
type PinnedChild struct {
	ID              string `json:"id"`
	RequirementHash string `json:"requirement_hash"`
}

// approve writes one ticket's approval row for decision decisionID from
// what was asked, which askedUnchanged found still current.
func approve(ctx context.Context, q *gen.Queries, decisionID string, p gen.ListDecisionTicketPinsRow, at string) error {
	if err := q.AddApproval(ctx, gen.AddApprovalParams{
		ID: idgen.New(), TicketID: p.TicketID, DecisionID: decisionID,
		RequirementHash: p.RequirementHash, PlanRevision: p.PlanRevision,
		Pins: `{"children":` + p.Children + `}`, At: at,
	}); err != nil {
		return fmt.Errorf("add approval: %w", err)
	}
	return nil
}

// planRevision is a ticket's plan revision, 0 when it has no plan.
func planRevision(ctx context.Context, q *gen.Queries, ticketID string) (int64, error) {
	p, err := q.GetPlanByTicket(ctx, ticketID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get plan: %w", err)
	}
	return p.Revision, nil
}

// WithdrawDecision withdraws an open question or approval: by must be who
// asked it, or the owner. A ruling is answered when made, so it is
// overruled rather than withdrawn. One decision.withdrawn event.
func (c *Core) WithdrawDecision(ctx context.Context, by, decisionID string) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		d, err := q.GetDecision(ctx, decisionID)
		if err != nil {
			return notFound(err, "get decision")
		}
		if d.State != "open" {
			return invalid("decision", "Only an open decision can be withdrawn.")
		}
		if by != d.AskedBy {
			who, err := q.GetActor(ctx, by)
			if err != nil {
				return notFound(err, "get actor")
			}
			if !IsOwner(who) {
				return invalid("decision", "Only who asked, or the owner, can withdraw a decision.")
			}
		}
		ids, err := q.ListDecisionTicketIDs(ctx, d.ID)
		if err != nil {
			return fmt.Errorf("decision tickets: %w", err)
		}
		if err := q.WithdrawDecision(ctx, d.ID); err != nil {
			return fmt.Errorf("withdraw decision: %w", err)
		}
		return c.emit(ctx, q, by, EvDecisionWithdrawn, d.ProjectID, decisionEvent(ids), DecisionPayload{
			DecisionID: d.ID, Kind: d.Kind, Subject: d.Subject, TicketIDs: nonNil(ids), Question: units.Clip(d.Question, decisionPayloadClip),
		})
	})
}

// nonNil is ids, or an empty list, so the payload has [] rather than null.
func nonNil(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}
