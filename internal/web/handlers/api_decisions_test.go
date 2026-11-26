package handlers_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
)

func TestAPIDecisionsRoundTrip(t *testing.T) {
	e, c, tok := apiWorld(t)
	if _, err := e.Deps.Core.CreateProject(bg, e.Deps.Actor.ID, core.ProjectInput{Key: "OPS", Name: "Operations"}); err != nil {
		t.Fatal(err)
	}
	r := c.API("POST", "/api/v1/decisions", tok, map[string]any{
		"project": "vaq", "tickets": []string{"vaq-1"}, "kind": "question", "question": "Which board order?",
		"options":        []map[string]string{{"label": "By priority", "tradeoff": "Urgent first"}, {"label": "By age"}},
		"recommendation": "By priority", "why": "It decides what agents pick up.", "run": "run-7",
	})
	if r.Status != 201 {
		t.Fatalf("ask: %d %s", r.Status, r.Body)
	}
	var q api.Decision
	decode(t, r.Body, &q)
	if q.ID == "" || q.Project != "VAQ" || len(q.Tickets) != 1 || q.Tickets[0] != "VAQ-1" || q.Kind != "question" || q.State != "open" ||
		q.AskedBy != "Owner" || q.AskedAt == "" || q.Run != "run-7" || len(q.Options) != 2 || q.Options[0].Tradeoff != "Urgent first" ||
		q.Recommendation != "By priority" || q.Why == "" || q.AnsweredBy != "" || q.Answered != "" {
		t.Fatalf("%+v", q)
	}
	if got := c.API("GET", "/api/v1/decisions/"+q.ID, tok, nil); got.Status != 200 || got.Body != r.Body {
		t.Fatalf("read back %d:\n%s\nwant\n%s", got.Status, got.Body, r.Body)
	}
	for path, want := range map[string]int{
		"/api/v1/decisions":                            1,
		"/api/v1/decisions?project=VAQ&state=open":     1,
		"/api/v1/decisions?ticket=VAQ-1&kind=question": 1,
		"/api/v1/decisions?ticket=VAQ-2":               0,
		"/api/v1/decisions?project=OPS":                0,
		"/api/v1/decisions?state=answered":             0,
		"/api/v1/decisions?kind=approval&project=vaq":  0,
	} {
		var list []api.Decision
		r := c.API("GET", path, tok, nil)
		decode(t, r.Body, &list)
		if r.Status != 200 || len(list) != want {
			t.Errorf("%s: %d, %d decisions, want %d", path, r.Status, len(list), want)
		}
	}
	for path, field := range map[string]string{
		"/api/v1/decisions?state=pending": "state",
		"/api/v1/decisions?kind=wish":     "kind",
		"/api/v1/decisions?project=NOPE":  "project",
		"/api/v1/decisions?ticket=VAQ-99": "ticket",
	} {
		var e apiErrBody
		r := c.API("GET", path, tok, nil)
		decode(t, r.Body, &e)
		if r.Status != 422 || e.Error.Fields[field] == "" {
			t.Errorf("%s: %d %s", path, r.Status, r.Body)
		}
	}

	// The owner answers directly.
	r = c.API("POST", "/api/v1/decisions/"+q.ID+"/answer", tok, map[string]any{"option": "by age", "note": "Simpler."})
	decode(t, r.Body, &q)
	if r.Status != 200 || q.State != "answered" || q.Answer != "By age" || q.Note != "Simpler." || q.AnsweredBy != "Owner" ||
		q.Answered != "direct" || q.RelayedBy != "" || q.Words != "" || q.AnsweredAt == "" {
		t.Fatalf("answer: %d %s", r.Status, r.Body)
	}
	var inv apiErrBody
	r = c.API("POST", "/api/v1/decisions/"+q.ID+"/answer", tok, map[string]any{"option": "By priority"})
	decode(t, r.Body, &inv)
	if r.Status != 422 || inv.Error.Fields["decision"] != "This decision is already answered." {
		t.Fatalf("again: %d %s", r.Status, r.Body)
	}

	// A ruling is created answered, by its maker.
	r = c.API("POST", "/api/v1/decisions", tok, map[string]any{
		"project": "VAQ", "kind": "ruling", "question": "Tier for VAQ-2?", "options": []map[string]string{{"label": "light"}, {"label": "heavy"}},
		"answer": map[string]string{"option": "light", "note": "Small change."},
	})
	var ruling api.Decision
	decode(t, r.Body, &ruling)
	if r.Status != 201 || ruling.State != "answered" || ruling.Answer != "light" || ruling.AnsweredBy != "Owner" || len(ruling.Tickets) != 0 {
		t.Fatalf("ruling: %d %s", r.Status, r.Body)
	}
	r = c.API("POST", "/api/v1/decisions", tok, map[string]any{
		"project": "VAQ", "kind": "question", "question": "Why?", "answer": map[string]string{"text": "Because."},
	})
	decode(t, r.Body, &inv)
	if r.Status != 422 || inv.Error.Fields["answer"] == "" {
		t.Fatalf("question with an answer: %d %s", r.Status, r.Body)
	}

	// A group plan approval answers with the approvals it pinned.
	r = c.API("POST", "/api/v1/decisions", tok, map[string]any{
		"project": "VAQ", "tickets": []string{"VAQ-1", "VAQ-2"}, "kind": "approval", "subject": "plan", "question": "Approve the plan?",
	})
	var ap api.Decision
	decode(t, r.Body, &ap)
	if r.Status != 201 || len(ap.Options) != 2 || ap.Options[0].Label != "Approve" || ap.Approves != "Approve" || ap.Subject != "plan" || len(ap.Approvals) != 0 {
		t.Fatalf("approval: %d %s", r.Status, r.Body)
	}
	r = c.API("POST", "/api/v1/decisions/"+ap.ID+"/answer", tok, map[string]any{"option": "Approve"})
	decode(t, r.Body, &ap)
	if r.Status != 200 || len(ap.Approvals) != 2 || ap.Approvals[0].Ticket != "VAQ-1" || ap.Approvals[1].Ticket != "VAQ-2" ||
		len(ap.Approvals[0].RequirementHash) != 64 || ap.Approvals[0].PlanRevision != 0 {
		t.Fatalf("approved: %d %s", r.Status, r.Body)
	}

	// An approval's options need Approve; a question has nothing that approves.
	r = c.API("POST", "/api/v1/decisions", tok, map[string]any{
		"project": "VAQ", "kind": "approval", "subject": "merge", "question": "Merge?", "options": []map[string]string{{"label": "Hold off"}, {"label": "Go"}},
	})
	decode(t, r.Body, &inv)
	if r.Status != 422 || inv.Error.Fields["options"] != "An approval needs an Approve option." {
		t.Fatalf("no Approve: %d %s", r.Status, r.Body)
	}
	if q.Approves != "" || strings.Contains(c.API("GET", "/api/v1/decisions/"+q.ID, tok, nil).Body, `"approves"`) {
		t.Fatal("a question names an approving option")
	}

	// Tickets come from the decision's project; unknown ones are named.
	for _, tc := range []struct {
		tickets []string
		want    string
	}{{[]string{"VAQ-99"}, "No ticket VAQ-99."}, {[]string{"nope"}, "That isn't a ticket key."}} {
		r = c.API("POST", "/api/v1/decisions", tok, map[string]any{"project": "OPS", "tickets": tc.tickets, "kind": "question", "question": "Q?"})
		decode(t, r.Body, &inv)
		if r.Status != 422 || inv.Error.Fields["tickets"] != tc.want {
			t.Errorf("%v: %d %s", tc.tickets, r.Status, r.Body)
		}
	}
	r = c.API("POST", "/api/v1/decisions", tok, map[string]any{"project": "OPS", "tickets": []string{"VAQ-1"}, "kind": "question", "question": "Q?"})
	decode(t, r.Body, &inv)
	if r.Status != 422 || inv.Error.Fields["tickets"] != "VAQ-1 isn't in OPS." {
		t.Fatalf("other project: %d %s", r.Status, r.Body)
	}
	if r := c.API("POST", "/api/v1/decisions", tok, map[string]any{"project": "NOPE", "kind": "question", "question": "Q?"}); r.Status != 422 {
		t.Fatalf("no project: %d %s", r.Status, r.Body)
	}
	if r := c.API("POST", "/api/v1/decisions", tok, map[string]any{"project": "VAQ", "kind": "question", "question": "Q?", "extra": 1}); r.Status != 400 {
		t.Fatalf("unknown field: %d %s", r.Status, r.Body)
	}

	// Withdrawing an open question; a missing decision is a 404.
	r = c.API("POST", "/api/v1/decisions", tok, map[string]any{"project": "VAQ", "kind": "question", "question": "Still needed?"})
	var w api.Decision
	decode(t, r.Body, &w)
	r = c.API("POST", "/api/v1/decisions/"+w.ID+"/withdraw", tok, nil)
	decode(t, r.Body, &w)
	if r.Status != 200 || w.State != "withdrawn" {
		t.Fatalf("withdraw: %d %s", r.Status, r.Body)
	}
	for _, path := range []string{"/api/v1/decisions/nope", "/api/v1/decisions/01ARZ3NDEKTSV4RRFFQ69G5FAV"} {
		if r := c.API("GET", path, tok, nil); r.Status != 404 {
			t.Errorf("%s: %d", path, r.Status)
		}
	}
	if r := c.API("POST", "/api/v1/decisions/nope/answer", tok, map[string]any{"option": "x"}); r.Status != 404 {
		t.Fatalf("answer nothing: %d", r.Status)
	}

	// Events carry the decision, cut short, and never the relayed words.
	ev := c.API("GET", "/api/v1/events?newest=20", tok, nil).Body
	for _, want := range []string{`"decision.asked"`, `"decision.ruled"`, `"decision.answered"`, `"decision.withdrawn"`, `"approved": true`} {
		if !strings.Contains(ev, want) {
			t.Errorf("events lack %s", want)
		}
	}
}

