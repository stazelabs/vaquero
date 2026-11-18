package help

import (
	"regexp"
	"slices"
	"strings"
	"testing"
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
var helpLink = regexp.MustCompile(`\(/help/([a-z0-9-]+)(?:\.md)?(?:#([a-z0-9-]+))?\)`)

func TestHelpLinksResolve(t *testing.T) {
	n := 0
	for _, s := range Sections() {
		for _, a := range s.Articles {
			for _, m := range helpLink.FindAllStringSubmatch(a.Markdown, -1) {
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
