package workflow

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
	if lower, _ := Render(audit, map[string]string{"project": "VAQ", "kind": "qa"}); err != nil || out != lower {
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
	plan, ok := Get("plan")
	if !ok {
		t.Fatal("no plan")
	}
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

// R-R4: the audit prompt looks for the pack in .vaquero/audits first, then
// in .claude/agent-workflow/audits (update-docs.md too for docs), and with
// neither stops with the ruling's message, word for word.
func TestAuditPackLookup(t *testing.T) {
	audit, _ := Get("audit")
	for _, kind := range Packs() {
		out, err := Render(audit, map[string]string{"project": "VAQ", "kind": kind})
		if err != nil {
			t.Fatal(err)
		}
		first, fallback := "`.vaquero/audits/"+kind+".md`", "`.claude/agent-workflow/audits/"+kind+".md`"
		i, j := strings.Index(out, first), strings.Index(out, fallback)
		if i < 0 || j < 0 || i > j {
			t.Errorf("%s: the pack paths, in order: %d %d", kind, i, j)
		}
		stop := `"No audit pack for ` + kind + `: run vaquero workflow pack ` + kind + ` > .vaquero/audits/` + kind + `.md and fill it in."`
		if !strings.Contains(out, stop) {
			t.Errorf("%s: no stop message %s", kind, stop)
		}
		if !strings.Contains(out, "`.claude/agent-workflow/audits/update-docs.md`") {
			t.Errorf("%s: the docs pack's old name", kind)
		}
	}
}

// Each template has its kind's headings, required and optional, in the
// order docs/audit-packs.md lists them, and MissingHeadings finds what a
// pack lacks.
func TestPackTemplatesHaveHeadings(t *testing.T) {
	for _, kind := range Packs() {
		b, _ := Pack(kind)
		if m := MissingHeadings(kind, b); len(m) != 0 {
			t.Errorf("%s template lacks %v", kind, m)
		}
		var want []string
		for _, h := range headings[kind] {
			want = append(want, h.name)
		}
		if got := packHeadings(b); !slices.Equal(got, want) {
			t.Errorf("%s template headings %v, want %v", kind, got, want)
		}
		if len(RequiredHeadings(kind)) == 0 || !slices.Contains(RequiredHeadings(kind), "Standards") {
			t.Errorf("%s: required %v", kind, RequiredHeadings(kind))
		}
	}
	if !slices.Equal(RequiredHeadings("docs"), []string{"Standards", "Ground truth", "Surfaces", "Frozen", "Partitions", "Voice", "Verify"}) {
		t.Errorf("docs: %v", RequiredHeadings("docs"))
	}
	if slices.Contains(RequiredHeadings("hygiene"), "Verification notes") || RequiredHeadings("perf") != nil {
		t.Error("an optional heading is required, or perf has headings")
	}
	// A heading counts only as a whole "## " line.
	pack := "# Pack\n\n## Standards\n\nText ## Boot\n### Never targets\n##Limits\n## Code map \n"
	got := MissingHeadings("qa", pack)
	if slices.Contains(got, "Standards") || slices.Contains(got, "Code map") || !slices.Contains(got, "Boot") ||
		!slices.Contains(got, "Never targets") || !slices.Contains(got, "Limits") {
		t.Fatalf("missing: %v", got)
	}
	if MissingHeadings("perf", pack) != nil {
		t.Fatal("perf")
	}
}

// Task 5: the audit prompt's method: pack, sweep, verify, dedupe, the
// owner's yes as one question, then one Backlog ticket per finding tagged
// audit-{kind} with a tier. It names every heading of every kind, so a
// heading renamed in one place fails here.
func TestAuditSteps(t *testing.T) {
	audit, _ := Get("audit")
	out, err := Render(audit, map[string]string{"project": "VAQ", "kind": "hygiene"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"`search`", "`list_tickets`", "`ask_decision`", "kind question", "`create_ticket`", "Backlog", "`audit-hygiene`",
		"tier", "`file:line`", "own ticket", "never fixes", "subagent", "Never drive the browser from a subagent",
		"`kill PID`", "new tab", "native alert", "2 or 3 attempts", "Never push", "`list_statuses`",
		// Review round 1: qa never reaches the tracker; docs never edits code.
		"new empty temporary data directory", "isn't the tracker's", "`VAQUERO_SERVER=local`", "never navigate to a Never target",
		"confirm its host and port are the scratch server's", "only on the scratch data", "`<…>`",
		"Never edit a source or test file, a doc comment in code included", "Never invent a feature, a ticket key or a date",
		"stop and report the question's id",
		// Final review: the tracker's CLI fallback, only outside the
		// scratch env, and no dev build or installed vaquero elsewhere.
		"only in a shell that has not sourced the qa scratch environment", "`--agent NAME`",
		"`vaquero statuses VAQ`", "`vaquero tickets VAQ --tag audit-hygiene --all`", "`vaquero search WORDS`",
		// The first runs' corrections: every read the steps need, the
		// question and its relay, a sweep without subagents, and never
		// another's tab.
		"`vaquero tickets VAQ`", "`vaquero ticket show TICKET`", "`vaquero decision show ID`",
		"`vaquero decision ask VAQ QUESTION --option \"File all\" --option \"File none\" --why TEXT`",
		"`vaquero decision answer ID OPTION --agent NAME --words TEXT`",
		"When subagents aren't available", "Never close, resize or navigate a tab or window you didn't open",
		"`vaquero ticket new VAQ TITLE --status Backlog --tag audit-hygiene --tier TIER --body -`",
		"`vaquero comment new TICKET -`", "`vaquero ticket link TICKET relates OTHER`",
		"run no other `vaquero` command that talks to a server", "never a dev build (`go run ./cmd/vaquero`, `bin/vaquero`) against anything but a scratch server",
		"`go run ./cmd/vaquero help`", "which need no server",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the audit prompt lacks %q", want)
		}
	}
	for _, kind := range Packs() {
		for _, h := range headings[kind] {
			if !strings.Contains(out, "**"+h.name+"**") {
				t.Errorf("the audit prompt doesn't name %s's **%s**", kind, h.name)
			}
		}
	}
	// In order: the pack, verify, duplicates, the owner's yes, then filing.
	idx := func(s string) int { return strings.Index(out, s) }
	if a, b, c, d, e := idx("## 1. Find the pack"), idx("## 4. Verify"), idx("## 5. Look for duplicates"), idx("## 6. Ask the owner"), idx("## 7. File"); a < 0 || a > b || b > c || c > d || d > e {
		t.Errorf("out of order: %d %d %d %d %d", a, b, c, d, e)
	}
	for _, p := range append([]string{out}, packTexts()...) {
		if strings.Contains(p, "!") {
			t.Errorf("an exclamation point in %.40q", p)
		}
	}
}

func packTexts() []string {
	var out []string
	for _, k := range Packs() {
		b, _ := Pack(k)
		out = append(out, b)
	}
	return out
}

// VAQ-181: Vaquero's own audit packs, in .vaquero/audits, have every
// heading of their kind in the template's order, a body under each, no
// template placeholder left (the audit prompt treats one as a missing
// section) and no exclamation point.
func TestVaqueroAuditPacks(t *testing.T) {
	spans := regexp.MustCompile("`[^`\n]*`")
	angle := regexp.MustCompile(`<[^<>\n]+>`)
	for _, kind := range Packs() {
		b, err := os.ReadFile(filepath.Join("..", "..", ".vaquero", "audits", kind+".md"))
		if err != nil {
			t.Fatal(err)
		}
		pack := string(b)
		if m := MissingHeadings(kind, pack); len(m) != 0 {
			t.Errorf("%s pack lacks %v", kind, m)
		}
		var want []string
		for _, h := range headings[kind] {
			want = append(want, h.name)
		}
		if got := packHeadings(pack); !slices.Equal(got, want) {
			t.Errorf("%s pack headings %v, want %v", kind, got, want)
		}
		// Each section has a body.
		for i, part := range strings.Split(pack, "\n## ")[1:] {
			if _, body, _ := strings.Cut(part, "\n"); strings.TrimSpace(body) == "" {
				t.Errorf("%s pack: section %d is empty", kind, i+1)
			}
		}
		// No placeholder of the template survives, and no <…> outside
		// code: prose that needs a literal tag puts it in backticks.
		tmpl, _ := Pack(kind)
		for _, p := range angle.FindAllString(tmpl, -1) {
			if strings.Contains(pack, p) {
				t.Errorf("%s pack still holds the template's %s", kind, p)
			}
		}
		fenced := false
		for n, line := range strings.Split(pack, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				fenced = !fenced
				continue
			}
			if fenced {
				continue
			}
			if m := angle.FindString(spans.ReplaceAllString(line, "")); m != "" {
				t.Errorf("%s pack line %d: %s outside code", kind, n+1, m)
			}
		}
		if strings.Contains(pack, "!") {
			t.Errorf("%s pack: an exclamation point", kind)
		}
	}
}

