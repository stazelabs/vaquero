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

func TestTopBarIcons(t *testing.T) {
	page := html(t, Shell("x"))
	for glyph, label := range map[string]string{"projects": "Projects", "activity": "Activity", "admin": "Admin", "help": "Help"} {
		if !glyphNear(page, glyph, label) {
			t.Errorf("top bar: %s lacks the %s glyph", label, glyph)
		}
	}
	for _, g := range []string{"search", "keyboard"} {
		if !strings.Contains(page, `data-icon="`+g+`"`) {
			t.Errorf("top bar lacks %s", g)
		}
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
var iconOnly = regexp.MustCompile(`(?s)<(a|button)\b([^>]*)>\s*@Icon(?:Lg|Class)?\([^)]*\)\s*</(?:a|button)>`)

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
