package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
)

func decisionJSON(t *testing.T, out string) api.Decision {
	t.Helper()
	var d api.Decision
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	return d
}

func TestCLIDecisionAnswer(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Board order")
	mustCLI(t, "ticket", "new", "VAQ", "Board colours")
	id := strings.TrimSpace(mustCLI(t, "decision", "ask", "VAQ", "Which board order?", "--ticket", "VAQ-1",
		"--option", "By priority: urgent first", "--option", "By age", "--recommend", "by priority", "--recommend-why", "Cheaper to read.", "--agent", "builder"))
	if len(id) != 26 {
		t.Fatalf("ask printed %q", id)
	}
	show := mustCLI(t, "decision", "show", id)
	for _, want := range []string{"Which board order?", "VAQ-1", "By priority: urgent first", "By age", "Open", "builder", "Recommended: By priority", "Cheaper to read."} {
		if !strings.Contains(show, want) {
			t.Errorf("show lacks %q:\n%s", want, show)
		}
	}
	if out := mustCLI(t, "decisions", "VAQ", "--state", "open"); !strings.Contains(out, id) || !strings.Contains(out, "Which board order?") {
		t.Fatalf("list: %q", out)
	}

	// The owner answers directly.
	out := mustCLI(t, "decision", "answer", id, "by age", "--note", "Simpler.")
	if !strings.Contains(out, "Answered "+id+": By age.") {
		t.Fatalf("%q", out)
	}
	d := decisionJSON(t, mustCLI(t, "decision", "show", id, "--json"))
	if d.AnsweredBy != "Owner" || d.Answered != "direct" || d.Note != "Simpler." || d.RelayedBy != "" {
		t.Fatalf("%+v", d)
	}

	// An agent relays, with the owner's words, or is refused.
	id = strings.TrimSpace(mustCLI(t, "decision", "ask", "VAQ", "Approve the plans?", "--approval", "plan", "--ticket", "VAQ-1", "--ticket", "vaq-2"))
	if code, _, errOut := cli(t, "decision", "answer", id, "Approve", "--agent", "builder"); code != 1 || !strings.Contains(errOut, "Quote the owner's answer when relaying it.") {
		t.Fatalf("no words: %d %q", code, errOut)
	}
	out = mustCLI(t, "decision", "answer", id, "Approve", "--agent", "builder", "--words", "Yes, both look right.")
	if !strings.Contains(out, "Relayed the owner's answer to "+id+": Approve.") {
		t.Fatalf("%q", out)
	}
	d = decisionJSON(t, mustCLI(t, "decision", "show", id, "--json"))
	if d.AnsweredBy != "Owner" || d.Answered != "relayed" || d.RelayedBy != "builder" || d.Words != "Yes, both look right." || len(d.Approvals) != 2 {
		t.Fatalf("%+v", d)
	}
	if show := mustCLI(t, "decision", "show", id); !strings.Contains(show, "relayed by builder") || !strings.Contains(show, "Yes, both look right.") {
		t.Fatalf("%s", show)
	}

	// A ruling, then the owner overrules it with free text.
	id = strings.TrimSpace(mustCLI(t, "decision", "rule", "VAQ", "Tier for VAQ-2?", "light", "--option", "light", "--option", "heavy", "--note", "Small.", "--agent", "builder"))
	d = decisionJSON(t, mustCLI(t, "decision", "show", id, "--json"))
	if d.Kind != "ruling" || d.State != "answered" || d.Answer != "light" || d.AnsweredBy != "builder" {
		t.Fatalf("%+v", d)
	}
	mustCLI(t, "decision", "answer", id, "--text", "Standard, it touches the board.")
	if d = decisionJSON(t, mustCLI(t, "decision", "show", id, "--json")); d.Answer != "Standard, it touches the board." || d.AnsweredBy != "Owner" {
		t.Fatalf("%+v", d)
	}

	// Withdrawing.
	id = strings.TrimSpace(mustCLI(t, "decision", "ask", "VAQ", "Still needed?"))
	if out := mustCLI(t, "decision", "withdraw", id); !strings.Contains(out, "Withdrew "+id+".") {
		t.Fatalf("%q", out)
	}
	for _, args := range [][]string{{"decision"}, {"decision", "answer"}, {"decision", "ask", "VAQ"}, {"decision", "rule", "VAQ", "Q?"}, {"decision", "nod", "x"},
		{"decision", "answer", id, "Yes", "--text", "No"}, {"decision", "show", id, "--note", "x"}} {
		if code, _, errOut := cli(t, args...); code != 2 || !strings.Contains(errOut, "usage") {
			t.Errorf("%v: %d %q", args, code, errOut)
		}
	}
}

func TestCLIDecisionShowsWhatApprovingCovers(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Board")
	mustCLI(t, "ticket", "new", "VAQ", "Columns", "--parent", "VAQ-1")
	id := strings.TrimSpace(mustCLI(t, "decision", "ask", "VAQ", "Approve the board plan?", "--approval", "plan", "--ticket", "VAQ-1", "--ticket", "VAQ-2"))
	show := mustCLI(t, "decision", "show", id)
	for _, want := range []string{"Approving covers:", "  VAQ-1 at plan revision 0\n    VAQ-2  Columns\n", "  VAQ-2 at plan revision 0, under VAQ-1\n"} {
		if !strings.Contains(show, want) {
			t.Errorf("show lacks %q:\n%s", want, show)
		}
	}
}

// An open question or approval ends with where to answer it in the web;
// an answered one doesn't.
func TestCLIDecisionShowLinksTheInbox(t *testing.T) {
	e := cliEnv(t)
	id := strings.TrimSpace(mustCLI(t, "decision", "ask", "VAQ", "Which way?", "--option", "Left", "--option", "Right"))
	show := mustCLI(t, "decision", "show", id)
	if want := "\nAnswer in the web: " + e.Server.URL + "/decisions/" + id + "\n"; !strings.HasSuffix(show, want) {
		t.Fatalf("show doesn't end with %q:\n%s", want, show)
	}
	mustCLI(t, "decision", "answer", id, "Left")
	if show = mustCLI(t, "decision", "show", id); strings.Contains(show, "Answer in the web") {
		t.Fatalf("an answered decision links the inbox:\n%s", show)
	}
}
