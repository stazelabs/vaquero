package markdown_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/markdown"
)

// handlerAttr is an on* attribute inside a real tag (escaped text like
// "&lt;img onerror=" is not a tag).
var handlerAttr = regexp.MustCompile(`<[a-z][^>]*\son[a-z]+=`)

// Review Focus 3
func TestRenderIsInert(t *testing.T) {
	for _, src := range []string{
		"<script>alert(1)</script>",
		"[x](javascript:alert(1))",
		"<javascript:alert(1)>",
		`<img src=x onerror="alert(1)">`,
		"[x](JaVaScRiPt:alert(1))",
		"[x](data:text/html;base64,PHNjcmlwdD4=)",
		"![x](javascript:alert(1))",
		"[x](vbscript:msgbox(1))",
	} {
		// Plain text may still say "javascript:"; only markup that can run counts.
		out := strings.ReplaceAll(strings.ToLower(string(markdown.Render(src))), "'", `"`)
		if handlerAttr.MatchString(out) {
			t.Errorf("%q rendered an event handler: %q", src, out)
		}
		for _, bad := range []string{"<script", `href="javascript:`, `src="javascript:`, `href="data:`, `href="vbscript:`} {
			if strings.Contains(out, bad) {
				t.Errorf("%q rendered %q", src, out)
			}
		}
	}
}

func TestRenderKeepsMarkdown(t *testing.T) {
	out := string(markdown.Render("**b** `c`\n\n- [ ] task\n\nhttps://example.com\n\n| a | b |\n|---|---|\n| 1 | 2 |"))
	for _, want := range []string{"<strong>b</strong>", "<code>c</code>", `type="checkbox"`, `href="https://example.com"`, `rel="nofollow"`, "<table>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}

func TestRenderEmpty(t *testing.T) {
	if got := markdown.Render("   "); got != "" {
		t.Fatalf("%q", got)
	}
}

// Raw HTML shows as text rather than vanishing: a ticket that mentions
// <Select> without backticks keeps the word.
func TestRawHTMLShownAsText(t *testing.T) {
	for src, want := range map[string]string{
		"Use the <Select> component.": "&lt;Select&gt;",
		"<script>alert(1)</script>":   "&lt;script&gt;",
		"<div>\nblock\n</div>":        "&lt;div&gt;",
	} {
		if out := string(markdown.Render(src)); !strings.Contains(out, want) {
			t.Errorf("%q rendered %q, want it to contain %q", src, out, want)
		}
	}
}
