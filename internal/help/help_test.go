package help

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/mcpserver"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

var firstArticles = []string{"welcome", "projects-and-tickets", "the-board", "editing-and-conflicts", "search",
	"keyboard-shortcuts", "cli", "api", "api-tokens", "data-and-backups"}

func TestArticlesComplete(t *testing.T) {
	for _, slug := range firstArticles {
		if _, ok := Get(slug); !ok {
			t.Errorf("no article %q", slug)
		}
	}
	for _, s := range Sections() {
		if s.Title == "" || s.Icon == "" || len(s.Articles) == 0 {
			t.Errorf("section %+v", s.Key)
		}
		orders := map[int]string{}
		for _, a := range s.Articles {
			if a.Title == "" || a.Summary == "" || a.HTML == "" || a.Section != s.Key {
				t.Errorf("%s incomplete", a.Slug)
			}
			if prev, dup := orders[a.Order]; dup {
				t.Errorf("%s and %s share order %d", prev, a.Slug, a.Order)
			}
			orders[a.Order] = a.Slug
			if !strings.HasPrefix(a.Markdown, "# "+a.Title+"\n\n> "+a.Summary) {
				t.Errorf("%s markdown head: %q", a.Slug, a.Markdown[:40])
			}
			if len(a.Headings) == 0 {
				t.Errorf("%s has no ## headings for its contents", a.Slug)
			}
		}
	}
}

func TestHeadings(t *testing.T) {
	a, _ := Get("cli")
	if !slices.ContainsFunc(a.Headings, func(h Heading) bool { return h.Text == "Exit codes" && h.ID == "exit-codes" }) {
		t.Fatalf("%+v", a.Headings)
	}
	if !strings.Contains(string(a.HTML), `<h2 id="exit-codes">`) {
		t.Fatal("headings lack ids")
	}
}

// Review Focus 2: a /help link in any article goes to an article (and an
// anchor on it, when given).
var helpLink = regexp.MustCompile(`href="/help/([^"#]*?)(?:\.md)?(?:#([^"]*))?"`)

func TestHelpLinksResolve(t *testing.T) {
	n := 0
	for _, s := range Sections() {
		for _, a := range s.Articles {
			for _, m := range helpLink.FindAllStringSubmatch(string(a.HTML), -1) {
				n++
				target, ok := Get(m[1])
				if !ok {
					t.Errorf("%s links to /help/%s", a.Slug, m[1])
					continue
				}
				if m[2] != "" && !slices.ContainsFunc(target.Headings, func(h Heading) bool { return h.ID == m[2] }) {
					t.Errorf("%s links to /help/%s#%s, which has no such heading", a.Slug, m[1], m[2])
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("no help links found; articles should point at each other")
	}
}

func TestBadArticlesRefused(t *testing.T) {
	for name, src := range map[string]string{
		"no front matter": "# Hi",
		"no summary":      "---\ntitle: X\nsection: getting-started\norder: 1\n---\nBody",
		"unknown section": "---\ntitle: X\nsummary: Y\nsection: nope\norder: 1\n---\nBody",
	} {
		if _, err := parseArticle("x", []byte(src)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	a, err := parseArticle("x", []byte("---\ntitle: X\nsummary: Y\nsection: getting-started\norder: 1\n---\n## A\n\n<b>raw</b> <javascript:alert(1)> [x](javascript:alert(2))"))
	if err != nil || strings.Contains(string(a.HTML), "<b>") || strings.Contains(string(a.HTML), `href="javascript`) {
		t.Fatalf("%v %s", err, a.HTML)
	}
}

// The API article's time example has the format the API sends.
func TestAPITimeExample(t *testing.T) {
	a, _ := Get("api")
	m := regexp.MustCompile("such as `([^`]+)`").FindStringSubmatch(a.Markdown)
	if m == nil {
		t.Fatal("no time example")
	}
	if _, err := time.Parse(timefmt.Layout, m[1]); err != nil {
		t.Fatalf("%s: %v", m[1], err)
	}
}

// Every error code the API can answer is in the API article.
func TestAPIErrorCodesDocumented(t *testing.T) {
	a, _ := Get("api")
	code := regexp.MustCompile(`Fail\([^,]+,\s*"([a-z_]+)"`)
	seen := 0
	for _, dir := range []string{"../api", "../web/handlers", "../web/middleware"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, _ := os.ReadFile(f)
			for _, m := range code.FindAllStringSubmatch(string(b), -1) {
				seen++
				if !strings.Contains(a.Markdown, "`"+m[1]+"`") {
					t.Errorf("%s: error code %q isn't in the api article", f, m[1])
				}
			}
		}
	}
	if seen < 5 {
		t.Fatalf("found %d codes", seen)
	}
}

// Every MCP tool has its line in the mcp article.
func TestMCPToolsDocumented(t *testing.T) {
	a, ok := Get("mcp")
	if !ok {
		t.Fatal("no mcp article")
	}
	for _, name := range mcpserver.ToolNames() {
		if !strings.Contains(a.Markdown, "| `"+name+"` |") {
			t.Errorf("tool %s isn't in the mcp article", name)
		}
	}
}

// VAQ-60: local mode trusts every program on the machine, so the articles
// say it is for a personal machine and a shared one needs hosted mode.
func TestLocalModeTrustIsDocumented(t *testing.T) {
	for _, slug := range []string{"api-tokens", "welcome"} {
		a, ok := Get(slug)
		if !ok {
			t.Fatalf("no %s article", slug)
		}
		for _, want := range []string{"personal machine", "hosted mode"} {
			if !strings.Contains(a.Markdown, want) {
				t.Errorf("%s doesn't say %q", slug, want)
			}
		}
	}
}
