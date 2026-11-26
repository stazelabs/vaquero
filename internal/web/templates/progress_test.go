package templates

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// VAQ-129: Chrome on macOS ignores accent-color on <progress>, so the bar
// is styled through its pseudo-elements with theme tokens (input.css), and
// every progress bar carries that class.
func TestProgressBarsUseTheTheme(t *testing.T) {
	css, err := os.ReadFile("../static/css/input.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, sel := range []string{`.progress-vq::-webkit-progress-value`, `.progress-vq::-moz-progress-bar`, `.progress-vq::-webkit-progress-bar`} {
		re := regexp.MustCompile(regexp.QuoteMeta(sel) + `\s*\{[^}]*var\(--color-`)
		if !re.Match(css) {
			t.Errorf("input.css lacks %s with a theme token", sel)
		}
	}
	files, _ := os.ReadDir(".")
	n := 0
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".templ") {
			continue
		}
		b, _ := os.ReadFile(f.Name())
		for _, tag := range regexp.MustCompile(`<progress\b[^>]*>`).FindAllString(string(b), -1) {
			n++
			if !strings.Contains(tag, "progress-vq") || strings.Contains(tag, "accent-accent") {
				t.Errorf("%s: %s", f.Name(), tag)
			}
		}
	}
	if n == 0 {
		t.Fatal("no progress bars found")
	}
}
