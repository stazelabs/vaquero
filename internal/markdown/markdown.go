// Package markdown renders the markdown people and agents write in ticket
// bodies and comments.
//
// goldmark renders it with raw HTML escaped, then bluemonday's policy for
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
	"github.com/yuin/goldmark/extension"
)

var (
	md     = goldmark.New(goldmark.WithExtensions(extension.GFM))
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