// VAQ-181, review rounds 1 and 2: in Vaquero's qa pack every command fails
// closed. A bare `vaquero`, or another install's binary, would talk to the
// owner's tracker, so no code in the pack calls one: the scratch binary is
// "${QA:?}/bin/vaquero" (Boot's own block, which sets it up, uses
// "$qa/bin/vaquero"), the directory and port are ${QA:?} and ${PORT:?},
// every fenced block after Boot's opens by sourcing the env file, and the
// tracker's :7878 appears nowhere. Only a sentence in Never targets or
// Limits that forbids ("never", "out of bounds") may name such a command.
func TestQAPackRunsOnlyTheScratchBinary(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".vaquero", "audits", "qa.md"))
	if err != nil {
		t.Fatal(err)
	}
	problems, blocks := qaPackProblems(string(b))
	for _, p := range problems {
		t.Error(p)
	}
	if blocks < 4 {
		t.Fatalf("found %d fenced blocks", blocks)
	}
	// Final review: the filing exception is the audit prompt's list, in a
	// shell without the env, never a command spelled out here.
	for _, want := range []string{"The one exception is filing", "has not sourced `tmp/qa/env`", "`--agent NAME`"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("qa.md lacks %q", want)
		}
	}
}

// The checker itself, final review item 7: a span is found on the whole
// line before sentences are told apart, so a "; " inside a span never
// splits it, and a forbidding word inside a span never exempts it.
func TestQAPackCheckerCatches(t *testing.T) {
	head := "## Never targets\n\n"
	for name, text := range map[string]string{
		"bare in prose":           "Run `vaquero tickets VAQ` first.\n",
		"semicolon in a span":     "Try `cd /; vaquero tickets VAQ` first.\n",
		"period in a span":        head + "Never mind. Run `cd /. vaquero tickets VAQ` after.\n",
		"never inside the span":   head + "Run `echo never; vaquero tickets VAQ` first.\n",
		"beside a forbidding one": head + "Never run make dev; run `vaquero tickets VAQ` instead.\n",
		"tracker port in prose":   "Open 127.0.0.1:7878 now.\n",
		"unguarded QA":            "Run `$QA/bin/vaquero tickets VAQ`.\n",
		"another binary":          "Run `/opt/homebrew/bin/vaquero tickets VAQ`.\n",
		"bare in a fenced block":  "```\nvaquero tickets VAQ\n```\n",
	} {
		if problems, _ := qaPackProblems(text); len(problems) == 0 {
			t.Errorf("%s: not caught", name)
		}
	}
	for name, text := range map[string]string{
		"forbidding sentence": head + "Never run `vaquero server new`; never pass `--server` either.\n",
		"scratch binary":      "Run `\"${QA:?}/bin/vaquero\" tickets VAQ`.\n",
	} {
		if problems, _ := qaPackProblems(text); len(problems) != 0 {
			t.Errorf("%s: %v", name, problems)
		}
	}
}

