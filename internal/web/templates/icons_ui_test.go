package templates

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/help"
	"github.com/stazelabs/vaquero/internal/web/icons"
)

func html(t *testing.T, c templ.Component) string {
	t.Helper()
	var b bytes.Buffer
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// glyphBefore reports whether the link whose text is label carries the glyph.
func glyphNear(page, glyph, label string) bool {
	re := regexp.MustCompile(`data-icon="` + regexp.QuoteMeta(glyph) + `"[^>]*>(?:<[a-z]+[^>]*></[a-z]+>)*</svg>\s*(?:<span[^>]*>)?\s*` + regexp.QuoteMeta(label))
	return re.MatchString(page)
}

// The sidebar's rows carry their glyphs; the foot's icon-only links and
// button carry theirs with a label (VAQ-171).
func TestTopBarIcons(t *testing.T) {
	ctx := WithNav(context.Background(), Place{}, func() (Nav, error) { return Nav{}, nil })
	var b bytes.Buffer
	if err := Shell("x").Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	page := b.String()
	for glyph, label := range map[string]string{"inbox": "Inbox", "my-work": "My work", "activity": "Activity", "projects": "All projects"} {
		if !glyphNear(page, glyph, label) {
			t.Errorf("sidebar: %s lacks the %s glyph", label, glyph)
		}
	}
	for glyph, label := range map[string]string{"help": "Help", "admin": "Admin", "keyboard": "Keyboard shortcuts", "menu": "Menu"} {
		re := regexp.MustCompile(`aria-label="` + label + `"[^>]*>\s*<svg[^>]*data-icon="` + glyph + `"`)
		if !re.MatchString(page) {
			t.Errorf("sidebar: %s lacks the %s glyph", label, glyph)
		}
	}
	if !strings.Contains(page, `data-icon="search"`) {
		t.Error("sidebar lacks search")
	}
}

func TestBoardIcons(t *testing.T) {
	b := board.Board{Project: gen.Project{Key: "VAQ", Name: "Vaquero"}}
	col := board.Column{Status: gen.Status{ID: "s1", Name: "Todo", Category: "todo"}, Cards: []board.Card{
		{Key: "VAQ-1", Title: "Fire", Priority: 1, Assignee: "builder", AssigneeIsAgent: true, Ready: false},
		{Key: "VAQ-2", Title: "Calm", Assignee: "Owner", Ready: true},
	}}
	page := html(t, column(b, col))
	for _, g := range []string{"cat-todo", "priority-urgent", "agent", "person", "ready"} {
		if !strings.Contains(page, `data-icon="`+g+`"`) {
			t.Errorf("column lacks %s", g)
		}
	}
	if !strings.Contains(page, "Urgent") || !strings.Contains(page, "Ready") {
		t.Error("glyphs replaced their words")
	}
}

func TestCategoryAndPriorityIcons(t *testing.T) {
	for _, c := range core.Categories {
		if _, ok := icons.UI(CategoryIcon(c)); !ok {
			t.Errorf("category %s: no icon", c)
		}
	}
	for p := int64(1); p <= 4; p++ {
		if _, ok := icons.UI(PriorityIcon(p)); !ok {
			t.Errorf("priority %d: no icon", p)
		}
	}
	if PriorityIcon(0) != "" {
		t.Error("no priority draws a glyph")
	}
}

func TestEveryEventKindHasIcon(t *testing.T) {
	for _, k := range core.EventKinds() {
		if _, ok := icons.UI(EventIcon(k)); !ok {
			t.Errorf("%s: icon %q", k, EventIcon(k))
		}
	}
}

// An icon-only link or button carries aria-label.
// The icon may sit in a span (or a few) inside it.
var iconOnly = regexp.MustCompile(`(?s)<(a|button)\b([^>]*)>\s*(?:<span\b[^>]*>\s*)*@Icon(?:Lg|Class)?\([^)]*\)\s*(?:</span>\s*)*</(?:a|button)>`)

func TestIconOnlyButtonsLabelled(t *testing.T) {
	files, _ := filepath.Glob("*.templ")
	for _, f := range files {
		b, _ := os.ReadFile(f)
		for _, m := range iconOnly.FindAllStringSubmatch(string(b), -1) {
			if !strings.Contains(m[2], "aria-label") {
				t.Errorf("%s: icon-only <%s> without aria-label: %s", f, m[1], m[0])
			}
		}
	}
}

// Review Focus 3: every shortcut in the dialog is in the shortcuts article.
func TestShortcutsDocumented(t *testing.T) {
	a, ok := help.Get("keyboard-shortcuts")
	if !ok {
		t.Fatal("no keyboard-shortcuts article")
	}
	for _, s := range shortcuts {
		if !strings.Contains(a.Markdown, "`"+s[0]+"`") {
			t.Errorf("shortcut %q isn't documented", s[0])
		}
	}
}

// A key app.js handles is in the shortcuts list (and so, by
// TestShortcutsDocumented, in the article): adding a case to the keydown
// handler without listing it fails here.
func TestEveryHandledKeyIsListed(t *testing.T) {
	b, err := os.ReadFile("../static/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	start := strings.Index(src, `addEventListener("keydown"`)
	if start < 0 {
		t.Fatal("no keydown handler in app.js")
	}
	end := strings.Index(src[start:], "\n  });")
	handler := src[start : start+end]
	listed := map[string]bool{}
	for _, s := range shortcuts {
		for _, k := range strings.FieldsFunc(s[0], func(r rune) bool { return r == ' ' || r == '/' && len(s[0]) > 1 }) {
			listed[k] = true
		}
	}
	keys := regexp.MustCompile(`case "(.)":|\bk === "(.)"`)
	seen := 0
	for _, m := range keys.FindAllStringSubmatch(handler, -1) {
		k := m[1] + m[2]
		seen++
		if !listed[k] {
			t.Errorf("app.js handles %q, which the shortcuts list lacks", k)
		}
	}
	if seen < 6 {
		t.Fatalf("found %d keys in the handler; the scan needs updating", seen)
	}
}

// The icon-only scan sees an icon wrapped in a span.
func TestIconOnlyScanSeesWrappedIcons(t *testing.T) {
	for _, src := range []string{
		`<button type="button"><span class="x">@Icon("close")</span></button>`,
		`<a href="/x">@IconLg("close")</a>`,
	} {
		if !iconOnly.MatchString(src) {
			t.Errorf("not seen: %s", src)
		}
	}
	if iconOnly.MatchString(`<a href="/x">@Icon("close") Close</a>`) {
		t.Error("an icon beside text counts as icon-only")
	}
}
