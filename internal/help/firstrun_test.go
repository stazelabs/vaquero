package help

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// VAQ-163, VAQ-171: Welcome describes the sidebar the pages have. Every
// labelled item in sidebar.templ (rows, the foot's buttons, the menu
// button) is named in the article.
func TestWelcomeNamesEveryTopBarLink(t *testing.T) {
	b, err := os.ReadFile("../web/templates/sidebar.templ")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	a, _ := Get("welcome")
	body := strings.ToLower(a.Markdown)
	var labels []string
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`@navItem\("[^"]+", "[^"]+", "([^"]+)"`),
		regexp.MustCompile(`<(?:a|button) [^>]*aria-label="([^"]+)"`),
		regexp.MustCompile(`<span class="truncate">([A-Za-z ]+)</span>`),
	} {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			labels = append(labels, m[1])
		}
	}
	if len(labels) < 8 {
		t.Fatalf("found %d sidebar labels in sidebar.templ: %v", len(labels), labels)
	}
	for _, l := range labels {
		if !strings.Contains(body, strings.ToLower(l)) {
			t.Errorf("welcome doesn't name the sidebar's %s", l)
		}
	}
	for _, want := range []string{"search", "keyboard shortcuts"} {
		if !strings.Contains(body, want) || !strings.Contains(strings.ToLower(src), want) {
			t.Errorf("welcome or sidebar.templ lacks %q", want)
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
