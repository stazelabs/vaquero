// Package help is Vaquero's documentation: markdown articles embedded in the
// binary, shown at /help, served as markdown at /help/{slug}.md for agents
// and scripts, and printed by `vaquero docs`. An article starts with front
// matter between --- lines (title, section, summary, order); its ## headings
// make the page's contents list. A bad article fails at start, so tests
// catch it.
package help

import (
	"bytes"
	"cmp"
	"embed"
	"errors"
	"fmt"
	"html"
	"html/template"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

//go:embed articles/*.md
var content embed.FS

// Article is one help page.
type Article struct {
	Slug, Title, Section, Summary string
	Order                         int
	HTML                          template.HTML
	// Headings are the ## headings, for the page's contents list.
	Headings []Heading
	// Markdown is the article for a reader that isn't a browser: the title,
	// the summary as a quote, then the body as written.
	Markdown string
}

// Heading is one ## heading and its anchor.
type Heading struct{ ID, Text string }

// Section is a group of articles on the index.
type Section struct {
	Key, Title, Icon string
	Articles         []Article
}

var sectionDefs = []struct{ key, title, icon string }{
	{"getting-started", "Getting started", "help"},
	{"using", "Using Vaquero", "board"},
	{"command-line", "Command line", "terminal"},
	{"api-and-agents", "API and agents", "plug"},
}

// md renders Markdown with raw HTML escaped (goldmark's default), tables and
// heading anchors.
var md = goldmark.New(goldmark.WithExtensions(extension.Table), goldmark.WithParserOptions(parser.WithAutoHeadingID()))

var (
	sections []Section
	articles = map[string]Article{}
	slugRE   = regexp.MustCompile(`^[a-z0-9-]+$`)
	h2RE     = regexp.MustCompile(`<h2 id="([^"]+)">(.*?)</h2>`)
	tagRE    = regexp.MustCompile(`<[^>]+>`)
)

func init() {
	if err := load(); err != nil {
		panic("help: " + err.Error())
	}
}

func load() error {
	files, err := content.ReadDir("articles")
	if err != nil {
		return err
	}
	bySection := map[string][]Article{}
	for _, f := range files {
		slug := strings.TrimSuffix(f.Name(), ".md")
		src, err := content.ReadFile(path.Join("articles", f.Name()))
		if err != nil {
			return err
		}
		a, err := parseArticle(slug, src)
		if err != nil {
			return err
		}
		articles[slug] = a
		bySection[a.Section] = append(bySection[a.Section], a)
	}
	for _, s := range sectionDefs {
		list := bySection[s.key]
		slices.SortFunc(list, func(a, b Article) int { return cmp.Or(cmp.Compare(a.Order, b.Order), cmp.Compare(a.Slug, b.Slug)) })
		sections = append(sections, Section{Key: s.key, Title: s.title, Icon: s.icon, Articles: list})
	}
	return nil
}

// parseArticle reads an article's front matter and renders its body.
func parseArticle(slug string, src []byte) (Article, error) {
	a := Article{Slug: slug}
	if !slugRE.MatchString(slug) {
		return a, fmt.Errorf("%s: bad slug", slug)
	}
	rest, ok := bytes.CutPrefix(src, []byte("---\n"))
	if !ok {
		return a, fmt.Errorf("%s: no front matter", slug)
	}
	head, body, ok := bytes.Cut(rest, []byte("\n---\n"))
	if !ok {
		return a, fmt.Errorf("%s: front matter not closed", slug)
	}
	for line := range strings.SplitSeq(string(head), "\n") {
		k, v, _ := strings.Cut(line, ":")
		v = unquote(strings.TrimSpace(v))
		switch strings.TrimSpace(k) {
		case "title":
			a.Title = v
		case "section":
			a.Section = v
		case "summary":
			a.Summary = v
		case "order":
			n, err := strconv.Atoi(v)
			if err != nil {
				return a, fmt.Errorf("%s: order %q", slug, v)
			}
			a.Order = n
		}
	}
	if a.Title == "" || a.Summary == "" {
		return a, fmt.Errorf("%s: needs a title and a summary", slug)
	}
	if !slices.ContainsFunc(sectionDefs, func(s struct{ key, title, icon string }) bool { return s.key == a.Section }) {
		return a, fmt.Errorf("%s: unknown section %q", slug, a.Section)
	}
	h, err := render(body)
	if err != nil {
		return a, fmt.Errorf("%s: %w", slug, err)
	}
	a.HTML = h
	for _, m := range h2RE.FindAllStringSubmatch(string(h), -1) {
		a.Headings = append(a.Headings, Heading{ID: m[1], Text: html.UnescapeString(tagRE.ReplaceAllString(m[2], ""))})
	}
	a.Markdown = "# " + a.Title + "\n\n> " + a.Summary + "\n\n" + string(body)
	return a, nil
}

// unquote drops one pair of matching quotes around a front-matter value.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// dangerousHref matches a link to a scheme that can run code. goldmark
// drops these from [text](url) links but not from <autolinks>.
var dangerousHref = regexp.MustCompile(`(?i)href="(javascript|vbscript|data):`)

// render turns Markdown into HTML; a link to a dangerous scheme is refused.
func render(src []byte) (template.HTML, error) {
	var b bytes.Buffer
	if err := md.Convert(src, &b); err != nil {
		return "", err
	}
	if b.Len() == 0 && len(bytes.TrimSpace(src)) > 0 {
		return "", errors.New("rendered nothing")
	}
	if dangerousHref.Match(b.Bytes()) {
		return "", errors.New("links to a javascript:, vbscript: or data: URL")
	}
	return template.HTML(b.String()), nil
}

// Sections are the index's groups, in order.
func Sections() []Section { return sections }

// Get is the article called slug.
func Get(slug string) (Article, bool) { a, ok := articles[slug]; return a, ok }

// SectionOf is the section an article belongs to.
func SectionOf(a Article) Section {
	for _, s := range sections {
		if s.Key == a.Section {
			return s
		}
	}
	return Section{}
}