// Review Focus 2 through the API: an agent (the owner's token with
// Vaquero-Agent) relays, with the owner's words, or is refused.
func TestAPIRelayedAnswerViaAgentHeader(t *testing.T) {
	_, c, tok := apiWorld(t)
	c.Agent = "builder"
	r := c.API("POST", "/api/v1/decisions", tok, map[string]any{"project": "VAQ", "tickets": []string{"VAQ-1"}, "kind": "question", "question": "Ship it?", "options": []map[string]string{{"label": "Yes"}, {"label": "No"}}})
	var q api.Decision
	decode(t, r.Body, &q)
	if r.Status != 201 || q.AskedBy != "builder" {
		t.Fatalf("ask: %d %s", r.Status, r.Body)
	}
	var inv apiErrBody
	for _, body := range []map[string]any{{"option": "Yes"}, {"option": "Yes", "words": "  "}} {
		r = c.API("POST", "/api/v1/decisions/"+q.ID+"/answer", tok, body)
		decode(t, r.Body, &inv)
		if r.Status != 422 || inv.Error.Fields["words"] != "Quote the owner's answer when relaying it." {
			t.Fatalf("no words: %d %s", r.Status, r.Body)
		}
	}
	r = c.API("POST", "/api/v1/decisions/"+q.ID+"/answer", tok, map[string]any{"option": "Yes", "words": "Yes, ship it today."})
	decode(t, r.Body, &q)
	if r.Status != 200 || q.AnsweredBy != "Owner" || q.Answered != "relayed" || q.RelayedBy != "builder" || q.Words != "Yes, ship it today." {
		t.Fatalf("relayed: %d %s", r.Status, r.Body)
	}
	// Without the header the owner answers directly, and words are refused.
	c.Agent = ""
	r = c.API("POST", "/api/v1/decisions", tok, map[string]any{"project": "VAQ", "kind": "question", "question": "Again?"})
	decode(t, r.Body, &q)
	r = c.API("POST", "/api/v1/decisions/"+q.ID+"/answer", tok, map[string]any{"text": "No.", "words": "No."})
	decode(t, r.Body, &inv)
	if r.Status != 422 || inv.Error.Fields["words"] == "" {
		t.Fatalf("direct with words: %d %s", r.Status, r.Body)
	}
}

