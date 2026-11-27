package help

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// VAQ-163: Welcome describes the top bar the pages have. Every labelled
// link in layout.templ's header is named in the article.
func TestWelcomeNamesEveryTopBarLink(t *testing.T) {
	src, err := os.ReadFile("../web/templates/layout.templ")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := Get("welcome")
	body := strings.ToLower(a.Markdown)
	labels := regexp.MustCompile(`<span class="max-sm:sr-only">([A-Za-z ]+)</span>`).FindAllStringSubmatch(string(src), -1)
	if len(labels) < 5 {
		t.Fatalf("found %d top bar labels in layout.templ", len(labels))
	}
	for _, m := range labels {
		if m[1] == "Vaquero" {
			continue // the logo's name
		}
		if !strings.Contains(body, strings.ToLower(m[1])) {
			t.Errorf("welcome doesn't name the top bar's %s", m[1])
		}
	}
	for _, want := range []string{"search", "keyboard shortcuts"} {
		if !strings.Contains(body, want) || !strings.Contains(strings.ToLower(string(src)), want) {
			t.Errorf("welcome or layout.templ lacks %q", want)
		}
	}
	if strings.Contains(body, "does everything the pages do") {
		t.Error("welcome claims the command line does everything the pages do")
	}
}

// The install article leads with the first run; the service manager's
// detail sits under a heading of its own, below it.
func TestInstallArticleLeadsWithFirstRun(t *testing.T) {
	a, _ := Get("installing-and-upgrading")
	var order []string
	for _, h := range a.Headings {
		order = append(order, h.Text)
	}
	idx := func(s string) int {
		for i, h := range order {
			if strings.EqualFold(h, s) {
				return i
			}
		}
		t.Fatalf("no heading %q in %v", s, order)
		return -1
	}
	if !(idx("Installing with Homebrew") < idx("Opening Vaquero") && idx("Opening Vaquero") < idx("Your first ticket") &&
		idx("Your first ticket") < idx("Connecting an agent") && idx("Connecting an agent") < idx("Running as a service")) {
		t.Errorf("headings out of order: %v", order)
	}
	first := strings.Index(a.Markdown, "launchd")
	if open := strings.Index(a.Markdown, "vaquero open"); open < 0 || first >= 0 && open > first {
		t.Error("vaquero open should come before the service manager detail")
	}
}

// Agent tokens: the articles name the command that makes one, and no
// longer promise a token made on the Admin page acts as an agent.
func TestAgentTokensDocumented(t *testing.T) {
	for _, slug := range []string{"api-tokens", "agents-and-claims"} {
		a, _ := Get(slug)
		if !strings.Contains(a.Markdown, "vaquero token new NAME --agent") {
			t.Errorf("%s doesn't document vaquero token new NAME --agent", slug)
		}
	}
}
