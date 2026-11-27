package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/client"
	"github.com/stazelabs/vaquero/internal/help"
	"github.com/stazelabs/vaquero/internal/mcpserver"
	"github.com/stazelabs/vaquero/internal/web"
	"github.com/stazelabs/vaquero/internal/web/webtest"
	"github.com/stazelabs/vaquero/internal/workflow"
)

// noListener points the CLI at a server that is gone, with an empty data
// directory, so a command that tried to reach a server would fail.
func noListener(t *testing.T) string {
	t.Helper()
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	home, data := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("VQ_DATA_DIR", data)
	cfg := client.Config{Profiles: []client.Profile{{Name: "gone", URL: gone.URL, Token: "vq_x"}}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VAQUERO_SERVER", "gone")
	return data
}

func TestWorkflowList(t *testing.T) {
	noListener(t)
	out := mustCLI(t, "workflow")
	for _, want := range []string{"NAME", "plan", "Plan a ticket", "--ticket TICKET", "audit", "--project KEY", "--kind qa|hygiene|continuity|docs",
		"vaquero workflow NAME", "vaquero workflow pack KIND"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "plan") > strings.Index(out, "audit") {
		t.Error("not in List's order")
	}
	var ps []struct {
		Name string `json:"name"`
		Args []struct {
			Name     string   `json:"name"`
			Required bool     `json:"required"`
			Values   []string `json:"values"`
		} `json:"args"`
	}
	if err := json.Unmarshal([]byte(mustCLI(t, "workflow", "--json")), &ps); err != nil || len(ps) != len(workflow.List()) ||
		ps[0].Name != "plan" || ps[1].Args[1].Name != "kind" || len(ps[1].Args[1].Values) != 4 {
		t.Fatalf("json: %v %+v", err, ps)
	}
}

// Review focus 2: vaquero workflow prints exactly what prompts/get answers,
// and needs no server: it runs with nothing listening first.
func TestWorkflowMatchesPromptsGet(t *testing.T) {
	data := noListener(t)
	cases := []struct {
		flags []string
		name  string
		args  map[string]string
	}{
		{[]string{"plan", "--ticket", "VAQ-1"}, "plan", map[string]string{"ticket": "VAQ-1"}},
		{[]string{"plan", "--ticket", "VAQ-1\n# x " + strings.Repeat("y", 200)}, "plan", map[string]string{"ticket": "VAQ-1\n# x " + strings.Repeat("y", 200)}},
		{[]string{"audit", "--project", "VAQ", "--kind", "qa"}, "audit", map[string]string{"project": "VAQ", "kind": "qa"}},
		{[]string{"--kind=docs", "audit", "--project", "VAQ"}, "audit", map[string]string{"project": "VAQ", "kind": "docs"}},
	}
	printed := make([]string, len(cases))
	for i, c := range cases {
		printed[i] = mustCLI(t, append([]string{"workflow"}, c.flags...)...)
	}
	for _, k := range workflow.Packs() {
		want, _ := workflow.Pack(k)
		if got := mustCLI(t, "workflow", "pack", k); got != want {
			t.Errorf("pack %s: %q", k, got)
		}
	}
	if entries, _ := os.ReadDir(data); len(entries) != 0 {
		t.Fatalf("the data directory was touched: %v", entries)
	}

	cliEnv(t)
	for i, c := range cases {
		args, _ := json.Marshal(map[string]any{"name": c.name, "arguments": c.args})
		in := initLine + "\n" + `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":` + string(args) + "}\n"
		code, out, errOut := bridge(t, strings.NewReader(in))
		if code != 0 {
			t.Fatalf("bridge: %d %s", code, errOut)
		}
		as := answers(t, out)
		var res struct {
			Messages []struct {
				Content struct{ Text string } `json:"content"`
			} `json:"messages"`
		}
		if len(as) != 2 || as[1].Error != nil || json.Unmarshal(as[1].Result, &res) != nil || len(res.Messages) != 1 {
			t.Fatalf("prompts/get: %s", out)
		}
		if res.Messages[0].Content.Text != printed[i] {
			t.Errorf("%v: the CLI printed\n%q\nprompts/get answered\n%q", c.flags, printed[i], res.Messages[0].Content.Text)
		}
	}
}

func TestWorkflowRefusals(t *testing.T) {
	noListener(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"plan"}, "Give --ticket.\n"},
		{[]string{"plan", "--ticket", " "}, "Give --ticket.\n"},
		{[]string{"audit", "--project", "VAQ"}, "Give --kind: qa, hygiene, continuity or docs.\n"},
		{[]string{"audit", "--project", "VAQ", "--kind", "perf"}, "Give --kind: qa, hygiene, continuity or docs.\n"},
		{[]string{"audit", "--kind", "qa"}, "Give --project.\n"},
		{[]string{"pack", "perf"}, "Give a pack kind: qa, hygiene, continuity or docs.\n"},
		{[]string{"pack"}, "usage: "},
		{[]string{"nope"}, "no workflow prompt called \"nope\"; run vaquero workflow to list them\n"},
		{[]string{"work", "--project", "VAQ"}, "no workflow prompt called \"work\""},
		{[]string{"plan", "--ticket", "VAQ-1", "--kind", "qa"}, "plan doesn't take --kind"},
		{[]string{"plan", "--ticket", "VAQ-1", "extra"}, "usage: "},
		{[]string{"--ticket", "VAQ-1"}, "usage: "},
	} {
		code, out, errOut := cli(t, append([]string{"workflow"}, c.args...)...)
		if code != 2 || out != "" || !strings.HasPrefix(errOut, c.want) && !strings.Contains(errOut, c.want) {
			t.Errorf("%v: %d %q %q", c.args, code, out, errOut)
		}
	}
}

