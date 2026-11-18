// Package markdown renders the markdown people and agents write in ticket
// bodies and comments.
//
// goldmark renders it, showing raw HTML as escaped text (so "use <Select>"
// keeps its word instead of losing it), then bluemonday's policy for
// user-written content strips anything that could run: script, event
// handlers, and javascript:, vbscript: or data: links (goldmark alone lets
// a <javascript:...> autolink through). Every link gets rel="nofollow".
package markdown

import (
	"bytes"
	"html/template"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

var (
	md = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(rawAsText{}, 100))),
	)
	policy = func() *bluemonday.Policy {
		p := bluemonday.UGCPolicy()
		p.RequireNoFollowOnLinks(true)
		// Task-list checkboxes render read-only.
		p.AllowAttrs("type").Matching(bluemonday.SpaceSeparatedTokens).OnElements("input")
		p.AllowAttrs("checked", "disabled").OnElements("input")
		return p
	}()
)

// Render turns src into safe HTML. Blank input renders as "".
func Render(src string) template.HTML {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	var b bytes.Buffer
	if err := md.Convert([]byte(src), &b); err != nil {
		return template.HTML(template.HTMLEscapeString(src))
	}
	return template.HTML(policy.SanitizeBytes(b.Bytes())) //nolint:gosec // sanitized above
}

// rawAsText renders raw HTML, inline or as a block, as visible escaped text.
type rawAsText struct{}

func (rawAsText) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindRawHTML, renderRawInline)
	reg.Register(ast.KindHTMLBlock, renderRawBlock)
}

func renderRawInline(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		segs := n.(*ast.RawHTML).Segments
		for i := range segs.Len() {
			seg := segs.At(i)
			_, _ = w.Write(util.EscapeHTML(seg.Value(source)))
		}
	}
	return ast.WalkSkipChildren, nil
}

func renderRawBlock(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	b := n.(*ast.HTMLBlock)
	_, _ = w.WriteString("<p>")
	lines := b.Lines()
	for i := range lines.Len() {
		line := lines.At(i)
		_, _ = w.Write(util.EscapeHTML(line.Value(source)))
	}
	if b.HasClosure() {
		_, _ = w.Write(util.EscapeHTML(b.ClosureLine.Value(source)))
	}
	_, _ = w.WriteString("</p>\n")
	return ast.WalkContinue, nil
}