func TestMCPDecisionToolsMatchTheAPI(t *testing.T) {
	e, tok := agentEnv(t)
	c := e.Client()
	cs := mcpConnect(t, e, tok, "builder")
	res, text := mcpCall(t, cs, "ask_decision", map[string]any{
		"project": "VAQ", "tickets": []string{"VAQ-1"}, "kind": "approval", "subject": "plan", "question": "Approve the plan?",
	})
	if res.IsError || !strings.Contains(text, `"asked_by": "builder"`) {
		t.Fatalf("ask: %s", text)
	}
	var q api.Decision
	decode(t, text, &q)
	for _, args := range []map[string]any{
		{},
		{"project": "VAQ", "state": "open"},
		{"ticket": "VAQ-1", "kind": "approval"},
	} {
		res, text := mcpCall(t, cs, "list_decisions", args)
		if res.IsError {
			t.Fatalf("list_decisions: %s", text)
		}
		path := "/api/v1/decisions"
		sep := "?"
		for _, k := range []string{"kind", "project", "state", "ticket"} {
			if v, ok := args[k]; ok {
				path += sep + k + "=" + v.(string)
				sep = "&"
			}
		}
		if want := c.API("GET", path, tok, nil).Body; text != want {
			t.Errorf("list_decisions %v:\n got %s\nwant %s", args, text, want)
		}
		checkStructured(t, "list_decisions", res, text)
	}
	res, text = mcpCall(t, cs, "answer_decision", map[string]any{"id": q.ID, "option": "Approve"})
	if !res.IsError || !strings.Contains(text, "Quote the owner's answer when relaying it.") {
		t.Fatalf("no words: %s", text)
	}
	res, text = mcpCall(t, cs, "answer_decision", map[string]any{"id": q.ID, "option": "Approve", "words": "Approved, go ahead."})
	if res.IsError || !strings.Contains(text, `"answered": "relayed"`) || !strings.Contains(text, `"requirement_hash"`) {
		t.Fatalf("answer: %s", text)
	}
	// A ruling through the same tool, answered by the agent as its call.
	res, text = mcpCall(t, cs, "ask_decision", map[string]any{
		"project": "VAQ", "kind": "ruling", "question": "Tier?", "answer": "light", "note": "Docs only.",
	})
	if res.IsError || !strings.Contains(text, `"state": "answered"`) || !strings.Contains(text, `"answered_by": "builder"`) {
		t.Fatalf("ruling: %s", text)
	}
}

