// Package markdown renders the markdown people and agents write in ticket
// bodies and comments.
//
// goldmark renders it, showing raw HTML as escaped text (so "use <Select>"
// keeps its word instead of losing it), then bluemonday's policy for
// user-written content strips anything that could run: script, event
// handlers, and javascript:, vbscript: or data: links (goldmark alone lets
// a <javascript:...> autolink through). Every link gets rel="nofollow".
//
// Images: only the app's own attachments (a destination starting with
// /attachments/) render as <img>. Any other image becomes a link to its URL
// labelled with its alt text, since the page CSP loads images only from
// 'self' and a remote image would show broken.
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
		goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(rawAsText{}, 100), util.Prioritized(images{}, 100))),
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

// attachmentPrefix starts the only image destinations shown as images.
const attachmentPrefix = "/attachments/"

// images renders an image as <img> for an attachment, else as a link.
type images struct{}

func (images) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindImage, renderImage)
}

func renderImage(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Image)
	dest := util.EscapeHTML(util.URLEscape(n.Destination, true))
	alt := util.EscapeHTML(plainText(source, n))
	if bytes.HasPrefix(n.Destination, []byte(attachmentPrefix)) {
		_, _ = w.WriteString(`<img src="`)
		_, _ = w.Write(dest)
		_, _ = w.WriteString(`" alt="`)
		_, _ = w.Write(alt)
		_, _ = w.WriteString(`">`)
		return ast.WalkSkipChildren, nil
	}
	if len(alt) == 0 {
		alt = dest
	}
	// Inside a link the label alone keeps anchors from nesting.
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == ast.KindLink || p.Kind() == ast.KindAutoLink {
			_, _ = w.Write(alt)
			return ast.WalkSkipChildren, nil
		}
	}
	// bluemonday drops an unsafe href (and then the bare <a>), keeping
	// the label.
	_, _ = w.WriteString(`<a href="`)
	_, _ = w.Write(dest)
	_, _ = w.WriteString(`">`)
	_, _ = w.Write(alt)
	_, _ = w.WriteString(`</a>`)
	return ast.WalkSkipChildren, nil
}

// plainText is the text inside n, without markup: an image's alt text.
func plainText(source []byte, n ast.Node) []byte {
	var b []byte
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch t := c.(type) {
		case *ast.Text:
			b = append(b, t.Segment.Value(source)...)
			if t.SoftLineBreak() || t.HardLineBreak() {
				b = append(b, ' ')
			}
		case *ast.String:
			b = append(b, t.Value...)
		default:
			b = append(b, plainText(source, c)...)
		}
	}
	return b
}
