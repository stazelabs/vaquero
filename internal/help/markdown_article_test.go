package help

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/markdown"
)

// markdownExamples is every example in the Writing in Markdown article
// (VAQ-106), as written there, and what the renderer ticket descriptions
// and comments go through makes of it. An example the article shows but
// this table doesn't check fails, so the article can't promise more than
// internal/markdown does.
var markdownExamples = map[string][]string{
	"First line\nsecond line, same paragraph\n\nA new paragraph": {
		"<p>First line\nsecond line, same paragraph</p>", "<p>A new paragraph</p>",
	},
	"Ends with two spaces  \nso this is a new line":    {"spaces<br>\nso"},
	"# Heading\n## Smaller heading\n### Smaller still": {"<h1>Heading</h1>", "<h2>Smaller heading</h2>", "<h3>Smaller still</h3>"},
	"*italic*, **bold**, ~~struck through~~":           {"<em>italic</em>", "<strong>bold</strong>", "<del>struck through</del>"},
	"- One\n- Two\n  - Indented under two\n1. First\n2. Second": {
		"<ul>\n<li>One</li>", "<li>Two\n<ul>\n<li>Indented under two</li>", "<ol>\n<li>First</li>\n<li>Second</li>\n</ol>",
	},
	"- [ ] Write the migration\n- [x] Agree the schema": {
		`<li><input disabled="" type="checkbox"> Write the migration</li>`,
		`<li><input checked="" disabled="" type="checkbox"> Agree the schema</li>`,
	},
	"Run `vaquero ready` first.": {"<code>vaquero ready</code>"},
	"```\nvaquero claim VAQ-12\nvaquero heartbeat VAQ-12\n```": {
		"<pre><code>vaquero claim VAQ-12\nvaquero heartbeat VAQ-12\n</code></pre>",
	},
	"| Step | Who |\n|---|---|\n| Review | Sam |": {"<table>", "<th>Step</th>", "<td>Review</td>", "<td>Sam</td>"},
	"> The build fails on Linux only.":            {"<blockquote>\n<p>The build fails on Linux only.</p>\n</blockquote>"},
	"[the runbook](https://example.com/runbook)":  {`<a href="https://example.com/runbook" rel="nofollow">the runbook</a>`},
	"https://example.com/status":                  {`<a href="https://example.com/status" rel="nofollow">https://example.com/status</a>`},
	"![The error dialog](/attachments/01M89WJCM72AW7ZD4DHSKJ0RQ1/error.png)\n[trace.log](/attachments/01M89WJD1B4C3PZ8N2X7GQ5K6V/trace.log)": {
		`<img src="/attachments/01M89WJCM72AW7ZD4DHSKJ0RQ1/error.png" alt="The error dialog">`,
		`<a href="/attachments/01M89WJD1B4C3PZ8N2X7GQ5K6V/trace.log" rel="nofollow">trace.log</a>`,
	},
	"![A chart](https://example.com/chart.png)": {`<a href="https://example.com/chart.png" rel="nofollow">A chart</a>`},
	"Pick a value from the <select> list.":      {"from the &lt;select&gt; list."},
}

// indentedBlocks returns the article's indented code blocks, unindented: a
// block is lines indented four spaces, blank lines inside it kept.
func indentedBlocks(src string) []string {
	var blocks []string
	var cur []string
	flush := func() {
		for len(cur) > 0 && cur[len(cur)-1] == "" {
			cur = cur[:len(cur)-1]
		}
		if len(cur) > 0 {
			blocks = append(blocks, strings.Join(cur, "\n"))
		}
		cur = nil
	}
	for line := range strings.SplitSeq(src, "\n") {
		switch {
		case strings.HasPrefix(line, "    "):
			cur = append(cur, strings.TrimPrefix(line, "    "))
		case strings.TrimSpace(line) == "" && len(cur) > 0:
			cur = append(cur, "")
		default:
			flush()
		}
	}
	flush()
	return blocks
}

func TestMarkdownArticleExamples(t *testing.T) {
	a, ok := Get("markdown")
	if !ok {
		t.Fatal("no markdown article")
	}
	if a.Title != "Writing in Markdown" || a.Section != "using" {
		t.Fatalf("%q in %q", a.Title, a.Section)
	}
	seen := map[string]bool{}
	for _, b := range indentedBlocks(a.Markdown) {
		want, ok := markdownExamples[b]
		if !ok {
			t.Errorf("example not checked against the renderer:\n%s", b)
			continue
		}
		seen[b] = true
		got := string(markdown.Render(b))
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("example:\n%s\nrenders:\n%s\nwant %q", b, got, w)
			}
		}
	}
	for b := range markdownExamples {
		if !seen[b] {
			t.Errorf("checked example not in the article:\n%s", b)
		}
	}
	ids := map[string]bool{}
	for _, h := range a.Headings {
		ids[h.ID] = true
	}
	for _, id := range []string{"headings", "lists", "code", "tables", "task-lists", "links", "images-and-attachments"} {
		if !ids[id] {
			t.Errorf("no heading %s", id)
		}
	}
}