// Ruling L-R2: the MCP bridge without --agent can't answer directly.
func TestMCPAnswerNeedsAnAgent(t *testing.T) {
	e, tok := agentEnv(t)
	cs := mcpConnect(t, e, tok, "")
	res, text := mcpCall(t, cs, "ask_decision", map[string]any{"project": "VAQ", "kind": "question", "question": "Which?", "options": []map[string]string{{"label": "A"}, {"label": "B"}}})
	if res.IsError {
		t.Fatalf("ask: %s", text)
	}
	var q api.Decision
	decode(t, text, &q)
	for _, args := range []map[string]any{{"id": q.ID, "option": "A"}, {"id": q.ID, "option": "A", "words": "A."}} {
		res, text = mcpCall(t, cs, "answer_decision", args)
		if !res.IsError || !strings.Contains(text, "Run vaquero mcp with --agent NAME to relay the owner's answer.") {
			t.Fatalf("%v: %s", args, text)
		}
	}
	if got := e.Client().API("GET", "/api/v1/decisions/"+q.ID, tok, nil).Body; !strings.Contains(got, `"state": "open"`) {
		t.Fatalf("answered: %s", got)
	}
}

// A plan approval's answer lists what an Approve covers: each ticket's
// plan revision, epic and sub-tickets as asked.
func TestAPIDecisionShowsWhatWasAsked(t *testing.T) {
	_, c, tok := apiWorld(t) // VAQ-1 "Board work" with sub-ticket VAQ-2 "Columns"
	r := c.API("POST", "/api/v1/decisions", tok, map[string]any{
		"project": "VAQ", "tickets": []string{"VAQ-1", "VAQ-2"}, "kind": "approval", "subject": "plan", "question": "Approve the board plan?",
	})
	if r.Status != 201 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	var d api.Decision
	decode(t, r.Body, &d)
	if len(d.Asked) != 2 {
		t.Fatalf("%+v", d.Asked)
	}
	one, two := d.Asked[0], d.Asked[1]
	if one.Ticket != "VAQ-1" || one.Parent != "" || len(one.Children) != 1 || one.Children[0] != (api.DecisionItem{Key: "VAQ-2", Title: "Columns"}) ||
		two.Ticket != "VAQ-2" || two.Parent != "VAQ-1" || len(two.Children) != 0 || two.Children == nil {
		t.Fatalf("%+v", d.Asked)
	}
	// Other decisions carry none.
	r = c.API("POST", "/api/v1/decisions", tok, map[string]any{"project": "VAQ", "tickets": []string{"VAQ-1"}, "kind": "question", "question": "Which?"})
	if strings.Contains(r.Body, `"asked"`) {
		t.Fatalf("%s", r.Body)
	}
}