// Every prompt argument is a flag of vaquero workflow, in the cli article.
func TestWorkflowFlagsDocumented(t *testing.T) {
	a, _ := help.Get("cli")
	for _, p := range workflow.List() {
		for _, arg := range p.Args {
			if !regexp.MustCompile(`--` + arg.Name + `\b`).MatchString(a.Markdown) {
				t.Errorf("--%s isn't in the cli article", arg.Name)
			}
		}
	}
	for _, want := range []string{"`vaquero workflow pack KIND`", "`vaquero workflow"} {
		if !strings.Contains(a.Markdown, want) {
			t.Errorf("cli article lacks %s", want)
		}
	}
}

// R-R3: every backticked MCP tool, vaquero command and /api/v1 route in a
// workflow prompt or pack template exists. The check lives here because
// this is the one package that sees all three lists: the CLI's commands
// (cliCommands, package main, which nothing can import), the MCP tools
// and the router.
//
// What counts as a mention, in backticks (or a fenced code line starting
// "vaquero "): a span starting "vaquero " is a command (its first word must
// be a command and the first two not a renamed spelling); "/api/v1/…",
// optionally after a method, is a route; a snake_case word (two or more
// parts) is a tool. A single-word tool such as `search` is checked only
// when it is spelled as one; a texts' own snake_case word that isn't a tool
// goes in notTools.
func TestWorkflowTextsNameOnlyWhatExists(t *testing.T) {
	ex := existing(t)
	var texts []string
	for _, p := range workflow.List() {
		for _, args := range workflow.SampleArgs(p) {
			out, err := workflow.Render(p, args)
			if err != nil {
				t.Fatal(err)
			}
			texts = append(texts, out)
		}
	}
	for _, k := range workflow.Packs() {
		b, _ := workflow.Pack(k)
		texts = append(texts, b)
	}
	var total [3]int
	for _, text := range texts {
		missing, counts := ex.check(text)
		for _, m := range missing {
			t.Errorf("a workflow text names %s, which doesn't exist", m)
		}
		for i := range counts {
			total[i] += counts[i]
		}
	}
	// The check must have something to bite on.
	if total[0] == 0 || total[1] == 0 || total[2] == 0 {
		t.Fatalf("found %d tools, %d commands and %d routes in the texts", total[0], total[1], total[2])
	}
}

