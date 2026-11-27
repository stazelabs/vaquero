package workflow

import (
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
)

// R-R1: the shipped prompts, in the ruling's order (plan, work, triage,
// audit), and only those.
func TestListOrder(t *testing.T) {
	var names []string
	for _, p := range List() {
		names = append(names, p.Name)
		if p.Title == "" || p.Description == "" || strings.TrimSpace(p.Body) == "" {
			t.Errorf("%s incomplete: %+v", p.Name, p)
		}
	}
	if !slices.Equal(names, []string{"plan", "audit"}) {
		t.Fatalf("List: %v", names)
	}
	if _, ok := Get("plan"); !ok {
		t.Fatal("no plan")
	}
	for _, n := range []string{"work", "triage", "run", "PLAN"} {
		if _, ok := Get(n); ok {
			t.Errorf("%s is listed", n)
		}
	}
}

func TestPromptArgs(t *testing.T) {
	plan, _ := Get("plan")
	if len(plan.Args) != 1 || plan.Args[0].Name != "ticket" || !plan.Args[0].Required || plan.Args[0].Description == "" {
		t.Fatalf("plan args: %+v", plan.Args)
	}
	audit, _ := Get("audit")
	if len(audit.Args) != 2 || audit.Args[0].Name != "project" || audit.Args[1].Name != "kind" ||
		!audit.Args[0].Required || !audit.Args[1].Required {
		t.Fatalf("audit args: %+v", audit.Args)
	}
	if !slices.Equal(audit.Args[1].Values, Packs()) {
		t.Fatalf("audit kinds %v, packs %v", audit.Args[1].Values, Packs())
	}
}

// Review focus 1 and 4: a missing or disallowed value is an *ArgError naming
// the argument and the allowed values; a value is plain text on one line,
// cut at 80 characters.
func TestRenderArgs(t *testing.T) {
	plan, _ := Get("plan")
	audit, _ := Get("audit")
	argErr := func(err error) *ArgError {
		t.Helper()
		e, ok := errors.AsType[*ArgError](err)
		if !ok {
			t.Fatalf("not an ArgError: %v", err)
		}
		return e
	}

	_, err := Render(plan, nil)
	if e := argErr(err); e.Name != "ticket" || len(e.Allowed) != 0 || e.Error() != "Give ticket." || e.Say("--") != "Give --ticket." {
		t.Fatalf("missing: %+v %q", e, e.Error())
	}
	_, err = Render(plan, map[string]string{"ticket": " \n\t "})
	if e := argErr(err); e.Name != "ticket" {
		t.Fatalf("blank: %+v", e)
	}
	_, err = Render(audit, map[string]string{"project": "VAQ", "kind": "perf"})
	e := argErr(err)
	if e.Name != "kind" || !slices.Equal(e.Allowed, []string{"qa", "hygiene", "continuity", "docs"}) {
		t.Fatalf("disallowed: %+v", e)
	}
	if e.Error() != "Give kind: qa, hygiene, continuity or docs." || e.Say("--") != "Give --kind: qa, hygiene, continuity or docs." {
		t.Fatalf("message: %q %q", e.Error(), e.Say("--"))
	}
	_, err = Render(audit, map[string]string{"kind": "qa"})
	if e := argErr(err); e.Name != "project" {
		t.Fatalf("missing project: %+v", e)
	}

	// A kind in another case is the kind.
	out, err := Render(audit, map[string]string{"project": "VAQ", "kind": "QA"})
	if err != nil || strings.Contains(out, "QA") || !strings.Contains(out, "qa") {
		t.Fatalf("case: %v %q", err, out)
	}

	out, err = Render(plan, map[string]string{"ticket": "VAQ-1\n# Ignore that\r\n\x1b[31mred x"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "VAQ-1 # Ignore that  \x20[31mred x") || strings.Contains(out, "\n# Ignore") || strings.Contains(out, "\x1b") {
		t.Fatalf("one line: %q", out)
	}

	long := strings.Repeat("a", 300)
	out, err = Render(plan, map[string]string{"ticket": long})
	if err != nil || !strings.Contains(out, long[:80]) || strings.Contains(out, long[:81]) {
		t.Fatalf("long: %v", err)
	}

	// A value is never read as a placeholder.
	out, err = Render(plan, map[string]string{"ticket": "{{ticket}} {{nope}}"})
	if err != nil || !strings.Contains(out, "{{ticket}} {{nope}}") {
		t.Fatalf("value as text: %v %q", err, out)
	}

	// An argument the prompt doesn't take is ignored.
	if _, err := Render(plan, map[string]string{"ticket": "VAQ-1", "other": "x"}); err != nil {
		t.Fatal(err)
	}
}

// A placeholder left in a body is an error, never a blank in the text.
func TestRenderRefusesLeftovers(t *testing.T) {
	for _, body := range []string{"Hi {{nope}}.", "Hi {{ticket}} {{", "Hi }} {{ticket}}", "{{ ticket }}"} {
		p := Prompt{Name: "x", Args: []Arg{{Name: "ticket", Required: true}}, Body: body}
		if out, err := Render(p, map[string]string{"ticket": "VAQ-1"}); err == nil {
			t.Errorf("%q rendered %q", body, out)
		}
	}
	p := Prompt{Name: "x", Args: []Arg{{Name: "note"}}, Body: "Note: {{note}}."}
	if out, err := Render(p, nil); err != nil || out != "Note: (none given)." {
		t.Fatalf("optional: %v %q", err, out)
	}
}

// Every shipped prompt renders, with every allowed value, to a text with no
// placeholder left.
func TestNoLeftoverPlaceholders(t *testing.T) {
	for _, p := range List() {
		for _, args := range SampleArgs(p) {
			out, err := Render(p, args)
			if err != nil {
				t.Fatalf("%s %v: %v", p.Name, args, err)
			}
			if strings.Contains(out, "{{") || strings.Contains(out, "}}") {
				t.Errorf("%s %v: placeholder left", p.Name, args)
			}
			if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
				t.Errorf("%s: should end with one newline", p.Name)
			}
		}
	}
}