// qaPackProblems is TestQAPackRunsOnlyTheScratchBinary's check of a qa
// pack's text: what breaks the pattern, and how many fenced blocks it has.
func qaPackProblems(text string) (problems []string, blocks int) {
	const source = ". /ABSOLUTE/REPO/tmp/qa/env || exit 1"
	bare := regexp.MustCompile(`(^|[\s;&|(` + "`" + `"])vaquero\s`)
	binary := regexp.MustCompile(`\S*bin/vaquero\b`)
	spans := regexp.MustCompile("`[^`\n]+`")
	unguarded := regexp.MustCompile(`\$\{?(QA|PORT)\b`)
	clause := regexp.MustCompile(`\.\s|;\s`)
	allowed := []string{"${QA:?}/bin/vaquero", "$qa/bin/vaquero", "bin/vaquero"}
	fail := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
	check := func(n int, code string) {
		if bare.MatchString(code) {
			fail("qa.md line %d runs a bare vaquero: %s", n, code)
		}
		for _, m := range binary.FindAllString(code, -1) {
			if !slices.Contains(allowed, strings.TrimLeft(m, `"'(`)) {
				fail("qa.md line %d runs another vaquero, %s: %s", n, m, code)
			}
		}
		if strings.Contains(code, ":7878") {
			fail("qa.md line %d names the tracker's port: %s", n, code)
		}
		rest := strings.NewReplacer("${QA:?}", "", "${PORT:?}", "").Replace(code)
		if unguarded.MatchString(rest) {
			fail("qa.md line %d uses QA or PORT without :?: %s", n, code)
		}
	}
	forbids := func(s string) bool {
		l := strings.ToLower(s)
		return strings.Contains(l, "never") || strings.Contains(l, "out of bounds")
	}
	section, fenced, firstLine := "", false, false
	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			if fenced {
				blocks++
				firstLine = true
			}
			continue
		}
		if fenced {
			if firstLine && blocks > 1 && line != source {
				fail("qa.md line %d: block %d doesn't open with %q", n, blocks, source)
			}
			firstLine = false
			check(n, line)
			continue
		}
		if name, ok := strings.CutPrefix(line, "## "); ok {
			section = name
		}
		// Spans first, on the whole line; then the sentences (". " and
		// "; " end one) are found with every span blanked out, so neither
		// a separator nor a forbidding word inside code counts. The
		// exemption is per sentence, and only in Never targets and Limits,
		// so a permitted command beside a forbidding sentence is still
		// checked.
		locs := spans.FindAllStringIndex(line, -1)
		masked := []byte(line)
		for _, l := range locs {
			for k := l[0] + 1; k < l[1]-1; k++ {
				masked[k] = '_'
			}
		}
		starts := []int{0}
		for _, l := range clause.FindAllStringIndex(string(masked), -1) {
			starts = append(starts, l[1])
		}
		ends := append(starts[1:len(starts):len(starts)], len(line))
		exempt := make([]bool, len(starts))
		for j := range starts {
			exempt[j] = (section == "Never targets" || section == "Limits") && forbids(string(masked[starts[j]:ends[j]]))
			if !exempt[j] && strings.Contains(line[starts[j]:ends[j]], ":7878") {
				fail("qa.md line %d names the tracker's port: %s", n, line[starts[j]:ends[j]])
			}
		}
		for _, l := range locs {
			j := 0
			for j+1 < len(starts) && starts[j+1] <= l[0] {
				j++
			}
			if !exempt[j] {
				check(n, strings.Trim(line[l[0]:l[1]], "`"))
			}
		}
	}
	return problems, blocks
}