// The checker itself: what is missing is found, what exists passes.
func TestWorkflowExistenceCheck(t *testing.T) {
	ex := existing(t)
	good := "Use `get_ticket`, `vaquero ticket show VAQ-1`, `GET /api/v1/tickets/VAQ-1`, `/api/v1/projects/VAQ/tickets?open=true` and `search`.\n\n" +
		"```\nvaquero plan save VAQ-1 -\n```\n"
	if missing, counts := ex.check(good); len(missing) != 0 || counts != [3]int{2, 2, 2} {
		t.Fatalf("good: %v %v", missing, counts)
	}
	bad := "Call `no_such_tool`, `vaquero frobnicate`, `vaquero comment VAQ-1`, `DELETE /api/v1/tickets/VAQ-1/plan` and `/api/v1/nope`.\n\n" +
		"```\n$ vaquero gone now\n```\n"
	missing, _ := ex.check(bad)
	want := []string{"the MCP tool no_such_tool", "the command vaquero frobnicate", "the command vaquero comment VAQ-1",
		"the route DELETE /api/v1/tickets/VAQ-1/plan", "the route /api/v1/nope", "the command vaquero gone now"}
	if !slices.Equal(missing, want) {
		t.Fatalf("bad: %q", missing)
	}
}

// notTools are snake_case words the texts may backtick that aren't tools.
var notTools = []string{}

type existence struct {
	tools    []string
	commands []string
	routes   [][2]string // method, pattern
}

func existing(t *testing.T) existence {
	t.Helper()
	ex := existence{tools: mcpserver.ToolNames(), commands: []string{"serve", "version", "help"}}
	for n := range cliCommands {
		ex.commands = append(ex.commands, n)
	}
	r := web.NewRouter(webtest.New(t).Deps).(chi.Routes)
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/api/v1/") {
			ex.routes = append(ex.routes, [2]string{method, strings.TrimSuffix(route, "/")})
		}
		return nil
	})
	if len(ex.routes) < 20 {
		t.Fatalf("walked %d routes", len(ex.routes))
	}
	return ex
}

var (
	backticked = regexp.MustCompile("`([^`\n]+)`")
	routeRE    = regexp.MustCompile(`^(?:(GET|HEAD|POST|PUT|PATCH|DELETE) )?(/api/v1/\S*)$`)
	toolRE     = regexp.MustCompile(`^[a-z]+(?:_[a-z]+)+$`)
)

// check returns what text names that doesn't exist, and how many tools,
// commands and routes it names.
func (ex existence) check(text string) (missing []string, counts [3]int) {
	var spans []string
	fenced := false
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			if l := strings.TrimPrefix(strings.TrimSpace(line), "$ "); strings.HasPrefix(l, "vaquero ") {
				spans = append(spans, l)
			}
			continue
		}
		for _, m := range backticked.FindAllStringSubmatch(line, -1) {
			spans = append(spans, m[1])
		}
	}
	for _, s := range spans {
		switch m := routeRE.FindStringSubmatch(s); {
		case strings.HasPrefix(s, "vaquero "):
			counts[1]++
			f := strings.Fields(s)[1:]
			if _, renamed := renamedSpelling(f); !slices.Contains(ex.commands, f[0]) || renamed {
				missing = append(missing, "the command "+s)
			}
		case m != nil:
			counts[2]++
			if !ex.hasRoute(m[1], m[2]) {
				missing = append(missing, "the route "+s)
			}
		case slices.Contains(ex.tools, s):
			counts[0]++
		case toolRE.MatchString(s) && !slices.Contains(notTools, s):
			counts[0]++
			missing = append(missing, "the MCP tool "+s)
		}
	}
	return missing, counts
}

// hasRoute reports whether a route matches path (a query left out; a
// {param} in the route matches any one segment, a * the rest), with
// method when one is given.
func (ex existence) hasRoute(method, path string) bool {
	path, _, _ = strings.Cut(path, "?")
	segs := strings.Split(strings.TrimSuffix(path, "/"), "/")
	for _, r := range ex.routes {
		if (method == "" || r[0] == method) && matchRoute(strings.Split(r[1], "/"), segs) {
			return true
		}
	}
	return false
}

func matchRoute(pat, segs []string) bool {
	for i, p := range pat {
		switch {
		case p == "*":
			return len(segs) > i
		case i >= len(segs), p != segs[i] && !(strings.HasPrefix(p, "{") && segs[i] != ""):
			return false
		}
	}
	return len(pat) == len(segs)
}