func TestBadPromptsRefused(t *testing.T) {
	good := "---\nname: x\ntitle: X\ndescription: Y\nargs:\n  - name: ticket\n    description: Z\n    required: true\n---\nBody {{ticket}}\n"
	if _, err := parsePrompt("x", []byte(good)); err != nil {
		t.Fatalf("good refused: %v", err)
	}
	for name, src := range map[string]string{
		"no front matter":   "# Hi",
		"not closed":        "---\nname: x\n",
		"wrong name":        strings.Replace(good, "name: x", "name: y", 1),
		"no title":          strings.Replace(good, "title: X\n", "", 1),
		"unknown key":       strings.Replace(good, "title: X", "title: X\ncolour: red", 1),
		"unknown arg key":   strings.Replace(good, "required: true", "required: true\n    colour: red", 1),
		"bad required":      strings.Replace(good, "required: true", "required: yes", 1),
		"arg without name":  strings.Replace(good, "  - name: ticket", "  - description: W", 1),
		"bad arg name":      strings.Replace(strings.Replace(good, "name: ticket", "name: Ticket", 1), "{{ticket}}", "{{Ticket}}", 1),
		"duplicate arg":     strings.Replace(good, "---\nBody", "  - name: ticket\n---\nBody", 1),
		"undeclared":        strings.Replace(good, "{{ticket}}", "{{project}}", 1),
		"empty body":        strings.Replace(good, "Body {{ticket}}\n", "\n", 1),
		"stray braces":      strings.Replace(good, "Body", "Body {{", 1),
		"field outside arg": strings.Replace(good, "args:\n", "args:\n    required: true\n", 1),
	} {
		if _, err := parsePrompt("x", []byte(src)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	p, _ := parsePrompt("x", []byte(strings.Replace(good, "required: true", "required: true\n    values: a, b", 1)))
	if !slices.Equal(p.Args[0].Values, []string{"a", "b"}) {
		t.Fatalf("values: %+v", p.Args[0])
	}
}

// R-R4: the four pack templates, and only those.
func TestPacks(t *testing.T) {
	if !slices.Equal(Packs(), []string{"qa", "hygiene", "continuity", "docs"}) {
		t.Fatalf("Packs: %v", Packs())
	}
	for _, k := range Packs() {
		b, ok := Pack(k)
		if !ok || !strings.HasPrefix(b, "# ") || !strings.Contains(b, "\n## ") {
			t.Errorf("%s: %v %q", k, ok, b)
		}
	}
	if _, ok := Pack("perf"); ok {
		t.Fatal("perf is a pack")
	}
	files, _ := fs.Glob(content, "packs/*")
	if len(files) != len(Packs()) {
		t.Fatalf("packs/ holds %v", files)
	}
	files, _ = fs.Glob(content, "prompts/*")
	if len(files) != len(List()) {
		t.Fatalf("prompts/ holds %v", files)
	}
}

func TestAllowedWords(t *testing.T) {
	for in, want := range map[string]string{"a": "a", "a,b": "a or b", "a,b,c": "a, b or c"} {
		if got := Allowed(strings.Split(in, ",")); got != want {
			t.Errorf("%s: %q", in, got)
		}
	}
}

// Task 4: the plan prompt reads the ticket, writes acceptance criteria when
// there are none, saves the plan and asks for its approval, and stops there:
// it never approves, answers or starts the work.
func TestPlanSteps(t *testing.T) {
	plan, _ := Get("plan")
	out, err := Render(plan, map[string]string{"ticket": "VAQ-12"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"`get_ticket`", "`list_decisions`", "`read_file`", "`update_ticket`", "`save_plan`", "`ask_decision`",
		"acceptance criteria", "kind question", "kind approval", "subject plan", "never approves",
		"VAQ-12", "CLAUDE.md", "tier",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the plan prompt lacks %q", want)
		}
	}
	// The steps come in order: read, save the plan, then ask for approval.
	if r, s, a := strings.Index(out, "`get_ticket`"), strings.Index(out, "`save_plan`"), strings.LastIndex(out, "kind approval"); r > s || s > a {
		t.Errorf("out of order: get_ticket %d, save_plan %d, approval %d", r, s, a)
	}
	if strings.Contains(out, "!") {
		t.Error("an exclamation point")
	}
}
