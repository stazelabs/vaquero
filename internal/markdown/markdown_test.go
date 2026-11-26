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

// Only the app's own attachments render as images; any other image is a
// link to its URL, so an external image never breaks against the CSP.
func TestMarkdownImagesOnlyForAttachments(t *testing.T) {
	out := string(markdown.Render("![x](/attachments/01h/x.png)"))
	if !strings.Contains(out, `<img src="/attachments/01h/x.png" alt="x"`) {
		t.Errorf("attachment: %s", out)
	}
	out = string(markdown.Render("![shot *one*](/attachments/01h/a%20b.png)"))
	if !strings.Contains(out, `<img src="/attachments/01h/a%20b.png" alt="shot one"`) {
		t.Errorf("alt text: %s", out)
	}
	out = string(markdown.Render("![x](https://e.com/x.png)"))
	if !strings.Contains(out, `<a href="https://e.com/x.png" rel="nofollow">x</a>`) || strings.Contains(out, "<img") {
		t.Errorf("external: %s", out)
	}
	for _, src := range []string{"![x](/attachments/../p/VAQ/board)", "![x](/attachments/a/../../p/VAQ/board)", "![x](/attachments/..)", "![x](/attachments/%2e%2e/p/VAQ/board)", `![x](/attachments/..\\p)`} {
		out = string(markdown.Render(src))
		if strings.Contains(out, "<img") || !strings.Contains(out, "<a href=") {
			t.Errorf("%q: %s", src, out)
		}
	}
	for _, src := range []string{"![x](javascript:alert(1))", "![x](//e.com/x.png)", "![x](data:image/png;base64,AAAA)", `![x"><b>](/attachments/01h/x.png)`} {
		out = string(markdown.Render(src))
		if strings.Contains(out, "<img") && !strings.Contains(src, "/attachments/") {
			t.Errorf("%q rendered an image: %s", src, out)
		}
		if strings.Contains(src, "javascript") && (strings.Contains(out, "<a") || strings.Contains(out, "javascript:")) {
			t.Errorf("%q rendered a link: %s", src, out)
		}
		if strings.Contains(out, "<b>") {
			t.Errorf("%q broke out of alt: %s", src, out)
		}
	}
}

// RenderDoc gives a repo document's headings ids, prefixed doc-, so a
// reference's anchor lands; nothing else gets an id or a new attribute.
func TestRenderDocHeadingIDs(t *testing.T) {
	src := "# The spec\n\n## Acceptance criteria\n\n## Acceptance criteria\n\n### Ünïcode & <stuff>!\n\n## x {#vq-confirm}\n\n" +
		"<h2 id=\"vq-confirm\" onclick=\"alert(1)\">raw</h2>\n\n[a](#acceptance-criteria)\n"
	got := string(markdown.RenderDoc(src))
	for _, want := range []string{
		`<h1 id="doc-the-spec">The spec</h1>`,
		`<h2 id="doc-acceptance-criteria">Acceptance criteria</h2>`,
		`<h2 id="doc-acceptance-criteria-1">Acceptance criteria</h2>`,
		`&lt;h2 id=&#34;vq-confirm&#34;`, // raw HTML stays text
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
	ids := regexp.MustCompile(`\sid="([^"]*)"`).FindAllStringSubmatch(got, -1)
	if len(ids) != 5 {
		t.Errorf("ids %v:\n%s", ids, got)
	}
	for _, m := range ids {
		if !regexp.MustCompile(`^doc-[a-z0-9-]+$`).MatchString(m[1]) {
			t.Errorf("id %q", m[1])
		}
	}
	if handlerAttr.MatchString(got) || strings.Contains(got, `id="vq-confirm"`) {
		t.Errorf("hostile attributes:\n%s", got)
	}
}

// Render, for tickets and comments, gives headings no ids.
func TestRenderHeadingsHaveNoIDs(t *testing.T) {
	if got := string(markdown.Render("## Acceptance criteria\n")); got != "<h2>Acceptance criteria</h2>\n" {
		t.Fatalf("%q", got)
	}
}
